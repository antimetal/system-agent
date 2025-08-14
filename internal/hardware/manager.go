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
func (m *Manager) Start() error {
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
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	snapshot := &performance.Snapshot{
		Timestamp: time.Now(),
		Metrics:   performance.Metrics{},
	}

	// Collect CPU info
	if collector, err := m.perfManager.GetCollector(performance.MetricTypeCPUInfo); err == nil {
		if data, err := collector.Collect(ctx); err == nil {
			if cpuInfo, ok := data.(*performance.CPUInfo); ok {
				snapshot.Metrics.CPUInfo = cpuInfo
			}
		} else {
			m.logger.V(1).Info("Failed to collect CPU info", "error", err)
		}
	}

	// Collect memory info
	if collector, err := m.perfManager.GetCollector(performance.MetricTypeMemoryInfo); err == nil {
		if data, err := collector.Collect(ctx); err == nil {
			if memInfo, ok := data.(*performance.MemoryInfo); ok {
				snapshot.Metrics.MemoryInfo = memInfo
			}
		} else {
			m.logger.V(1).Info("Failed to collect memory info", "error", err)
		}
	}

	// Collect disk info
	if collector, err := m.perfManager.GetCollector(performance.MetricTypeDiskInfo); err == nil {
		if data, err := collector.Collect(ctx); err == nil {
			if diskInfo, ok := data.([]performance.DiskInfo); ok {
				snapshot.Metrics.DiskInfo = diskInfo
			}
		} else {
			m.logger.V(1).Info("Failed to collect disk info", "error", err)
		}
	}

	// Collect network info
	if collector, err := m.perfManager.GetCollector(performance.MetricTypeNetworkInfo); err == nil {
		if data, err := collector.Collect(ctx); err == nil {
			if netInfo, ok := data.([]performance.NetworkInfo); ok {
				snapshot.Metrics.NetworkInfo = netInfo
			}
		} else {
			m.logger.V(1).Info("Failed to collect network info", "error", err)
		}
	}

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
