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
	"github.com/antimetal/agent/pkg/performance/collectors"
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

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
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

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		logger:      logger.WithName("hardware-manager"),
		store:       config.Store,
		perfManager: config.PerformanceManager,
		builder:     graph.NewBuilder(logger, config.Store),
		interval:    interval,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// Start begins hardware discovery and graph building
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting hardware manager", "interval", m.interval)

	// Do an initial hardware discovery
	if err := m.updateHardwareGraph(); err != nil {
		m.logger.Error(err, "Failed initial hardware discovery")
		// Don't fail startup on initial discovery error
	}

	// Start the periodic update goroutine
	m.wg.Add(1)
	go m.runPeriodicUpdates()

	return nil
}

// Stop stops the hardware manager
func (m *Manager) Stop() error {
	m.logger.Info("Stopping hardware manager")
	m.cancel()
	m.wg.Wait()
	return nil
}

// runPeriodicUpdates runs periodic hardware graph updates
func (m *Manager) runPeriodicUpdates() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if err := m.updateHardwareGraph(); err != nil {
				m.logger.Error(err, "Failed to update hardware graph")
			}
		}
	}
}

// updateHardwareGraph collects hardware info and updates the graph
func (m *Manager) updateHardwareGraph() error {
	m.logger.V(1).Info("Updating hardware graph")

	// Collect a snapshot of all hardware information
	snapshot, err := m.collectHardwareSnapshot()
	if err != nil {
		return fmt.Errorf("failed to collect hardware snapshot: %w", err)
	}

	// Build the hardware graph from the snapshot
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()

	if err := m.builder.BuildFromSnapshot(ctx, snapshot); err != nil {
		return fmt.Errorf("failed to build hardware graph: %w", err)
	}

	// Update last update time
	m.mu.Lock()
	m.lastUpdate = time.Now()
	m.mu.Unlock()

	m.logger.Info("Hardware graph updated successfully")
	return nil
}

