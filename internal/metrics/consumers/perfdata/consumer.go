// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package perfdata

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"

	"github.com/antimetal/agent/internal/metrics"
)

// Compile-time check
var _ metrics.Consumer = (*Consumer)(nil)

const (
	consumerName = "perfdata"
)

// Consumer implements metrics.Consumer for perf.data file writing
type Consumer struct {
	config Config
	logger logr.Logger

	// Writer handles perf.data format
	writer *Writer

	// Internal state
	mu            sync.Mutex
	currentFile   *os.File
	currentPath   string
	currentSize   int64
	rotationTimer *time.Timer
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	healthy       atomic.Bool
	lastError     atomic.Pointer[error]

	// Statistics
	eventsReceived atomic.Uint64
	eventsWritten  atomic.Uint64
	eventsDropped  atomic.Uint64
	bytesWritten   atomic.Uint64
	filesCreated   atomic.Uint64
}

// NewConsumer creates a new perf.data consumer
func NewConsumer(config Config, logger logr.Logger) (*Consumer, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(config.OutputPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	consumer := &Consumer{
		config: config,
		logger: logger.WithName(consumerName),
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

	c.logger.Info("perfdata: received profile event", "metric_type", event.MetricType)
	c.eventsReceived.Add(1)

	// Check if we need to rotate
	c.mu.Lock()
	needsRotation := c.currentFile == nil ||
		(c.config.MaxFileSize > 0 && c.currentSize >= c.config.MaxFileSize)
	c.mu.Unlock()

	if needsRotation {
		if err := c.rotate(); err != nil {
			c.eventsDropped.Add(1)
			c.setLastError(err)
			return fmt.Errorf("failed to rotate file: %w", err)
		}
	}

	// Write event
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.writer == nil {
		c.logger.Error(fmt.Errorf("writer not initialized"), "perfdata: writer is nil")
		c.eventsDropped.Add(1)
		return fmt.Errorf("writer not initialized")
	}

	c.logger.Info("perfdata: calling WriteProfile")
	bytesWritten, err := c.writer.WriteProfile(event)
	if err != nil {
		c.logger.Error(err, "perfdata: WriteProfile failed")
		c.eventsDropped.Add(1)
		c.setLastError(err)
		return fmt.Errorf("failed to write profile: %w", err)
	}

	c.logger.Info("perfdata: wrote profile data", "bytes", bytesWritten)
	c.currentSize += bytesWritten
	c.bytesWritten.Add(uint64(bytesWritten))
	c.eventsWritten.Add(1)

	return nil
}

// Start initializes the consumer
func (c *Consumer) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		return fmt.Errorf("consumer already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start with initial file
	if err := c.rotateUnlocked(); err != nil {
		return fmt.Errorf("failed to create initial file: %w", err)
	}

	// Start rotation timer
	c.rotationTimer = time.AfterFunc(c.config.RotationInterval, c.timedRotation)

	c.logger.Info("perf.data consumer started",
		"output_path", c.config.OutputPath,
		"file_mode", c.config.FileMode,
		"rotation_interval", c.config.RotationInterval,
		"max_file_size", c.config.MaxFileSize)

	return nil
}

// rotate creates a new perf.data file
func (c *Consumer) rotate() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rotateUnlocked()
}

// rotateUnlocked performs rotation (caller must hold mutex)
func (c *Consumer) rotateUnlocked() error {
	// Close current file
	if c.writer != nil {
		if err := c.writer.Close(); err != nil {
			c.logger.Error(err, "failed to close current file")
		}
		c.writer = nil
	}
	if c.currentFile != nil {
		c.currentFile.Close()
		c.currentFile = nil
	}

	// Generate new filename
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("perf-%s.data", timestamp)
	c.currentPath = filepath.Join(c.config.OutputPath, filename)

	// Create new file
	file, err := os.OpenFile(c.currentPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	c.currentFile = file
	c.currentSize = 0
	c.filesCreated.Add(1)

	// Create new writer
	writerConfig := WriterConfig{
		FileMode:   c.config.FileMode,
		BufferSize: c.config.BufferSize,
	}
	c.writer = NewWriter(file, writerConfig, c.logger)

	if err := c.writer.WriteHeader(); err != nil {
		file.Close()
		return fmt.Errorf("failed to write header: %w", err)
	}

	c.logger.Info("rotated to new perf.data file",
		"path", c.currentPath,
		"file_mode", c.config.FileMode)

	// Clean up old files if needed
	if c.config.MaxFiles > 0 {
		c.cleanupOldFiles()
	}

	return nil
}

// timedRotation handles periodic rotation
func (c *Consumer) timedRotation() {
	if err := c.rotate(); err != nil {
		c.logger.Error(err, "failed to rotate file")
		c.setLastError(err)
	}

	// Schedule next rotation
	c.mu.Lock()
	if c.rotationTimer != nil {
		c.rotationTimer.Reset(c.config.RotationInterval)
	}
	c.mu.Unlock()
}

// cleanupOldFiles removes old perf.data files beyond MaxFiles limit
func (c *Consumer) cleanupOldFiles() {
	// TODO: Implement cleanup of old files
	// This would:
	// 1. List all perf-*.data files in OutputPath
	// 2. Sort by modification time
	// 3. Delete oldest files if count > MaxFiles
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
		ErrorsCount: c.eventsDropped.Load(),
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
	if c.rotationTimer != nil {
		c.rotationTimer.Stop()
		c.rotationTimer = nil
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.writer != nil {
		c.writer.Close()
		c.writer = nil
	}
	if c.currentFile != nil {
		c.currentFile.Close()
		c.currentFile = nil
	}
	c.mu.Unlock()

	c.wg.Wait()

	c.logger.Info("perf.data consumer stopped",
		"events_received", c.eventsReceived.Load(),
		"events_written", c.eventsWritten.Load(),
		"events_dropped", c.eventsDropped.Load(),
		"bytes_written", c.bytesWritten.Load(),
		"files_created", c.filesCreated.Load())

	return nil
}
