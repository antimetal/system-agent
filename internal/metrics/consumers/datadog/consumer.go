// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package datadog

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"

	"github.com/antimetal/agent/internal/metrics"
)

// Compile-time check
var _ metrics.Consumer = (*Consumer)(nil)

const (
	consumerName = "datadog-profiling"
)

// Consumer implements metrics.Consumer for Datadog profiling
type Consumer struct {
	config Config
	logger logr.Logger

	// HTTP client for uploads
	httpClient *http.Client

	// Internal state
	mu        sync.Mutex
	buffer    []*metrics.MetricEvent
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	healthy   atomic.Bool
	lastError atomic.Pointer[error]

	// Statistics
	eventsReceived atomic.Uint64
	eventsUploaded atomic.Uint64
	eventsDropped  atomic.Uint64
	uploadErrors   atomic.Uint64
	lastUploadTime atomic.Pointer[time.Time]
}

// NewConsumer creates a new Datadog profiling consumer
func NewConsumer(config Config, logger logr.Logger) (*Consumer, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	consumer := &Consumer{
		config: config,
		logger: logger.WithName(consumerName),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		buffer: make([]*metrics.MetricEvent, 0, config.MaxQueueSize),
	}

	consumer.healthy.Store(true)
	return consumer, nil
}

// Name returns the consumer name
func (c *Consumer) Name() string {
	return consumerName
}

// HandleEvent processes a single metric event
func (c *Consumer) HandleEvent(event metrics.MetricEvent) error {
	// Only handle profile events
	if event.MetricType != metrics.MetricTypeProfile {
		return nil
	}

	c.eventsReceived.Add(1)

	// Buffer the event
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.buffer) >= c.config.MaxQueueSize {
		c.eventsDropped.Add(1)
		c.logger.V(1).Info("dropping profile event, buffer full",
			"buffer_size", len(c.buffer),
			"max_queue_size", c.config.MaxQueueSize)
		return fmt.Errorf("buffer full")
	}

	eventCopy := event
	c.buffer = append(c.buffer, &eventCopy)

	return nil
}

// Start initializes the consumer and starts background workers
func (c *Consumer) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		return fmt.Errorf("consumer already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	c.wg.Add(1)
	go c.uploadWorker()

	c.logger.Info("Datadog profiling consumer started",
		"agent_url", c.config.AgentURL,
		"service", c.config.Service,
		"env", c.config.Env,
		"upload_interval", c.config.UploadInterval)

	return nil
}

// uploadWorker periodically uploads buffered profiles
func (c *Consumer) uploadWorker() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.UploadInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			// Final upload before shutdown
			c.upload()
			return
		case <-ticker.C:
			c.upload()
		}
	}
}

// upload sends buffered profiles to Datadog Agent
func (c *Consumer) upload() {
	c.mu.Lock()
	if len(c.buffer) == 0 {
		c.mu.Unlock()
		return
	}

	// Take all buffered events
	batch := c.buffer
	c.buffer = make([]*metrics.MetricEvent, 0, c.config.MaxQueueSize)
	c.mu.Unlock()

	c.logger.V(1).Info("uploading profiles to Datadog",
		"batch_size", len(batch))

	// Merge all events into a single pprof profile
	// Datadog expects one profile per upload covering the time window
	if len(batch) == 0 {
		return
	}

	// Use the first event for conversion (or merge if needed)
	// For now, we upload each profile separately
	for _, event := range batch {
		if err := c.uploadSingleProfile(*event); err != nil {
			c.uploadErrors.Add(1)
			c.logger.Error(err, "failed to upload profile")
			c.setLastError(err)
		} else {
			c.eventsUploaded.Add(1)
		}
	}

	now := time.Now()
	c.lastUploadTime.Store(&now)
}

// uploadSingleProfile uploads a single profile to Datadog
func (c *Consumer) uploadSingleProfile(event metrics.MetricEvent) error {
	// Convert to pprof
	prof, err := ConvertToPprof(event, c.config)
	if err != nil {
		return fmt.Errorf("converting to pprof: %w", err)
	}

	// Serialize pprof
	var buf bytes.Buffer
	if err := prof.Write(&buf); err != nil {
		return fmt.Errorf("serializing pprof: %w", err)
	}

	// Create multipart form with profile and metadata
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add the profile data
	part, err := writer.CreateFormFile("data[0]", "profile.pprof")
	if err != nil {
		return fmt.Errorf("creating form file: %w", err)
	}
	if _, err := io.Copy(part, &buf); err != nil {
		return fmt.Errorf("writing profile data: %w", err)
	}

	// Add metadata
	metadata := GetProfileMetadata(event, c.config)

	// Format metadata as Datadog expects
	if err := writer.WriteField("version", "4"); err != nil {
		return fmt.Errorf("writing version field: %w", err)
	}
	if err := writer.WriteField("family", metadata.Family); err != nil {
		return fmt.Errorf("writing family field: %w", err)
	}
	if err := writer.WriteField("start", fmt.Sprintf("%d", metadata.Start.Unix())); err != nil {
		return fmt.Errorf("writing start field: %w", err)
	}
	if err := writer.WriteField("end", fmt.Sprintf("%d", metadata.End.Unix())); err != nil {
		return fmt.Errorf("writing end field: %w", err)
	}

	// Add tags
	for k, v := range metadata.Tags {
		if err := writer.WriteField("tags[]", fmt.Sprintf("%s:%s", k, v)); err != nil {
			return fmt.Errorf("writing tag field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("closing multipart writer: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(c.ctx, "POST", c.config.AgentURL, body)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	if c.config.APIKey != "" {
		req.Header.Set("DD-API-KEY", c.config.APIKey)
	}

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	c.logger.V(2).Info("profile uploaded successfully",
		"samples", len(prof.Sample),
		"locations", len(prof.Location),
		"status", resp.StatusCode)

	return nil
}

// Health returns the current health status
func (c *Consumer) Health() metrics.ConsumerHealth {
	var lastErr error
	if errPtr := c.lastError.Load(); errPtr != nil {
		lastErr = *errPtr
	}

	return metrics.ConsumerHealth{
		Healthy:     c.healthy.Load(),
		LastError:   lastErr,
		EventsCount: c.eventsReceived.Load(),
		ErrorsCount: c.uploadErrors.Load(),
	}
}

// setLastError stores the most recent error
func (c *Consumer) setLastError(err error) {
	c.lastError.Store(&err)
	c.healthy.Store(false)
}

// Stop gracefully shuts down the consumer
func (c *Consumer) Stop() error {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()

	c.wg.Wait()

	c.logger.Info("Datadog profiling consumer stopped",
		"events_received", c.eventsReceived.Load(),
		"events_uploaded", c.eventsUploaded.Load(),
		"events_dropped", c.eventsDropped.Load(),
		"upload_errors", c.uploadErrors.Load())

	return nil
}
