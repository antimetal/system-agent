// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
}

type ManagerConfig struct {
	UpdateInterval     time.Duration
	Store              resource.Store
	PerformanceManager *performance.Manager
	CgroupPath         string
}

func NewManager(logger logr.Logger, config ManagerConfig) (*Manager, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("resource store is required")
	}
	if config.PerformanceManager == nil {
		return nil, fmt.Errorf("performance manager is required")
	}

	interval := config.UpdateInterval
	if interval == 0 {
		interval = 30 * time.Second
	}

	// Determine cgroup path, respecting HOST_SYS environment variable
	cgroupPath := config.CgroupPath
	if cgroupPath == "" {
		// Check for HOST_SYS environment variable (for containerized environments)
		if hostSys := os.Getenv("HOST_SYS"); hostSys != "" {
			cgroupPath = filepath.Join(hostSys, "fs", "cgroup")
		} else {
			cgroupPath = "/sys/fs/cgroup"
		}
	}

	return &Manager{
		logger:      logger.WithName("runtime-manager"),
		store:       config.Store,
		perfManager: config.PerformanceManager,
		builder:     graph.NewBuilder(logger, config.Store),
		discovery:   containers.NewDiscovery(cgroupPath),
		interval:    interval,
	}, nil
}

// Start begins runtime discovery and graph building
// Implements controller-runtime's Runnable interface
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting runtime manager", "interval", m.interval)

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
			m.logger.Info("Stopping runtime manager")
			return
		case <-ticker.C:
			if err := m.updateRuntimeGraph(ctx); err != nil {
				m.logger.Error(err, "Failed to update runtime graph")
			}
		}
	}
}

func (m *Manager) updateRuntimeGraph(ctx context.Context) error {
	m.logger.V(1).Info("Updating runtime graph")

	// Collect a snapshot of all runtime information
	snapshot, err := m.collectRuntimeSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("failed to collect runtime snapshot: %w", err)
	}

	// Build the runtime graph from the snapshot
	buildCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := m.builder.BuildFromSnapshot(buildCtx, snapshot); err != nil {
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
	Timestamp    time.Time
	Containers   []containers.Container
	ProcessStats *performance.ProcessSnapshot
}

// Compile-time interface implementation checks
var _ graph.RuntimeSnapshot = (*RuntimeSnapshot)(nil)

// GetContainers implements the graph.RuntimeSnapshot interface
func (s *RuntimeSnapshot) GetContainers() []graph.ContainerInfo {
	containerInfos := make([]graph.ContainerInfo, len(s.Containers))

	for i, container := range s.Containers {
		containerInfo := graph.ContainerInfo{
			ID:            container.ID,
			Runtime:       container.Runtime,
			CgroupVersion: container.CgroupVersion,
			CgroupPath:    container.CgroupPath,
		}

		// TODO: Extract image name/tag from container metadata files
		// The MVP Container struct doesn't include image metadata yet
		// This will be implemented when container metadata extraction is added
		// imageName, imageTag := parseImageNameTag(container.Image)
		// containerInfo.ImageName = imageName
		// containerInfo.ImageTag = imageTag

		// TODO: Extract resource limits from cgroup files
		// TODO: Extract labels from container runtime

		containerInfos[i] = containerInfo
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
		allContainers = []containers.Container{}
	}

	m.logger.V(1).Info("Discovered containers", "count", len(allContainers))

	// Collect process information using the performance manager's configuration
	// Since the performance manager doesn't expose running collectors yet,
	// we create a process collector using the same config and invoke it for one-shot collection
	var processSnapshot *performance.ProcessSnapshot

	if factory, err := performance.GetCollector(performance.MetricTypeProcess); err == nil {
		// Create process collector using the same config as performance manager
		if processCollector, err := factory(m.logger, m.perfManager.GetConfig()); err == nil {
			// Use the collector for a one-shot collection
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			if dataChannel, err := processCollector.Start(ctx); err == nil {
				// Read the first data point and stop
				select {
				case data := <-dataChannel:
					if snapshot, ok := data.(*performance.ProcessSnapshot); ok {
						processSnapshot = snapshot
					}
				case <-ctx.Done():
					m.logger.V(1).Info("Process collection timed out")
				}
				if err := processCollector.Stop(); err != nil {
					m.logger.V(1).Info("Failed to stop process collector", "error", err)
				}
			}
		}
	}

	// Fallback to empty snapshot if collection failed
	if processSnapshot == nil {
		processSnapshot = &performance.ProcessSnapshot{
			Timestamp: startTime,
			Processes: []performance.ProcessStats{},
		}
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

// TODO: parseImageNameTag will be used when container metadata extraction is implemented
// parseImageNameTag parses a Docker/OCI image reference into name and tag components.
// It handles various image reference formats including:
// - Simple names: "nginx" -> ("nginx", "latest")
// - Tagged images: "nginx:1.21" -> ("nginx", "1.21")
// - Registry paths: "docker.io/library/nginx:1.21" -> ("docker.io/library/nginx", "1.21")
// - Images with SHA256 digests: "nginx@sha256:abc123" -> ("nginx@sha256:abc123", "latest")
//
// The function follows these rules:
// - If no tag is present, defaults to "latest"
// - Only the rightmost colon is considered as a tag separator
// - Images with @ (digest) are not parsed for tags
/*
func parseImageNameTag(image string) (name, tag string) {
	if image == "" {
		return "", ""
	}

	// If the image contains @, it has a digest, so don't parse for tag
	if strings.Contains(image, "@") {
		return image, "latest"
	}

	// Find the last colon which could be a tag separator
	lastColonIndex := strings.LastIndex(image, ":")
	if lastColonIndex == -1 {
		// No colon found, so no tag
		return image, "latest"
	}

	// Check if the part after the colon looks like a tag (not a port number)
	potentialTag := image[lastColonIndex+1:]
	potentialName := image[:lastColonIndex]

	// If the potential tag contains a slash, it's likely part of the registry path
	// e.g., "registry.example.com/path/with:colon/image"
	if strings.Contains(potentialTag, "/") {
		return image, "latest"
	}

	// If the potential name ends with a port-like pattern (e.g., "localhost:5000"),
	// check if there are any more path components after the colon
	if strings.Count(potentialName, "/") == 0 && len(potentialTag) <= 5 {
		// Could be a port number (like :5000), check if it's all digits
		isAllDigits := true
		for _, c := range potentialTag {
			if c < '0' || c > '9' {
				isAllDigits = false
				break
			}
		}
		// If it's all digits and short, it might be a port, so look for the next colon
		if isAllDigits && len(potentialTag) <= 5 {
			// This looks like a port, so don't treat it as a tag
			return image, "latest"
		}
	}

	return potentialName, potentialTag
}
*/
