// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

// Package containers provides container and process discovery and graph building.
// This package is responsible for discovering runtime resources (containers, processes)
// and building their relationships in the resource graph.
package containers

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	containergraph "github.com/antimetal/agent/internal/containers/graph"
	"github.com/antimetal/agent/internal/resource"
	"github.com/antimetal/agent/pkg/config/environment"
	pkgcontainers "github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// Manager coordinates runtime (container/process) discovery and graph building
type Manager struct {
	logger    logr.Logger
	store     resource.Store
	builder   *containergraph.Builder
	discovery *pkgcontainers.Discovery

	interval   time.Duration
	lastUpdate time.Time
	mu         sync.RWMutex

	// Metrics for monitoring discovery performance
	metrics *DiscoveryMetrics
}

// DiscoveryMetrics tracks performance metrics for runtime discovery
type DiscoveryMetrics struct {
	mu                     sync.RWMutex
	LastDiscoveryDuration  time.Duration
	LastContainerCount     int
	LastProcessCount       int
	LastDiscoveryErrors    int
	TotalDiscoveries       uint64
	TotalDiscoveryErrors   uint64
	LastDiscoveryTimestamp time.Time
}

type ManagerConfig struct {
	UpdateInterval time.Duration
	Store          resource.Store
	CgroupPath     string
}

func NewManager(logger logr.Logger, config ManagerConfig) (*Manager, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("resource store is required")
	}

	interval := config.UpdateInterval
	if interval == 0 {
		interval = defaultUpdateInterval
	}

	// Determine cgroup path using proper host path utilities
	cgroupPath := config.CgroupPath
	if cgroupPath == "" {
		hostPaths := environment.GetHostPaths()
		cgroupPath = filepath.Join(hostPaths.Sys, "fs", "cgroup")
	}

	return &Manager{
		logger:    logger.WithName("containers-manager"),
		store:     config.Store,
		builder:   containergraph.NewBuilder(logger, config.Store),
		discovery: pkgcontainers.NewDiscovery(cgroupPath),
		interval:  interval,
		metrics:   &DiscoveryMetrics{},
	}, nil
}

// Implements sigs.k8s.io/controller-runtime/pkg/manager.LeaderElectionRunnable interface
// This manager runs on every cluster node.
func (m *Manager) NeedLeaderElection() bool {
	return false
}

// Start begins runtime discovery and graph building
// Implements controller-runtime's Runnable interface
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting containers manager", "interval", m.interval)

	// Do an initial runtime discovery
	if err := m.updateRuntimeGraph(ctx); err != nil {
		m.logger.Error(err, "Failed initial runtime discovery")
		// Don't fail startup on initial discovery error
	}

	// Run periodic updates until context is cancelled
	m.runPeriodicUpdates(ctx)

	return nil
}

func (m *Manager) runPeriodicUpdates(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Stopping containers manager")
			return
		case <-ticker.C:
			if err := m.updateRuntimeGraph(ctx); err != nil {
				m.logger.Error(err, "Failed to update runtime graph")
			}
		}
	}
}

func (m *Manager) updateRuntimeGraph(ctx context.Context) error {
	startTime := time.Now()
	m.logger.V(1).Info("Updating runtime graph")

	// Track discovery attempt
	errorCount := 0

	// Collect a snapshot of all runtime information
	snapshot, err := m.collectRuntimeSnapshot(ctx)
	if err != nil {
		errorCount++
		m.updateMetrics(startTime, nil, errorCount)
		return fmt.Errorf("failed to collect runtime snapshot: %w", err)
	}

	// Build the runtime graph from the snapshot
	buildCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := m.builder.BuildFromSnapshot(buildCtx, snapshot); err != nil {
		errorCount++
		m.updateMetrics(startTime, snapshot, errorCount)
		return fmt.Errorf("failed to build runtime graph: %w", err)
	}

	// Update metrics with successful discovery
	m.updateMetrics(startTime, snapshot, errorCount)

	// Update last update time
	m.mu.Lock()
	m.lastUpdate = time.Now()
	m.mu.Unlock()

	// Log with metrics
	duration := time.Since(startTime)
	m.logger.V(1).Info("Runtime graph updated successfully",
		"duration", duration,
		"containers", len(snapshot.Containers),
		"processes", len(snapshot.ProcessStats.Processes))
	return nil
}

// RuntimeSnapshot contains all runtime information collected at a point in time
type RuntimeSnapshot struct {
	Timestamp    time.Time
	Containers   []pkgcontainers.Container
	ProcessStats *performance.ProcessSnapshot
}

// Compile-time interface implementation checks
var _ containergraph.RuntimeSnapshot = (*RuntimeSnapshot)(nil)

