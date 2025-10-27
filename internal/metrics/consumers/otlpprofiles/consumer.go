// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otlpprofiles

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"

	"github.com/antimetal/agent/internal/metrics"
)

// Compile-time check
var _ metrics.Consumer = (*Consumer)(nil)

const (
	consumerName = "otlp-profiles"
)

// Consumer implements metrics.Consumer for OTLP profile export
type Consumer struct {
	config Config
	logger logr.Logger

	// Transformer converts ProfileStats to OTLP format
	transformer *Transformer

	// Internal state
	mu        sync.Mutex
	buffer    []*metrics.MetricEvent // Buffered profile events
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	healthy   atomic.Bool
	lastError atomic.Pointer[error]

	// Statistics
	eventsReceived atomic.Uint64
	eventsExported atomic.Uint64
	eventsDropped  atomic.Uint64
	exportErrors   atomic.Uint64
	lastExportTime atomic.Pointer[time.Time]
}

// NewConsumer creates a new OTLP profiling consumer
func NewConsumer(config Config, logger logr.Logger) (*Consumer, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	consumer := &Consumer{
		config: config,
		logger: logger.WithName(consumerName),
		buffer: make([]*metrics.MetricEvent, 0, config.MaxQueueSize),
	}

	// Create transformer
	consumer.transformer = NewTransformer(logger, config.ServiceName, config.ServiceVersion)

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

	// Store copy of event
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

	// TODO: Initialize OTLP exporter here
	// For now, we'll just start the export worker
	c.wg.Add(1)
	go c.exportWorker()

	c.logger.Info("OTLP profiling consumer started",
		"endpoint", c.config.Endpoint,
		"export_interval", c.config.ExportInterval,
		"max_queue_size", c.config.MaxQueueSize)

	return nil
}

// exportWorker periodically exports buffered profiles
func (c *Consumer) exportWorker() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.ExportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			// Final export before shutdown
			c.export()
			return
		case <-ticker.C:
			c.export()
		}
	}
}

// export sends buffered profiles to OTLP endpoint
func (c *Consumer) export() {
	c.mu.Lock()
	if len(c.buffer) == 0 {
		c.mu.Unlock()
		return
	}

	// Take up to ExportBatchSize events
	batchSize := c.config.ExportBatchSize
	if batchSize > len(c.buffer) {
		batchSize = len(c.buffer)
	}

	batch := c.buffer[:batchSize]
	c.buffer = c.buffer[batchSize:]
	c.mu.Unlock()

	c.logger.V(1).Info("exporting profiles",
		"batch_size", len(batch),
		"remaining_buffer", len(c.buffer))

	// TODO: Implement actual OTLP export
	// For now, just transform to verify the pipeline works
	for _, event := range batch {
		if _, err := c.transformer.Transform(*event); err != nil {
			c.exportErrors.Add(1)
			c.logger.Error(err, "failed to transform profile")
			c.setLastError(err)
		} else {
			c.eventsExported.Add(1)
		}
	}

	now := time.Now()
	c.lastExportTime.Store(&now)
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
		ErrorsCount: c.exportErrors.Load(),
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

	c.logger.Info("OTLP profiling consumer stopped",
		"events_received", c.eventsReceived.Load(),
		"events_exported", c.eventsExported.Load(),
		"events_dropped", c.eventsDropped.Load(),
		"export_errors", c.exportErrors.Load())

	return nil
}
