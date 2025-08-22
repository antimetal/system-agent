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
	"github.com/antimetal/agent/internal/runtime/tracker"
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
	tracker     tracker.RuntimeTracker

	interval   time.Duration
	lastUpdate time.Time
	mu         sync.RWMutex

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ManagerConfig contains configuration for the runtime manager
type ManagerConfig struct {
	// UpdateInterval is how often to refresh the runtime graph (for polling mode)
	UpdateInterval time.Duration
	// Store is the resource store to write runtime nodes to
	Store resource.Store
	// PerformanceManager is the performance collector manager
	PerformanceManager *performance.Manager
	// TrackerConfig contains runtime tracker configuration
	TrackerConfig tracker.TrackerConfig
}

// NewManager creates a new runtime manager
func NewManager(logger logr.Logger, config ManagerConfig) (*Manager, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("resource store is required")
	}
	if config.PerformanceManager == nil {
		return nil, fmt.Errorf("performance manager is required")
	}

	// Validate and setup tracker configuration
	trackerConfig := config.TrackerConfig
	if err := tracker.ValidateConfig(&trackerConfig); err != nil {
		return nil, fmt.Errorf("invalid tracker config: %w", err)
	}

	// Set polling interval if not specified in tracker config
	if trackerConfig.UpdateInterval == 0 {
		interval := config.UpdateInterval
		if interval == 0 {
			interval = 30 * time.Second
		}
		trackerConfig.UpdateInterval = interval
	}

	// Create the appropriate tracker
	runtimeTracker, err := tracker.NewRuntimeTracker(logger, trackerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime tracker: %w", err)
	}

	return &Manager{
		logger:      logger.WithName("runtime-manager"),
		store:       config.Store,
		perfManager: config.PerformanceManager,
		builder:     graph.NewBuilder(logger, config.Store),
		tracker:     runtimeTracker,
		interval:    trackerConfig.UpdateInterval,
	}, nil
}

// Start begins runtime discovery and graph building
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting runtime manager", 
		"tracker_mode", fmt.Sprintf("%T", m.tracker),
		"event_driven", m.tracker.IsEventDriven())

	// Create cancellable context for internal operations
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	// Start the tracker
	if err := m.tracker.Start(ctx); err != nil {
		return fmt.Errorf("failed to start runtime tracker: %w", err)
	}

	// Do an initial runtime discovery
	if err := m.updateRuntimeGraph(ctx); err != nil {
		m.logger.Error(err, "Failed initial runtime discovery")
		// Don't fail startup on initial discovery error
	}

	// Start update management based on tracker type
	if m.tracker.IsEventDriven() {
		// For event-driven trackers, process events and do periodic graph rebuilds
		m.wg.Add(1)
		go m.processRuntimeEvents(ctx)
		
		m.wg.Add(1)
		go m.runPeriodicGraphUpdates(ctx)
	} else {
		// For polling trackers, use the traditional periodic update approach
		m.wg.Add(1)
		go m.runPeriodicUpdates(ctx)
	}

	return nil
}

// Stop stops the runtime manager
func (m *Manager) Stop() error {
	m.logger.Info("Stopping runtime manager")
	
	// Stop the tracker first
	if m.tracker != nil {
		if err := m.tracker.Stop(); err != nil {
			m.logger.Error(err, "Error stopping runtime tracker")
		}
	}
	
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	return nil
}

// runPeriodicUpdates runs periodic runtime graph updates
func (m *Manager) runPeriodicUpdates(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.updateRuntimeGraph(ctx); err != nil {
				m.logger.Error(err, "Failed to update runtime graph")
			}
		}
	}
}

// updateRuntimeGraph collects runtime info and updates the graph
func (m *Manager) updateRuntimeGraph(ctx context.Context) error {
	m.logger.V(1).Info("Updating runtime graph")

	// Get snapshot from tracker
	snapshot, err := m.tracker.GetSnapshot()
	if err != nil {
		return fmt.Errorf("failed to get runtime snapshot: %w", err)
	}

	// Build the runtime graph from the snapshot
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
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


// GetLastUpdateTime returns the last time the runtime graph was updated
func (m *Manager) GetLastUpdateTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastUpdate
}

// processRuntimeEvents handles events from event-driven trackers
func (m *Manager) processRuntimeEvents(ctx context.Context) {
	defer m.wg.Done()

	events := m.tracker.Events()
	if events == nil {
		m.logger.Info("Tracker does not provide events, ending event processing")
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return // Channel closed
			}
			m.handleRuntimeEvent(event)
		}
	}
}

// handleRuntimeEvent processes individual runtime events
func (m *Manager) handleRuntimeEvent(event tracker.RuntimeEvent) {
	m.logger.V(1).Info("Received runtime event", "type", event.Type)

	switch event.Type {
	case tracker.EventTypeContainerCreated, tracker.EventTypeContainerDeleted, tracker.EventTypeContainerUpdated:
		// For container events, rebuild the graph to reflect changes
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := m.updateRuntimeGraph(ctx); err != nil {
			m.logger.Error(err, "Failed to update runtime graph after container event")
		}
		cancel()

	case tracker.EventTypeProcessCreated, tracker.EventTypeProcessExited:
		// Process events are more frequent, so we log them but don't rebuild graph each time
		// The periodic graph updates will capture process changes
		m.logger.V(2).Info("Process event received", "type", event.Type)

	case tracker.EventTypeError:
		if errorEvent, ok := event.Data.(tracker.ErrorEvent); ok {
			m.logger.Error(errorEvent.Error, "Runtime tracking error", "context", errorEvent.Context)
		}
	}
}

// runPeriodicGraphUpdates runs periodic graph rebuilds for event-driven trackers
func (m *Manager) runPeriodicGraphUpdates(ctx context.Context) {
	defer m.wg.Done()

	// For event-driven trackers, rebuild graph less frequently (every 2 minutes)
	// since we get real-time events for container changes
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.updateRuntimeGraph(ctx); err != nil {
				m.logger.Error(err, "Failed periodic runtime graph update")
			}
		}
	}
}

// ForceUpdate triggers an immediate runtime graph update
func (m *Manager) ForceUpdate(ctx context.Context) error {
	return m.updateRuntimeGraph(ctx)
}
