// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package metrics

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// BusConfig configures the metrics event bus
type BusConfig struct {
	// BufferSize is the size of the internal event buffer
	BufferSize int

	// FlushInterval is how often to flush batched events
	FlushInterval time.Duration

	// MaxBatchSize is the maximum number of events to batch together
	MaxBatchSize int

	// DropPolicy determines what to do when buffer is full
	DropPolicy DropPolicy
}

// DropPolicy determines behavior when the event buffer is full
type DropPolicy string

const (
	DropPolicyOldest DropPolicy = "oldest" // Drop oldest events (default)
	DropPolicyNewest DropPolicy = "newest" // Drop newest events
	DropPolicyBlock  DropPolicy = "block"  // Block until space available
)

var (
	// ErrBusClosed is returned when attempting to publish to a closed bus
	ErrBusClosed = errors.New("metrics bus is closed")
)

// DefaultBusConfig returns a sensible default configuration
func DefaultBusConfig() BusConfig {
	return BusConfig{
		BufferSize:    10000,
		FlushInterval: time.Second,
		MaxBatchSize:  100,
		DropPolicy:    DropPolicyOldest,
	}
}

// MetricsBus is an in-memory event bus that routes metrics events to multiple consumers
// It implements both Publisher and manager.Runnable interfaces
type MetricsBus struct {
	config    BusConfig
	logger    logr.Logger
	mu        sync.RWMutex
	consumers map[string]consumerChannel
	events    chan MetricEvent
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closed    atomic.Bool

	// Metrics (thread-safe counters)
	totalEvents   atomic.Uint64
	droppedEvents atomic.Uint64
}

type consumerChannel struct {
	consumer Consumer
	channel  chan MetricEvent
}

// NewMetricsBus creates a new metrics bus
func NewMetricsBus(config BusConfig, logger logr.Logger) *MetricsBus {
	ctx, cancel := context.WithCancel(context.Background())

	bus := &MetricsBus{
		config:    config,
		logger:    logger.WithName("metrics-bus"),
		consumers: make(map[string]consumerChannel),
		events:    make(chan MetricEvent, config.BufferSize),
		ctx:       ctx,
		cancel:    cancel,
	}

	return bus
}

// Start implements manager.Runnable and begins event processing
func (b *MetricsBus) Start(ctx context.Context) error {
	b.logger.Info("Starting metrics bus", "buffer_size", b.config.BufferSize)

	b.wg.Add(1)
	go b.eventLoop()

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	b.logger.Info("Shutting down metrics bus...")
	if err := b.stop(); err != nil {
		b.logger.Error(err, "Error stopping metrics bus")
	}

	return nil
}

// stop gracefully stops the event bus and all consumers (private method)
func (b *MetricsBus) stop() error {
	b.logger.Info("Stopping metrics bus...")

	// Mark as closed first to prevent new publishes
	b.closed.Store(true)

	// Cancel context and wait for event loop to finish
	b.cancel()
	b.wg.Wait()

	// Stop all consumers
	b.mu.Lock()
	defer b.mu.Unlock()

	for name, cc := range b.consumers {
		if err := cc.consumer.Stop(); err != nil {
			b.logger.Error(err, "Failed to stop consumer", "consumer", name)
		}
		close(cc.channel)
	}

	close(b.events)
	b.logger.Info("Metrics bus stopped")
	return nil
}

// RegisterConsumer adds a consumer to receive events
func (b *MetricsBus) RegisterConsumer(consumer Consumer) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	name := consumer.Name()
	if _, exists := b.consumers[name]; exists {
		return fmt.Errorf("consumer %s already registered", name)
	}

	// Create dedicated channel for this consumer
	ch := make(chan MetricEvent, b.config.BufferSize/4) // Smaller buffer per consumer
	b.consumers[name] = consumerChannel{
		consumer: consumer,
		channel:  ch,
	}

	// Start the consumer
	if err := consumer.Start(ch); err != nil {
		delete(b.consumers, name)
		close(ch)
		return fmt.Errorf("failed to start consumer %s: %w", name, err)
	}

	b.logger.Info("Consumer registered", "consumer", name)
	return nil
}

