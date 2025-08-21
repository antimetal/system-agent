// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/antimetal/agent/internal/runtime/graph"
	"github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/resource"
	"github.com/go-logr/logr"
)

// Manager coordinates runtime (container/process) discovery and graph building
type Manager struct {
	logger      logr.Logger
	store       resource.Store
	perfManager *performance.Manager
	builder     *graph.Builder
	discovery   *containers.Discovery

	interval   time.Duration
	lastUpdate time.Time
	mu         sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ManagerConfig contains configuration for the runtime manager
type ManagerConfig struct {
	// UpdateInterval is how often to refresh the runtime graph
	UpdateInterval time.Duration
	// Store is the resource store to write runtime nodes to
	Store resource.Store
	// PerformanceManager is the performance collector manager
	PerformanceManager *performance.Manager
	// CgroupPath is the root cgroup filesystem path (default: /sys/fs/cgroup)
	CgroupPath string
}

// NewManager creates a new runtime manager
func NewManager(logger logr.Logger, config ManagerConfig) (*Manager, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("resource store is required")
	}
	if config.PerformanceManager == nil {
		return nil, fmt.Errorf("performance manager is required")
	}

	// Default to 30 second update interval (more frequent than hardware)
	interval := config.UpdateInterval
	if interval == 0 {
		interval = 30 * time.Second
	}

	// Default cgroup path
	cgroupPath := config.CgroupPath
	if cgroupPath == "" {
		cgroupPath = "/sys/fs/cgroup"
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		logger:      logger.WithName("runtime-manager"),
		store:       config.Store,
		perfManager: config.PerformanceManager,
		builder:     graph.NewBuilder(logger, config.Store),
		discovery:   containers.NewDiscovery(cgroupPath),
		interval:    interval,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// Start begins runtime discovery and graph building
func (m *Manager) Start() error {
	m.logger.Info("Starting runtime manager", "interval", m.interval)

	// Do an initial runtime discovery
	if err := m.updateRuntimeGraph(); err != nil {
		m.logger.Error(err, "Failed initial runtime discovery")
		// Don't fail startup on initial discovery error
	}

	// Start the periodic update goroutine
	m.wg.Add(1)
	go m.runPeriodicUpdates()

	return nil
}

// Stop stops the runtime manager
func (m *Manager) Stop() error {
	m.logger.Info("Stopping runtime manager")
	m.cancel()
	m.wg.Wait()
	return nil
}

// runPeriodicUpdates runs periodic runtime graph updates
func (m *Manager) runPeriodicUpdates() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if err := m.updateRuntimeGraph(); err != nil {
				m.logger.Error(err, "Failed to update runtime graph")
			}
		}
	}
}

// updateRuntimeGraph collects runtime info and updates the graph
func (m *Manager) updateRuntimeGraph() error {
	m.logger.V(1).Info("Updating runtime graph")

	// Collect a snapshot of all runtime information
	snapshot, err := m.collectRuntimeSnapshot()
	if err != nil {
		return fmt.Errorf("failed to collect runtime snapshot: %w", err)
	}

	// Build the runtime graph from the snapshot
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()

	if err := m.builder.BuildFromSnapshot(ctx, snapshot); err != nil {
		return fmt.Errorf("failed to build runtime graph: %w", err)
	}

	// Update last update time
	m.mu.Lock()
	m.lastUpdate = time.Now()
	m.mu.Unlock()

	m.logger.Info("Runtime graph updated successfully")
	return nil
}

// RuntimeSnapshot contains all runtime information collected at a point in time
type RuntimeSnapshot struct {
	Timestamp  time.Time
	Containers []containers.Container
	// Process information will be collected from existing performance collectors
	ProcessStats *performance.ProcessSnapshot
}

// GetContainers implements the graph.RuntimeSnapshot interface
func (s *RuntimeSnapshot) GetContainers() []graph.ContainerInfo {
	containerInfos := make([]graph.ContainerInfo, len(s.Containers))
	
	for i, container := range s.Containers {
		containerInfos[i] = graph.ContainerInfo{
			ID:            container.ID,
			Runtime:       container.Runtime,
			CgroupVersion: container.CgroupVersion,
			CgroupPath:    container.CgroupPath,
			// TODO: Extract image name/tag from container metadata
			// TODO: Extract resource limits from cgroup files
			// TODO: Extract labels from container runtime
		}
	}
	
	return containerInfos
}

// GetProcesses implements the graph.RuntimeSnapshot interface  
func (s *RuntimeSnapshot) GetProcesses() []graph.ProcessInfo {
	if s.ProcessStats == nil {
		return []graph.ProcessInfo{}
	}
	
	processInfos := make([]graph.ProcessInfo, len(s.ProcessStats.Processes))
	
	for i, process := range s.ProcessStats.Processes {
		processInfos[i] = graph.ProcessInfo{
			PID:     process.PID,
			PPID:    process.PPID,
			PGID:    process.PGID,
			SID:     process.SID,
			Command: process.Command,
			State:   process.State,
			// TODO: Add cmdline when available in ProcessStats
		}
	}
	
	return processInfos
}

// collectRuntimeSnapshot collects all runtime information into a snapshot
func (m *Manager) collectRuntimeSnapshot() (*RuntimeSnapshot, error) {
	startTime := time.Now()

	// Discover all containers
	allContainers, err := m.discovery.DiscoverAllContainers()
	if err != nil {
		m.logger.Error(err, "Failed to discover containers")
		// Continue with empty containers list rather than failing completely
		allContainers = []containers.Container{}
	}

	m.logger.V(1).Info("Discovered containers", "count", len(allContainers))

	// Collect process information from performance manager
	// For now, we'll use a placeholder - in a full implementation,
	// we would integrate with the process collector
	processSnapshot := &performance.ProcessSnapshot{
		Timestamp: startTime,
		Processes: []performance.ProcessStats{},
	}

	snapshot := &RuntimeSnapshot{
		Timestamp:    startTime,
		Containers:   allContainers,
		ProcessStats: processSnapshot,
	}

	m.logger.V(1).Info("Runtime snapshot collected successfully",
		"duration", time.Since(startTime),
		"containers", len(allContainers))

	return snapshot, nil
}

// GetLastUpdateTime returns the last time the runtime graph was updated
func (m *Manager) GetLastUpdateTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastUpdate
}

// ForceUpdate triggers an immediate runtime graph update
func (m *Manager) ForceUpdate() error {
	return m.updateRuntimeGraph()
}