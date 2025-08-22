// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tracker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// EventDrivenTracker implements RuntimeTracker using fsnotify-based event monitoring.
// It combines container and process watchers to provide real-time runtime events.
type EventDrivenTracker struct {
	logger logr.Logger
	config TrackerConfig

	// Event management
	events          chan RuntimeEvent
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	
	// Watchers
	containerWatcher *ContainerWatcher
	processWatcher   *ProcessWatcher
	
	// State management
	mu             sync.RWMutex
	lastSnapshot   *RuntimeSnapshot
	lastUpdateTime time.Time
	
	// For snapshot generation
	discovery *containers.Discovery
}

// NewEventDrivenTracker creates a new event-driven runtime tracker
func NewEventDrivenTracker(logger logr.Logger, config TrackerConfig) (*EventDrivenTracker, error) {
	// Validate configuration
	if config.EventBufferSize <= 0 {
		config.EventBufferSize = 1000
	}
	if config.DebounceInterval <= 0 {
		config.DebounceInterval = 10 * time.Millisecond
	}
	if config.CgroupPath == "" {
		config.CgroupPath = "/sys/fs/cgroup"
	}

	// Create event channel
	events := make(chan RuntimeEvent, config.EventBufferSize)

	// Create container discovery for snapshots
	discovery := containers.NewDiscovery(config.CgroupPath)

	// Create watchers
	containerWatcher, err := NewContainerWatcher(
		logger,
		config.CgroupPath,
		events,
		config.DebounceInterval,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create container watcher: %w", err)
	}

	processWatcher, err := NewProcessWatcher(
		logger,
		events,
		config.DebounceInterval,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create process watcher: %w", err)
	}

	return &EventDrivenTracker{
		logger:           logger.WithName("event-driven-tracker"),
		config:           config,
		events:           events,
		containerWatcher: containerWatcher,
		processWatcher:   processWatcher,
		discovery:        discovery,
	}, nil
}

// Start begins event-driven runtime tracking
func (edt *EventDrivenTracker) Start(ctx context.Context) error {
	edt.logger.Info("Starting event-driven runtime tracker",
		"eventBufferSize", edt.config.EventBufferSize,
		"debounceInterval", edt.config.DebounceInterval)

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	edt.cancel = cancel

	// Do initial snapshot
	if err := edt.updateSnapshot(); err != nil {
		edt.logger.Error(err, "Failed initial runtime snapshot")
		// Don't fail startup on initial snapshot error
	}

	// Start watchers
	if err := edt.containerWatcher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start container watcher: %w", err)
	}

	if err := edt.processWatcher.Start(ctx); err != nil {
		// If process watcher fails, continue with just container watching
		edt.logger.Error(err, "Failed to start process watcher, continuing with container-only tracking")
	}

	// Start event processing
	edt.wg.Add(1)
	go edt.processIncomingEvents(ctx)

	// Start periodic snapshot updates (less frequent than polling tracker)
	edt.wg.Add(1)
	go edt.periodicSnapshotUpdates(ctx)

	return nil
}

// Stop stops the event-driven tracker
func (edt *EventDrivenTracker) Stop() error {
	edt.logger.Info("Stopping event-driven runtime tracker")
	
	if edt.cancel != nil {
		edt.cancel()
	}

	// Stop watchers
	if edt.containerWatcher != nil {
		edt.containerWatcher.Stop()
	}
	if edt.processWatcher != nil {
		edt.processWatcher.Stop()
	}

	// Wait for background tasks
	edt.wg.Wait()

	// Close event channel
	close(edt.events)
	
	return nil
}