// UnregisterConsumer removes a consumer
func (b *MetricsBus) UnregisterConsumer(name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	cc, exists := b.consumers[name]
	if !exists {
		return fmt.Errorf("consumer %s not found", name)
	}

	if err := cc.consumer.Stop(); err != nil {
		b.logger.Error(err, "Failed to stop consumer during unregister", "consumer", name)
	}

	close(cc.channel)
	delete(b.consumers, name)
	b.logger.Info("Consumer unregistered", "consumer", name)
	return nil
}

// Publish emits a single metrics event
func (b *MetricsBus) Publish(event MetricEvent) error {
	// Check if bus is closed
	if b.closed.Load() {
		return ErrBusClosed
	}

	select {
	case b.events <- event:
		b.totalEvents.Add(1)
		return nil
	default:
		// Buffer is full, apply drop policy
		switch b.config.DropPolicy {
		case DropPolicyNewest:
			b.droppedEvents.Add(1)
			return fmt.Errorf("event dropped: buffer full")
		case DropPolicyOldest:
			// Try to drain one old event and add the new one
			select {
			case <-b.events:
				b.droppedEvents.Add(1)
			default:
			}
			select {
			case b.events <- event:
				b.totalEvents.Add(1)
				return nil
			default:
				b.droppedEvents.Add(1)
				return fmt.Errorf("event dropped: buffer full")
			}
		case DropPolicyBlock:
			select {
			case b.events <- event:
				b.totalEvents.Add(1)
				return nil
			case <-b.ctx.Done():
				return b.ctx.Err()
			}
		default:
			b.droppedEvents.Add(1)
			return fmt.Errorf("event dropped: unknown drop policy")
		}
	}
}

// PublishBatch emits multiple metrics events efficiently
func (b *MetricsBus) PublishBatch(events []MetricEvent) error {
	for _, event := range events {
		if err := b.Publish(event); err != nil {
			return err
		}
	}
	return nil
}

// GetStats returns bus statistics
func (b *MetricsBus) GetStats() BusStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	consumerStats := make(map[string]ConsumerHealth)
	for name, cc := range b.consumers {
		consumerStats[name] = cc.consumer.Health()
	}

	return BusStats{
		TotalEvents:   b.totalEvents.Load(),
		DroppedEvents: b.droppedEvents.Load(),
		BufferSize:    len(b.events),
		ConsumerCount: len(b.consumers),
		Consumers:     consumerStats,
	}
}

// BusStats contains metrics about the event bus
type BusStats struct {
	TotalEvents   uint64
	DroppedEvents uint64
	BufferSize    int
	ConsumerCount int
	Consumers     map[string]ConsumerHealth
}

// eventLoop is the main event processing loop
func (b *MetricsBus) eventLoop() {
	defer b.wg.Done()

	ticker := time.NewTicker(b.config.FlushInterval)
	defer ticker.Stop()

	batch := make([]MetricEvent, 0, b.config.MaxBatchSize)

	for {
		select {
		case event, ok := <-b.events:
			if !ok {
				// Channel closed, flush remaining batch and exit
				if len(batch) > 0 {
					b.deliverBatch(batch)
				}
				return
			}

			batch = append(batch, event)

			// Flush if batch is full
			if len(batch) >= b.config.MaxBatchSize {
				b.deliverBatch(batch)
				batch = batch[:0] // Reset slice but keep capacity
			}

		case <-ticker.C:
			// Periodic flush
			if len(batch) > 0 {
				b.deliverBatch(batch)
				batch = batch[:0]
			}

		case <-b.ctx.Done():
			// Flush remaining batch and exit
			if len(batch) > 0 {
				b.deliverBatch(batch)
			}
			return
		}
	}
}

// deliverBatch sends events to all registered consumers
func (b *MetricsBus) deliverBatch(events []MetricEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, event := range events {
		for name, cc := range b.consumers {
			select {
			case cc.channel <- event:
				// Successfully delivered
			default:
				// Consumer channel is full, log warning
				b.logger.V(1).Info("Consumer channel full, dropping event",
					"consumer", name, "metric_type", event.MetricType)
			}
		}
	}
}

// Compile-time check that MetricsBus implements manager.Runnable
var _ manager.Runnable = (*MetricsBus)(nil)

// Compile-time check that MetricsBus implements Publisher
var _ Publisher = (*MetricsBus)(nil)