// GetContainers implements the containergraph.RuntimeSnapshot interface
func (s *RuntimeSnapshot) GetContainers() []containergraph.ContainerInfo {
	containerInfos := make([]containergraph.ContainerInfo, len(s.Containers))

	for i := range s.Containers {
		container := &s.Containers[i]
		// Note: Metadata extraction errors are silently ignored to allow
		// container discovery to succeed even if metadata files are unavailable
		_ = pkgcontainers.ExtractMetadata(container, "")

		containerInfo := containergraph.ContainerInfo{
			// Discovery fields
			ID:            container.ID,
			Runtime:       container.Runtime,
			CgroupVersion: container.CgroupVersion,
			CgroupPath:    container.CgroupPath,

			// Image information
			ImageName: container.ImageName,
			ImageTag:  container.ImageTag,

			// Human-readable identifiers
			ContainerName: container.ContainerName,
			WorkloadName:  container.WorkloadName,

			// Labels
			Labels: container.Labels,

			// Resource limits
			CPUShares:        container.CPUShares,
			CPUQuotaUs:       container.CPUQuotaUs,
			CPUPeriodUs:      container.CPUPeriodUs,
			MemoryLimitBytes: container.MemoryLimitBytes,
			CpusetCpus:       container.CpusetCpus,
			CpusetMems:       container.CpusetMems,
		}

		containerInfos[i] = containerInfo
	}

	return containerInfos
}

// GetProcesses implements the containergraph.RuntimeSnapshot interface
func (s *RuntimeSnapshot) GetProcesses() []containergraph.ProcessInfo {
	if s.ProcessStats == nil {
		return []containergraph.ProcessInfo{}
	}

	processInfos := make([]containergraph.ProcessInfo, len(s.ProcessStats.Processes))

	for i, process := range s.ProcessStats.Processes {
		processInfos[i] = containergraph.ProcessInfo{
			PID:     process.PID,
			PPID:    process.PPID,
			PGID:    process.PGID,
			SID:     process.SID,
			Command: process.Command,
			Cmdline: process.Cmdline,
			State:   process.State,
		}
	}

	return processInfos
}

func (m *Manager) collectRuntimeSnapshot(ctx context.Context) (*RuntimeSnapshot, error) {
	startTime := time.Now()

	allContainers, err := m.discovery.DiscoverAllContainers()
	if err != nil {
		m.logger.Error(err, "Failed to discover containers")
		// Continue with empty containers list rather than failing completely
		allContainers = []pkgcontainers.Container{}
	}

	m.logger.V(1).Info("Discovered containers", "count", len(allContainers))

	// Log extracted metadata for demonstration (first 3 containers)
	for i := range allContainers {
		if i >= 3 {
			break
		}
		container := &allContainers[i]
		if err := pkgcontainers.ExtractMetadata(container, ""); err == nil {
			m.logger.Info("Container metadata sample",
				"id", container.ID[:min(12, len(container.ID))],
				"runtime", container.Runtime,
				// Container-specific human names
				"container_name", container.ContainerName,
				"workload_name", container.WorkloadName,
				// Image info
				"image", container.ImageName,
				"tag", container.ImageTag,
				// Resource limits
				"has_cpu_limit", container.CPUQuotaUs != nil,
				"has_memory_limit", container.MemoryLimitBytes != nil)
		}
	}

	// For now, we'll use an empty process snapshot
	// TODO: Integrate with performance manager when process collection is available
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

func (m *Manager) GetLastUpdateTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastUpdate
}

func (m *Manager) ForceUpdate(ctx context.Context) error {
	return m.updateRuntimeGraph(ctx)
}

// updateMetrics updates discovery performance metrics
func (m *Manager) updateMetrics(startTime time.Time, snapshot *RuntimeSnapshot, errorCount int) {
	duration := time.Since(startTime)

	m.metrics.mu.Lock()
	defer m.metrics.mu.Unlock()

	m.metrics.LastDiscoveryDuration = duration
	m.metrics.LastDiscoveryTimestamp = startTime
	m.metrics.LastDiscoveryErrors = errorCount
	m.metrics.TotalDiscoveries++

	if errorCount > 0 {
		m.metrics.TotalDiscoveryErrors += uint64(errorCount)
	}

	if snapshot != nil {
		m.metrics.LastContainerCount = len(snapshot.Containers)
		if snapshot.ProcessStats != nil {
			m.metrics.LastProcessCount = len(snapshot.ProcessStats.Processes)
		}
	}
}

// GetMetrics returns a copy of the current discovery metrics
func (m *Manager) GetMetrics() DiscoveryMetrics {
	m.metrics.mu.RLock()
	defer m.metrics.mu.RUnlock()

	// Return a copy to avoid race conditions
	return DiscoveryMetrics{
		LastDiscoveryDuration:  m.metrics.LastDiscoveryDuration,
		LastContainerCount:     m.metrics.LastContainerCount,
		LastProcessCount:       m.metrics.LastProcessCount,
		LastDiscoveryErrors:    m.metrics.LastDiscoveryErrors,
		TotalDiscoveries:       m.metrics.TotalDiscoveries,
		TotalDiscoveryErrors:   m.metrics.TotalDiscoveryErrors,
		LastDiscoveryTimestamp: m.metrics.LastDiscoveryTimestamp,
	}
}