// GetSnapshot returns the current runtime state
func (edt *EventDrivenTracker) GetSnapshot() (*RuntimeSnapshot, error) {
	edt.mu.RLock()
	defer edt.mu.RUnlock()

	if edt.lastSnapshot == nil {
		// If no snapshot yet, do a fresh collection
		edt.mu.RUnlock()
		err := edt.updateSnapshot()
		edt.mu.RLock()
		if err != nil {
			return nil, fmt.Errorf("failed to collect runtime snapshot: %w", err)
		}
	}

	// Return a copy to avoid race conditions
	snapshot := &RuntimeSnapshot{
		Timestamp:    edt.lastSnapshot.Timestamp,
		Containers:   make([]containers.Container, len(edt.lastSnapshot.Containers)),
		ProcessStats: edt.lastSnapshot.ProcessStats,
	}
	copy(snapshot.Containers, edt.lastSnapshot.Containers)

	return snapshot, nil
}

// Events returns the channel of runtime events
func (edt *EventDrivenTracker) Events() <-chan RuntimeEvent {
	return edt.events
}

// IsEventDriven returns true for event-driven tracker
func (edt *EventDrivenTracker) IsEventDriven() bool {
	return true
}

// processIncomingEvents handles events from watchers
func (edt *EventDrivenTracker) processIncomingEvents(ctx context.Context) {
	defer edt.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-edt.events:
			if !ok {
				return // Channel closed
			}
			edt.handleRuntimeEvent(event)
		}
	}
}

// handleRuntimeEvent processes individual runtime events
func (edt *EventDrivenTracker) handleRuntimeEvent(event RuntimeEvent) {
	edt.logger.V(1).Info("Processing runtime event", "type", event.Type)

	switch event.Type {
	case EventTypeContainerCreated, EventTypeContainerDeleted, EventTypeContainerUpdated:
		// For container events, refresh our snapshot to stay current
		if err := edt.updateSnapshot(); err != nil {
			edt.logger.Error(err, "Failed to update snapshot after container event")
		}

	case EventTypeProcessCreated, EventTypeProcessExited:
		// Process events are more frequent, so we don't update snapshots for each one
		// Snapshots will be updated periodically

	case EventTypeError:
		// Log error events
		if errorEvent, ok := event.Data.(ErrorEvent); ok {
			edt.logger.Error(errorEvent.Error, "Runtime tracking error", "context", errorEvent.Context)
		}
	}
}

// periodicSnapshotUpdates refreshes snapshots periodically (less frequent than polling)
func (edt *EventDrivenTracker) periodicSnapshotUpdates(ctx context.Context) {
	defer edt.wg.Done()

	// Update snapshots every 60 seconds (less frequent than polling since we have events)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := edt.updateSnapshot(); err != nil {
				edt.logger.Error(err, "Failed periodic snapshot update")
			}
		}
	}
}

// updateSnapshot collects a fresh runtime snapshot
func (edt *EventDrivenTracker) updateSnapshot() error {
	startTime := time.Now()

	// Discover all containers
	allContainers, err := edt.discovery.DiscoverAllContainers()
	if err != nil {
		edt.logger.Error(err, "Failed to discover containers")
		// Continue with empty containers list rather than failing completely
		allContainers = []containers.Container{}
	}

	edt.logger.V(1).Info("Discovered containers", "count", len(allContainers))

	// For now, we'll use a placeholder for process information
	// In a full implementation, this would integrate with performance collectors
	processSnapshot := &performance.ProcessSnapshot{
		Timestamp: startTime,
		Processes: []performance.ProcessStats{},
	}

	snapshot := &RuntimeSnapshot{
		Timestamp:    startTime,
		Containers:   allContainers,
		ProcessStats: processSnapshot,
	}

	// Update stored snapshot
	edt.mu.Lock()
	edt.lastSnapshot = snapshot
	edt.lastUpdateTime = startTime
	edt.mu.Unlock()

	edt.logger.V(1).Info("Runtime snapshot updated",
		"duration", time.Since(startTime),
		"containers", len(allContainers))

	return nil
}

// GetLastUpdateTime returns when the snapshot was last updated
func (edt *EventDrivenTracker) GetLastUpdateTime() time.Time {
	edt.mu.RLock()
	defer edt.mu.RUnlock()
	return edt.lastUpdateTime
}