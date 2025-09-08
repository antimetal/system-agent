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

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Compile-time checks
var _ manager.Runnable = (*MetricsRouter)(nil)
var _ manager.LeaderElectionRunnable = (*MetricsRouter)(nil)
var _ Router = (*MetricsRouter)(nil)

var (
	// ErrRouterClosed is returned when attempting to publish to a closed router
	ErrRouterClosed = errors.New("metrics router is closed")
)

// MetricsRouter is a simple registry that routes metrics events to multiple consumers
// It implements both Publisher and manager.Runnable interfaces
type MetricsRouter struct {
	logger    logr.Logger
	mu        sync.RWMutex
	consumers map[string]Consumer
	closed    bool // Set when shutting down
}

// NewMetricsRouter creates a new metrics router
func NewMetricsRouter(logger logr.Logger) *MetricsRouter {
	return &MetricsRouter{
		logger:    logger.WithName("metrics-router"),
		consumers: make(map[string]Consumer),
	}
}

// Start initializes the router and blocks until the context is cancelled.
// This implements the manager.Runnable interface correctly by blocking.
func (r *MetricsRouter) Start(ctx context.Context) error {
	r.logger.Info("Starting metrics router")

	// Block until context is cancelled
	<-ctx.Done()

	// Mark router as closed
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()

	r.logger.Info("Metrics router shutdown")
	return nil
}

// NeedLeaderElection returns false since metrics collection should run on all nodes
func (r *MetricsRouter) NeedLeaderElection() bool {
	return false
}

// RegisterConsumer adds a consumer to receive events.
// The consumer must already be started by the caller before registration.
func (r *MetricsRouter) RegisterConsumer(consumer Consumer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := consumer.Name()

	// Check if already registered
	if _, exists := r.consumers[name]; exists {
		return fmt.Errorf("consumer %s already registered", name)
	}

	r.consumers[name] = consumer
	r.logger.Info("Consumer registered", "consumer", name)
	return nil
}

// UnregisterConsumer removes a consumer
func (r *MetricsRouter) UnregisterConsumer(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, exists := r.consumers[name]
	if !exists {
		return fmt.Errorf("consumer %s not found", name)
	}

	delete(r.consumers, name)
	r.logger.Info("Consumer unregistered", "consumer", name)
	return nil
}

// Publish emits a single metrics event to all registered consumers
func (r *MetricsRouter) Publish(event MetricEvent) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return ErrRouterClosed
	}

	// Directly call HandleEvent on each consumer
	// Consumers handle their own buffering/batching internally
	var lastErr error
	for name, consumer := range r.consumers {
		if err := consumer.HandleEvent(event); err != nil {
			// Log but don't fail - other consumers should still get the event
			r.logger.V(1).Info("Failed to handle event in consumer",
				"consumer", name, "error", err)
			lastErr = err
		}
	}

	return lastErr
}

// PublishBatch emits multiple metrics events efficiently
func (r *MetricsRouter) PublishBatch(events []MetricEvent) error {
	for _, event := range events {
		if err := r.Publish(event); err != nil {
			return err
		}
	}
	return nil
}

// GetStats returns router statistics
func (r *MetricsRouter) GetStats() RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	consumerStats := make(map[string]ConsumerHealth)

	// Get health stats from each consumer
	for name, consumer := range r.consumers {
		consumerStats[name] = consumer.Health()
	}

	return RouterStats{
		ConsumerCount: len(r.consumers),
		Consumers:     consumerStats,
	}
}

// RouterStats contains metrics about the event router
type RouterStats struct {
	ConsumerCount int
	Consumers     map[string]ConsumerHealth
}