// collectHardwareSnapshot collects all hardware information into a snapshot
func (m *Manager) collectHardwareSnapshot() (*performance.Snapshot, error) {
	// Create collectors with the config from performance manager
	config := m.perfManager.GetConfig()
	ctx := context.Background()

	// Initialize the snapshot
	snapshot := &performance.Snapshot{
		Timestamp:   time.Now(),
		NodeName:    m.perfManager.GetNodeName(),
		ClusterName: m.perfManager.GetClusterName(),
		Metrics:     performance.Metrics{},
		CollectorRun: performance.CollectorRunInfo{
			CollectorStats: make(map[performance.MetricType]performance.CollectorStat),
		},
	}

	startTime := time.Now()

	// Collect hardware-related information
	// We need: CPUInfo, MemoryInfo, DiskInfo, NetworkInfo, NUMAStats

	// CPU Information
	cpuStartTime := time.Now()
	if cpuInfoCollector, err := collectors.NewCPUInfoCollector(m.logger, config); err == nil {
		if cpuInfo, err := cpuInfoCollector.Collect(ctx); err == nil {
			snapshot.Metrics.CPUInfo = cpuInfo.(*performance.CPUInfo)
			snapshot.CollectorRun.CollectorStats[performance.MetricTypeCPUInfo] = performance.CollectorStat{
				Status:   performance.CollectorStatusActive,
				Duration: time.Since(cpuStartTime),
				Data:     cpuInfo,
			}
		} else {
			m.logger.Error(err, "Failed to collect CPU info")
			snapshot.CollectorRun.CollectorStats[performance.MetricTypeCPUInfo] = performance.CollectorStat{
				Status:   performance.CollectorStatusFailed,
				Duration: time.Since(cpuStartTime),
				Error:    err,
			}
		}
	} else {
		m.logger.Error(err, "Failed to create CPU info collector")
	}

	// Memory Information
	memStartTime := time.Now()
	if memInfoCollector, err := collectors.NewMemoryInfoCollector(m.logger, config); err == nil {
		if memInfo, err := memInfoCollector.Collect(ctx); err == nil {
			snapshot.Metrics.MemoryInfo = memInfo.(*performance.MemoryInfo)
			snapshot.CollectorRun.CollectorStats[performance.MetricTypeMemoryInfo] = performance.CollectorStat{
				Status:   performance.CollectorStatusActive,
				Duration: time.Since(memStartTime),
				Data:     memInfo,
			}
		} else {
			m.logger.Error(err, "Failed to collect memory info")
			snapshot.CollectorRun.CollectorStats[performance.MetricTypeMemoryInfo] = performance.CollectorStat{
				Status:   performance.CollectorStatusFailed,
				Duration: time.Since(memStartTime),
				Error:    err,
			}
		}
	} else {
		m.logger.Error(err, "Failed to create memory info collector")
	}

	// Disk Information
	diskStartTime := time.Now()
	if diskInfoCollector, err := collectors.NewDiskInfoCollector(m.logger, config); err == nil {
		if diskInfo, err := diskInfoCollector.Collect(ctx); err == nil {
			snapshot.Metrics.DiskInfo = diskInfo.([]performance.DiskInfo)
			snapshot.CollectorRun.CollectorStats[performance.MetricTypeDiskInfo] = performance.CollectorStat{
				Status:   performance.CollectorStatusActive,
				Duration: time.Since(diskStartTime),
				Data:     diskInfo,
			}
		} else {
			m.logger.Error(err, "Failed to collect disk info")
			snapshot.CollectorRun.CollectorStats[performance.MetricTypeDiskInfo] = performance.CollectorStat{
				Status:   performance.CollectorStatusFailed,
				Duration: time.Since(diskStartTime),
				Error:    err,
			}
		}
	} else {
		m.logger.Error(err, "Failed to create disk info collector")
	}

	// Network Information
	netStartTime := time.Now()
	if netInfoCollector, err := collectors.NewNetworkInfoCollector(m.logger, config); err == nil {
		if netInfo, err := netInfoCollector.Collect(ctx); err == nil {
			snapshot.Metrics.NetworkInfo = netInfo.([]performance.NetworkInfo)
			snapshot.CollectorRun.CollectorStats[performance.MetricTypeNetworkInfo] = performance.CollectorStat{
				Status:   performance.CollectorStatusActive,
				Duration: time.Since(netStartTime),
				Data:     netInfo,
			}
		} else {
			m.logger.Error(err, "Failed to collect network info")
			snapshot.CollectorRun.CollectorStats[performance.MetricTypeNetworkInfo] = performance.CollectorStat{
				Status:   performance.CollectorStatusFailed,
				Duration: time.Since(netStartTime),
				Error:    err,
			}
		}
	} else {
		m.logger.Error(err, "Failed to create network info collector")
	}

	// NUMA Statistics
	numaStartTime := time.Now()
	if numaCollector, err := collectors.NewNUMAStatsCollector(m.logger, config); err == nil {
		if numaStats, err := numaCollector.Collect(ctx); err == nil {
			snapshot.Metrics.NUMAStats = numaStats.(*performance.NUMAStatistics)
			snapshot.CollectorRun.CollectorStats[performance.MetricTypeNUMAStats] = performance.CollectorStat{
				Status:   performance.CollectorStatusActive,
				Duration: time.Since(numaStartTime),
				Data:     numaStats,
			}
		} else {
			m.logger.Error(err, "Failed to collect NUMA stats")
			snapshot.CollectorRun.CollectorStats[performance.MetricTypeNUMAStats] = performance.CollectorStat{
				Status:   performance.CollectorStatusFailed,
				Duration: time.Since(numaStartTime),
				Error:    err,
			}
		}
	} else {
		m.logger.Error(err, "Failed to create NUMA stats collector")
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
func (m *Manager) ForceUpdate() error {
	return m.updateHardwareGraph()
}
