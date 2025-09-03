//go:build linux

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
	"github.com/antimetal/agent/internal/runtime/tracker"
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
	tracker     tracker.RuntimeTracker

	interval   time.Duration
	lastUpdate time.Time
	mu         sync.RWMutex

	// Metrics for monitoring discovery performance
	metrics *DiscoveryMetrics

	// Event processing
	eventProcessor *EventProcessor
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

// EventProcessor handles events from the tracker
type EventProcessor struct {
	logger  logr.Logger
	builder *graph.Builder
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
	UpdateInterval     time.Duration
	Store              resource.Store
	PerformanceManager *performance.Manager
	CgroupPath         string
	EventBufferSize    int
	DebounceInterval   time.Duration
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

	// Configure eBPF tracker
	trackerConfig := tracker.TrackerConfig{
		ReconciliationInterval: interval,
		EventBufferSize:        config.EventBufferSize,
		DebounceInterval:       config.DebounceInterval,
		CgroupPath:             cgroupPath,
	}

	// Set defaults
	if trackerConfig.EventBufferSize == 0 {
		trackerConfig.EventBufferSize = 10000
	}
	if trackerConfig.DebounceInterval == 0 {
		trackerConfig.DebounceInterval = 100 * time.Millisecond
	}
	if trackerConfig.ReconciliationInterval == 0 {
		trackerConfig.ReconciliationInterval = 5 * time.Minute
	}

	// Create eBPF tracker
	runtimeTracker, err := tracker.NewTracker(logger, trackerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create eBPF tracker: %w", err)
	}

	builder := graph.NewBuilder(logger, config.Store)

	return &Manager{
		logger:      logger.WithName("runtime-manager"),
		store:       config.Store,
		perfManager: config.PerformanceManager,
		builder:     builder,
		discovery:   containers.NewDiscovery(cgroupPath),
		tracker:     runtimeTracker,
		interval:    interval,
		metrics:     &DiscoveryMetrics{},
		eventProcessor: &EventProcessor{
			logger:  logger.WithName("event-processor"),
			builder: builder,
			metrics: &DiscoveryMetrics{},
		},
	}, nil
}

// Start begins runtime discovery and graph building
// Implements controller-runtime's Runnable interface
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting runtime manager with eBPF tracker")

	// Start the tracker
	events, err := m.tracker.Run(ctx)
	if err != nil {
		return fmt.Errorf("failed to start eBPF tracker: %w", err)
	}

	// Create cancellable context
	ctx, m.cancel = context.WithCancel(ctx)

	// Do initial population from snapshot
	if err := m.initialPopulation(ctx); err != nil {
		m.logger.Error(err, "Failed initial population, continuing anyway")
	}

	// Start event processing
	m.wg.Add(1)
	go m.processEvents(ctx, events)

	return nil
}

// Stop gracefully stops the manager
func (m *Manager) Stop() error {
	m.logger.Info("Stopping runtime manager")

	// Cancel context to stop event processing
	if m.cancel != nil {
		m.cancel()
	}

	// Wait for event processing to finish
	m.wg.Wait()

	return nil
}

// initialPopulation performs the initial graph population from a full snapshot
func (m *Manager) initialPopulation(ctx context.Context) error {
	startTime := time.Now()
	m.logger.Info("Performing initial runtime population")

	// Get full snapshot from tracker
	trackerSnapshot, err := m.tracker.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("failed to get initial snapshot: %w", err)
	}

	// Convert tracker snapshot to runtime snapshot for builder
	runtimeSnapshot := m.convertSnapshot(trackerSnapshot)

	// Build initial graph
	buildCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := m.builder.BuildFromSnapshot(buildCtx, runtimeSnapshot); err != nil {
		return fmt.Errorf("failed to build initial graph: %w", err)
	}

	// Update metrics
	m.updateMetrics(startTime, runtimeSnapshot, 0)
	
	m.mu.Lock()
	m.lastUpdate = time.Now()
	m.mu.Unlock()

	m.logger.Info("Initial population completed",
		"duration", time.Since(startTime),
		"containers", len(trackerSnapshot.Containers),
		"processes", len(trackerSnapshot.Processes))

	return nil
}

// processEvents handles events from the tracker
func (m *Manager) processEvents(ctx context.Context, events <-chan tracker.RuntimeEvent) {
	defer m.wg.Done()
	
	for {
		select {
		case <-ctx.Done():
			return
		
		case event, ok := <-events:
			if !ok {
				m.logger.Info("Event channel closed")
				return
			}

			// Process event
			if err := m.eventProcessor.ProcessEvent(ctx, event); err != nil {
				m.logger.Error(err, "Failed to process event",
					"event_type", event.Type,
					"pid", event.ProcessPID,
					"container_id", event.ContainerID)
			}

			// Update last update time
			m.mu.Lock()
			m.lastUpdate = time.Now()
			m.mu.Unlock()
		}
	}
}

