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

	hardwaregraph "github.com/antimetal/agent/internal/hardware/graph"
	"github.com/antimetal/agent/internal/hardware/types"
	"github.com/antimetal/agent/internal/resource"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// Manager coordinates hardware discovery and graph building
type Manager struct {
	logger  logr.Logger
	store   resource.Store
	builder *hardwaregraph.Builder

	// Configuration
	collectionConfig performance.CollectionConfig
	nodeName         string
	clusterName      string

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
	// CollectionConfig is the performance collection configuration for paths
	CollectionConfig performance.CollectionConfig
	// NodeName is the name of the node
	NodeName string
	// ClusterName is the name of the cluster
	ClusterName string
}

// NewManager creates a new hardware manager
func NewManager(logger logr.Logger, config ManagerConfig) (*Manager, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("resource store is required")
	}

	// Apply defaults to collection config
	config.CollectionConfig.ApplyDefaults()

	// Default to 5 minute update interval
	interval := config.UpdateInterval
	if interval == 0 {
		interval = defaultUpdateInterval
	}

	return &Manager{
		logger:           logger.WithName("hardware-manager"),
		store:            config.Store,
		builder:          hardwaregraph.NewBuilder(logger, config.Store),
		collectionConfig: config.CollectionConfig,
		nodeName:         config.NodeName,
		clusterName:      config.ClusterName,
		interval:         interval,
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

// Implements sigs.k8s.io/controller-runtime/pkg/manager.LeaderElectionRunnable interface
// This manager runs on every cluster node.
func (m *Manager) NeedLeaderElection() bool {
	return false
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

	// Collect a snapshot of all hardware information
	snapshot, err := m.collectHardwareSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("failed to collect hardware snapshot: %w", err)
	}

	// Build the hardware graph from the snapshot
	buildCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := m.builder.BuildFromSnapshot(buildCtx, snapshot); err != nil {
		return fmt.Errorf("failed to build hardware graph: %w", err)
	}

	// Update last update time
	m.mu.Lock()
	m.lastUpdate = time.Now()
	m.mu.Unlock()

	m.logger.V(1).Info("Hardware graph updated successfully")
	return nil
}

// collectHardwareSnapshot collects all hardware information into a snapshot
func (m *Manager) collectHardwareSnapshot(ctx context.Context) (*types.Snapshot, error) {
	// Initialize the snapshot
	snapshot := &types.Snapshot{
		Timestamp:   time.Now(),
		NodeName:    m.nodeName,
		ClusterName: m.clusterName,
		CollectorRun: performance.CollectorRunInfo{
			CollectorStats: make(map[performance.MetricType]performance.CollectorStat),
		},
	}

	startTime := time.Now()

	// Define hardware collectors we need from the registry
	// These are all registered as OnceContinuousCollectors for one-shot collection
	hardwareMetrics := []performance.MetricType{
		performance.MetricTypeCPUInfo,
		performance.MetricTypeMemoryInfo,
		performance.MetricTypeDiskInfo,
		performance.MetricTypeNetworkInfo,
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
		collector, err := factory(m.logger, m.collectionConfig)
		if err != nil {
			m.logger.Error(err, "Failed to create collector",
				"metric_type", metricType)
			snapshot.CollectorRun.CollectorStats[metricType] = performance.CollectorStat{
				Status:   performance.CollectorStatusFailed,
				Duration: time.Since(collectorStartTime),
				Error:    err,
			}
			continue
		}

		// Verify that hardware collectors are OnceContinuousCollectors
		if _, ok := collector.(*performance.OnceContinuousCollector); !ok {
			m.logger.Error(nil, "Hardware collector is not an OnceContinuousCollector",
				"metric_type", metricType, "collector_type", fmt.Sprintf("%T", collector))
			snapshot.CollectorRun.CollectorStats[metricType] = performance.CollectorStat{
				Status:   performance.CollectorStatusFailed,
				Duration: time.Since(collectorStartTime),
				Error:    fmt.Errorf("expected OnceContinuousCollector, got %T", collector),
			}
			continue
		}

		// Start the collector - for OnceContinuousCollector this does a one-shot collection
		dataChan, err := collector.Start(ctx)
		if err != nil {
			m.logger.Error(err, "Failed to start collector",
				"metric_type", metricType)
			snapshot.CollectorRun.CollectorStats[metricType] = performance.CollectorStat{
				Status:   performance.CollectorStatusFailed,
				Duration: time.Since(collectorStartTime),
				Error:    err,
			}
			continue
		}

		// Read the one-shot data from the channel
		data := <-dataChan

		// Store the collected data in the snapshot based on type
		switch metricType {
		case performance.MetricTypeCPUInfo:
			snapshot.CPUInfo = data.Data.(*performance.CPUInfo)
		case performance.MetricTypeMemoryInfo:
			snapshot.MemoryInfo = data.Data.(*performance.MemoryInfo)
		case performance.MetricTypeDiskInfo:
			snapshot.DiskInfo = data.Data.([]*performance.DiskInfo)
		case performance.MetricTypeNetworkInfo:
			snapshot.NetworkInfo = data.Data.([]*performance.NetworkInfo)
		}

		snapshot.CollectorRun.CollectorStats[metricType] = performance.CollectorStat{
			Status:   performance.CollectorStatusActive,
			Duration: time.Since(collectorStartTime),
		}
	}

	snapshot.CollectorRun.Duration = time.Since(startTime)
	m.logger.V(1).Info("Hardware snapshot collected successfully",
		"duration", snapshot.CollectorRun.Duration,
		"collectors", len(snapshot.CollectorRun.CollectorStats))

	return snapshot, nil
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
