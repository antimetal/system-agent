// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package hardware

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/antimetal/agent/internal/hardware/graph"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/resource"
	"github.com/go-logr/logr"
)

// Manager coordinates hardware discovery and graph building
type Manager struct {
	logger      logr.Logger
	store       resource.Store
	perfManager *performance.Manager
	builder     *graph.Builder

	interval   time.Duration
	lastUpdate time.Time
	mu         sync.RWMutex
}

// ManagerConfig contains configuration for the hardware manager
type ManagerConfig struct {
	// UpdateInterval is how often to refresh the hardware graph
	UpdateInterval time.Duration
	// Store is the resource store to write hardware nodes to
	Store resource.Store
	// PerformanceManager is the performance collector manager
	PerformanceManager *performance.Manager
}

// NewManager creates a new hardware manager
func NewManager(logger logr.Logger, config ManagerConfig) (*Manager, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("resource store is required")
	}
	if config.PerformanceManager == nil {
		return nil, fmt.Errorf("performance manager is required")
	}

	// Default to 5 minute update interval
	interval := config.UpdateInterval
	if interval == 0 {
		interval = 5 * time.Minute
	}

	return &Manager{
		logger:      logger.WithName("hardware-manager"),
		store:       config.Store,
		perfManager: config.PerformanceManager,
		builder:     graph.NewBuilder(logger, config.Store),
		interval:    interval,
	}, nil
}

// Start begins hardware discovery and graph building
// Implements controller-runtime's Runnable interface
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting hardware manager", "interval", m.interval)

	// Do an initial hardware discovery
	if err := m.updateHardwareGraph(ctx); err != nil {
		m.logger.Error(err, "Failed initial hardware discovery")
		// Don't fail startup on initial discovery error
	}

	// Run periodic updates until context is cancelled
	m.runPeriodicUpdates(ctx)

	return nil
}

// runPeriodicUpdates runs periodic hardware graph updates
func (m *Manager) runPeriodicUpdates(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Stopping hardware manager")
			return
		case <-ticker.C:
			if err := m.updateHardwareGraph(ctx); err != nil {
				m.logger.Error(err, "Failed to update hardware graph")
			}
		}
	}
}

// updateHardwareGraph collects hardware info and updates the graph
func (m *Manager) updateHardwareGraph(ctx context.Context) error {
	m.logger.V(1).Info("Updating hardware graph")

	// Create a hardware graph receiver for this collection session
	nodeName := m.perfManager.GetNodeName()
	clusterName := m.perfManager.GetClusterName()
	receiver := graph.NewHardwareGraphReceiver(m.logger, m.builder, nodeName, clusterName)

	// Collect hardware information directly to the receiver
	if err := m.collectHardwareWithReceiver(ctx, receiver); err != nil {
		return fmt.Errorf("failed to collect hardware: %w", err)
	}

	// Build the graph from the collected data
	buildCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	snapshot, err := receiver.GetSnapshot(buildCtx)
	if err != nil {
		return fmt.Errorf("failed to get snapshot: %w", err)
	}
	if snapshot != nil {
		if err := m.builder.BuildFromSnapshot(buildCtx, snapshot); err != nil {
			return fmt.Errorf("failed to build hardware graph: %w", err)
		}
	}

	// Update last update time
	m.mu.Lock()
	m.lastUpdate = time.Now()
	m.mu.Unlock()

	m.logger.V(1).Info("Hardware graph updated successfully")
	return nil
}

// collectHardwareWithReceiver collects hardware information using the receiver pattern
func (m *Manager) collectHardwareWithReceiver(ctx context.Context, receiver *graph.HardwareGraphReceiver) error {
	// Create collectors with the config from performance manager
	config := m.perfManager.GetConfig()

	startTime := time.Now()

	// Define hardware collectors we need from the registry
	// These are all registered as OnceContinuousCollectors for one-shot collection
	hardwareMetrics := []performance.MetricType{
		performance.MetricTypeCPUInfo,
		performance.MetricTypeMemoryInfo,
		performance.MetricTypeDiskInfo,
		performance.MetricTypeNetworkInfo,
		performance.MetricTypeNUMAStats,
	}

	// Collect from each available collector using the registry
	for _, metricType := range hardwareMetrics {
		collectorStartTime := time.Now()

		// Get collector factory from registry
		factory, err := performance.GetCollector(metricType)
		if err != nil {
			// Check if it's unavailable due to platform constraints
			available, reason := performance.GetCollectorStatus(metricType)
			if !available {
				m.logger.V(1).Info("Collector not available",
					"metric_type", metricType, "reason", reason)
			} else {
				m.logger.Error(err, "Failed to get collector from registry",
					"metric_type", metricType)
			}
			continue
		}

		// Create the continuous collector instance
		collector, err := factory(m.logger, config)
		if err != nil {
			m.logger.Error(err, "Failed to create collector",
				"metric_type", metricType)
			continue
		}

		// Start the collector with the receiver
		if err := collector.Start(ctx, receiver); err != nil {
			m.logger.Error(err, "Failed to start collector",
				"metric_type", metricType)
			continue
		}

		m.logger.V(2).Info("Collected hardware metric",
			"metric_type", metricType,
			"duration", time.Since(collectorStartTime))
	}

	m.logger.V(1).Info("Hardware collection completed",
		"duration", time.Since(startTime),
		"collectors", len(hardwareMetrics))

	return nil
}

// GetLastUpdateTime returns the last time the hardware graph was updated
func (m *Manager) GetLastUpdateTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastUpdate
}

// ForceUpdate triggers an immediate hardware graph update
func (m *Manager) ForceUpdate(ctx context.Context) error {
	return m.updateHardwareGraph(ctx)
}
