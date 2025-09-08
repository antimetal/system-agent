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
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Compile-time checks
var _ manager.Runnable = (*MetricsRouter)(nil)

var (
	// ErrRouterClosed is returned when attempting to publish to a closed router
	ErrRouterClosed = errors.New("metrics router is closed")
)

// MetricsRouter is a simple registry that routes metrics events to multiple consumers
// It implements Publisher, Receiver, and manager.Runnable interfaces
type MetricsRouter struct {
	logger      logr.Logger
	mu          sync.RWMutex
	consumers   map[string]Consumer
	closed      bool   // Set when shutting down
	nodeName    string // Node name for metric events
	clusterName string // Cluster name for metric events
	source      string // Source identifier for metric events
}

// NewMetricsRouter creates a new metrics router
func NewMetricsRouter(logger logr.Logger, nodeName, clusterName, source string) *MetricsRouter {
	return &MetricsRouter{
		logger:      logger.WithName("metrics-router"),
		consumers:   make(map[string]Consumer),
		nodeName:    nodeName,
		clusterName: clusterName,
		source:      source,
	}
}

// Start initializes the router.
// This just marks the router as started and sets up shutdown handling.
func (r *MetricsRouter) Start(ctx context.Context) error {
	r.logger.Info("Starting metrics router")

	// When context is cancelled, mark as closed
	go func() {
		<-ctx.Done()
		r.mu.Lock()
		r.closed = true
		r.mu.Unlock()
		r.logger.Info("Metrics router shutdown")
	}()

	return nil
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

// publishEvent emits a single metrics event to all registered consumers
func (r *MetricsRouter) publishEvent(event MetricEvent) error {
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

// Accept processes metrics data from collectors and routes them to consumers
// It implements a compatible interface with performance.Receiver
func (r *MetricsRouter) Accept(data any) error {
	// Determine the metric type from the data type
	var metricsType MetricType

	// Use type assertion to determine what kind of data we received
	switch data.(type) {
	case *performance.LoadStats:
		metricsType = MetricTypeLoad
	case *performance.MemoryStats:
		metricsType = MetricTypeMemory
	case *performance.CPUStats:
		metricsType = MetricTypeCPU
	case []*performance.ProcessStats:
		metricsType = MetricTypeProcess
	case []*performance.DiskStats:
		metricsType = MetricTypeDisk
	case []performance.NetworkStats:
		metricsType = MetricTypeNetwork
	case *performance.TCPStats:
		metricsType = MetricTypeTCP
	case []*performance.KernelMessage:
		metricsType = MetricTypeKernel
	case *performance.SystemStats:
		metricsType = MetricTypeSystem
	case *performance.CPUInfo:
		metricsType = MetricTypeCPUInfo
	case *performance.MemoryInfo:
		metricsType = MetricTypeMemoryInfo
	case []performance.DiskInfo:
		metricsType = MetricTypeDiskInfo
	case []performance.NetworkInfo:
		metricsType = MetricTypeNetworkInfo
	case *performance.NUMAStatistics:
		metricsType = MetricTypeNUMAStats
	default:
		// For testing and unknown types, use a generic metric type
		metricsType = MetricType("unknown")
	}

	// Determine event type based on the metric type
	eventType := r.getEventType(metricsType)

	// Create a MetricEvent
	event := MetricEvent{
		Timestamp:   time.Now(),
		Source:      r.source,
		NodeName:    r.nodeName,
		ClusterName: r.clusterName,
		MetricType:  metricsType,
		EventType:   eventType,
		Data:        data,
	}

	// Route the event to all consumers
	return r.publishEvent(event)
}

// Name implements the performance.Receiver interface
func (r *MetricsRouter) Name() string {
	return "metrics-router"
}

// getEventType determines the appropriate event type for a metric
func (r *MetricsRouter) getEventType(metricType MetricType) EventType {
	// Hardware info types are snapshots (static configuration)
	switch metricType {
	case MetricTypeCPUInfo, MetricTypeMemoryInfo, MetricTypeDiskInfo, MetricTypeNetworkInfo:
		return EventTypeSnapshot
	case MetricTypeNUMAStats:
		return EventTypeSnapshot
	}

	// Most performance metrics are gauges (point-in-time measurements)
	// Some like network/disk bytes are counters but we'll handle that
	// in the consumers that need to differentiate
	return EventTypeGauge
}

// RouterStats contains metrics about the event router
type RouterStats struct {
	ConsumerCount int
	Consumers     map[string]ConsumerHealth
}
