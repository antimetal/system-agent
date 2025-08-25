// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtime

import (
	"context"
	"fmt"
	"strings"
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
// Implements controller-runtime's Runnable interface - blocks until context is cancelled
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting runtime manager",
		"tracker_mode", fmt.Sprintf("%T", m.tracker),
		"event_driven", m.tracker.IsEventDriven())

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
		return m.runEventDrivenMode(ctx)
	} else {
		// For polling trackers, use the traditional periodic update approach
		return m.runPollingMode(ctx)
	}
}

// runPollingMode runs the manager in polling mode (blocks until context is cancelled)
func (m *Manager) runPollingMode(ctx context.Context) error {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Stopping runtime manager (polling mode)")
			if err := m.tracker.Stop(); err != nil {
				m.logger.Error(err, "Error stopping runtime tracker")
			}
			return nil
		case <-ticker.C:
			if err := m.updateRuntimeGraph(ctx); err != nil {
				m.logger.Error(err, "Failed to update runtime graph")
			}
		}
	}
}

// runEventDrivenMode runs the manager in event-driven mode (blocks until context is cancelled)
func (m *Manager) runEventDrivenMode(ctx context.Context) error {
	// For event-driven trackers, handle events and do periodic graph rebuilds
	events := m.tracker.Events()
	if events == nil {
		m.logger.Info("Tracker does not provide events, falling back to polling mode")
		return m.runPollingMode(ctx)
	}

	// Periodic graph rebuild ticker (less frequent for event-driven mode)
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Stopping runtime manager (event-driven mode)")
			if err := m.tracker.Stop(); err != nil {
				m.logger.Error(err, "Error stopping runtime tracker")
			}
			return nil

		case event, ok := <-events:
			if !ok {
				m.logger.Info("Event channel closed, stopping runtime manager")
				return nil
			}
			m.handleRuntimeEvent(ctx, event)

		case <-ticker.C:
			if err := m.updateRuntimeGraph(ctx); err != nil {
				m.logger.Error(err, "Failed periodic runtime graph update")
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


// handleRuntimeEvent processes individual runtime events
func (m *Manager) handleRuntimeEvent(ctx context.Context, event tracker.RuntimeEvent) {
	m.logger.V(1).Info("Received runtime event", "type", event.Type)

	switch event.Type {
	case tracker.EventTypeContainerCreated, tracker.EventTypeContainerDeleted, tracker.EventTypeContainerUpdated:
		// For container events, rebuild the graph to reflect changes
		updateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := m.updateRuntimeGraph(updateCtx); err != nil {
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


// ForceUpdate triggers an immediate runtime graph update
func (m *Manager) ForceUpdate(ctx context.Context) error {
	return m.updateRuntimeGraph(ctx)
}

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