// convertSnapshot converts a tracker snapshot to a runtime snapshot
func (m *Manager) convertSnapshot(trackerSnapshot *tracker.RuntimeSnapshot) *RuntimeSnapshot {
	// Convert processes to performance.ProcessStats format
	processes := make([]performance.ProcessStats, len(trackerSnapshot.Processes))
	for i, p := range trackerSnapshot.Processes {
		processes[i] = performance.ProcessStats{
			PID:     p.Pid,
			PPID:    p.Ppid,
			Command: p.Command,
			Cmdline: p.Cmdline,
			State:   p.State,
		}
	}

	// Convert containers to containers.Container format
	containers := make([]containers.Container, len(trackerSnapshot.Containers))
	for i, c := range trackerSnapshot.Containers {
		containers[i] = containers.Container{
			ID:         c.Id,
			Runtime:    c.Runtime,
			CgroupPath: c.CgroupPath,
		}
	}

	return &RuntimeSnapshot{
		Timestamp:  trackerSnapshot.Timestamp,
		Containers: containers,
		ProcessStats: &performance.ProcessSnapshot{
			Timestamp: trackerSnapshot.Timestamp,
			Processes: processes,
		},
	}
}

// ProcessEvent handles a single runtime event
func (ep *EventProcessor) ProcessEvent(ctx context.Context, event tracker.RuntimeEvent) error {
	ep.logger.V(2).Info("Processing runtime event",
		"type", event.Type,
		"timestamp", event.Timestamp)

	switch event.Type {
	case tracker.EventTypeProcessCreate:
		return ep.handleProcessCreate(ctx, event)
	
	case tracker.EventTypeProcessExit:
		return ep.handleProcessExit(ctx, event)
	
	case tracker.EventTypeContainerCreate:
		return ep.handleContainerCreate(ctx, event)
	
	case tracker.EventTypeContainerDestroy:
		return ep.handleContainerDestroy(ctx, event)
	
	default:
		return fmt.Errorf("unknown event type: %v", event.Type)
	}
}

// handleProcessCreate processes a process creation event
func (ep *EventProcessor) handleProcessCreate(ctx context.Context, event tracker.RuntimeEvent) error {
	// TODO: Implement delta update to graph for process creation
	// This will be implemented when graph delta methods are added
	ep.logger.V(2).Info("Process created",
		"pid", event.ProcessPID,
		"ppid", event.ProcessPPID,
		"comm", event.ProcessComm)
	
	// Update metrics
	ep.metrics.mu.Lock()
	ep.metrics.LastProcessCount++
	ep.metrics.mu.Unlock()

	return nil
}

// handleProcessExit processes a process exit event
func (ep *EventProcessor) handleProcessExit(ctx context.Context, event tracker.RuntimeEvent) error {
	// TODO: Implement delta update to graph for process removal
	ep.logger.V(2).Info("Process exited",
		"pid", event.ProcessPID)
	
	// Update metrics
	ep.metrics.mu.Lock()
	if ep.metrics.LastProcessCount > 0 {
		ep.metrics.LastProcessCount--
	}
	ep.metrics.mu.Unlock()

	return nil
}

// handleContainerCreate processes a container creation event
func (ep *EventProcessor) handleContainerCreate(ctx context.Context, event tracker.RuntimeEvent) error {
	// TODO: Implement delta update to graph for container creation
	ep.logger.V(2).Info("Container created",
		"container_id", event.ContainerID,
		"runtime", event.ContainerRuntime,
		"cgroup_path", event.CgroupPath)
	
	// Update metrics
	ep.metrics.mu.Lock()
	ep.metrics.LastContainerCount++
	ep.metrics.mu.Unlock()

	return nil
}

// handleContainerDestroy processes a container destruction event
func (ep *EventProcessor) handleContainerDestroy(ctx context.Context, event tracker.RuntimeEvent) error {
	// TODO: Implement delta update to graph for container removal
	ep.logger.V(2).Info("Container destroyed",
		"container_id", event.ContainerID)
	
	// Update metrics
	ep.metrics.mu.Lock()
	if ep.metrics.LastContainerCount > 0 {
		ep.metrics.LastContainerCount--
	}
	ep.metrics.mu.Unlock()

	return nil
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
	m.logger.Info("Runtime graph updated successfully",
		"duration", duration,
		"containers", len(snapshot.Containers),
		"processes", len(snapshot.ProcessStats.Processes))
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
				// Ensure collector is always stopped to prevent resource leaks
				defer func() {
					if err := processCollector.Stop(); err != nil {
						m.logger.V(1).Info("Failed to stop process collector", "error", err)
					}
				}()

				// Read the first data point
				select {
				case data := <-dataChannel:
					if snapshot, ok := data.(*performance.ProcessSnapshot); ok {
						processSnapshot = snapshot
					}
				case <-ctx.Done():
					m.logger.V(1).Info("Process collection timed out")
					// Stop() will be called by the defer above
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
	return m.initialPopulation(ctx)
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

	// Merge metrics from event processor
	epMetrics := m.eventProcessor.metrics
	epMetrics.mu.RLock()
	defer epMetrics.mu.RUnlock()

	// Return a copy to avoid race conditions
	return DiscoveryMetrics{
		LastDiscoveryDuration:  m.metrics.LastDiscoveryDuration,
		LastContainerCount:     epMetrics.LastContainerCount,
		LastProcessCount:       epMetrics.LastProcessCount,
		LastDiscoveryErrors:    m.metrics.LastDiscoveryErrors,
		TotalDiscoveries:       m.metrics.TotalDiscoveries,
		TotalDiscoveryErrors:   m.metrics.TotalDiscoveryErrors,
		LastDiscoveryTimestamp: m.metrics.LastDiscoveryTimestamp,
	}
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
