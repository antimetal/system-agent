// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tracker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/containers"
	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
)

// ContainerWatcher monitors cgroup filesystem for container lifecycle events
type ContainerWatcher struct {
	logger     logr.Logger
	cgroupPath string
	discovery  *containers.Discovery
	
	// Event management
	events      chan RuntimeEvent
	debounce    time.Duration
	lastEvents  map[string]time.Time
	eventsMu    sync.RWMutex
	
	// Filesystem watching
	watcher *fsnotify.Watcher
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewContainerWatcher creates a new container filesystem watcher
func NewContainerWatcher(logger logr.Logger, cgroupPath string, eventChan chan RuntimeEvent, debounce time.Duration) (*ContainerWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	discovery := containers.NewDiscovery(cgroupPath)

	return &ContainerWatcher{
		logger:     logger.WithName("container-watcher"),
		cgroupPath: cgroupPath,
		discovery:  discovery,
		events:     eventChan,
		debounce:   debounce,
		lastEvents: make(map[string]time.Time),
		watcher:    watcher,
	}, nil
}

// Start begins watching the cgroup filesystem for container events
func (cw *ContainerWatcher) Start(ctx context.Context) error {
	cw.logger.Info("Starting container watcher", "cgroupPath", cw.cgroupPath)

	ctx, cancel := context.WithCancel(ctx)
	cw.cancel = cancel

	// Set up filesystem watches
	if err := cw.setupWatches(); err != nil {
		return fmt.Errorf("failed to setup filesystem watches: %w", err)
	}

	// Start event processing
	cw.wg.Add(1)
	go cw.processEvents(ctx)

	return nil
}

// Stop stops the container watcher
func (cw *ContainerWatcher) Stop() error {
	cw.logger.Info("Stopping container watcher")
	
	if cw.cancel != nil {
		cw.cancel()
	}
	
	if cw.watcher != nil {
		cw.watcher.Close()
	}
	
	cw.wg.Wait()
	return nil
}

// setupWatches configures fsnotify to watch relevant cgroup directories
func (cw *ContainerWatcher) setupWatches() error {
	// Watch the root cgroup path
	if err := cw.addWatch(cw.cgroupPath); err != nil {
		return fmt.Errorf("failed to watch root cgroup path: %w", err)
	}

	// Watch common container runtime paths
	containerPaths := []string{
		filepath.Join(cw.cgroupPath, "system.slice"),
		filepath.Join(cw.cgroupPath, "docker"),
		filepath.Join(cw.cgroupPath, "machine.slice"),
		filepath.Join(cw.cgroupPath, "systemd"),
		// For cgroup v2 unified hierarchy
		filepath.Join(cw.cgroupPath, "system.slice/docker-*.scope"),
		filepath.Join(cw.cgroupPath, "system.slice/containerd.service"),
	}

	for _, path := range containerPaths {
		// Use glob pattern matching for dynamic paths
		if strings.Contains(path, "*") {
			matches, err := filepath.Glob(path)
			if err != nil {
				continue // Skip invalid patterns
			}
			for _, match := range matches {
				cw.addWatch(match)
			}
		} else {
			cw.addWatch(path) // Ignore errors for optional paths
		}
	}

	cw.logger.V(1).Info("Container filesystem watches configured")
	return nil
}

// addWatch adds a filesystem watch, ignoring errors for optional paths
func (cw *ContainerWatcher) addWatch(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cw.logger.V(2).Info("Skipping non-existent watch path", "path", path)
		return nil
	}

	if err := cw.watcher.Add(path); err != nil {
		cw.logger.V(1).Info("Failed to add watch", "path", path, "error", err)
		return err
	}

	cw.logger.V(2).Info("Added filesystem watch", "path", path)
	return nil
}

// processEvents handles fsnotify events and converts them to runtime events
func (cw *ContainerWatcher) processEvents(ctx context.Context) {
	defer cw.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}
			cw.handleFilesystemEvent(event)

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			cw.logger.Error(err, "Filesystem watcher error")
			// Send error event
			cw.sendEvent(RuntimeEvent{
				Type:      EventTypeError,
				Timestamp: time.Now(),
				Data: ErrorEvent{
					Error:   err,
					Context: "container filesystem watcher",
				},
			})
		}
	}
}

// handleFilesystemEvent processes individual fsnotify events
func (cw *ContainerWatcher) handleFilesystemEvent(event fsnotify.Event) {
	path := event.Name
	
	// Filter for container-related events
	if !cw.isContainerRelated(path) {
		return
	}

	cw.logger.V(2).Info("Container filesystem event", "path", path, "op", event.Op.String())

	// Apply debouncing to avoid event storms
	if !cw.shouldProcessEvent(path) {
		return
	}

	// Determine event type based on filesystem operation
	var eventType EventType
	switch {
	case event.Op&fsnotify.Create != 0:
		eventType = EventTypeContainerCreated
	case event.Op&fsnotify.Remove != 0:
		eventType = EventTypeContainerDeleted
	case event.Op&fsnotify.Write != 0, event.Op&fsnotify.Chmod != 0:
		eventType = EventTypeContainerUpdated
	default:
		return // Ignore other operations
	}

	// Try to discover container information
	cw.discoverAndSendContainerEvent(eventType, path)
}

// isContainerRelated checks if a filesystem path is related to containers
func (cw *ContainerWatcher) isContainerRelated(path string) bool {
	// Look for container runtime patterns
	containerPatterns := []string{
		"docker-",
		"containerd-",
		"crio-",
		"podman-",
		".scope",
		".service",
		"machine.slice",
	}

	filename := filepath.Base(path)
	for _, pattern := range containerPatterns {
		if strings.Contains(filename, pattern) {
			return true
		}
	}

	// Check if it's in a container-related directory
	containerDirs := []string{
		"docker",
		"containerd",
		"crio",
		"podman",
		"system.slice",
		"machine.slice",
	}

	for _, dir := range containerDirs {
		if strings.Contains(path, filepath.Join(cw.cgroupPath, dir)) {
			return true
		}
	}

	return false
}

// shouldProcessEvent implements debouncing to avoid event storms
func (cw *ContainerWatcher) shouldProcessEvent(path string) bool {
	cw.eventsMu.Lock()
	defer cw.eventsMu.Unlock()

	now := time.Now()
	lastEventTime, exists := cw.lastEvents[path]
	
	if exists && now.Sub(lastEventTime) < cw.debounce {
		return false // Too soon, skip this event
	}

	cw.lastEvents[path] = now
	return true
}

// discoverAndSendContainerEvent attempts to discover container details and send event
func (cw *ContainerWatcher) discoverAndSendContainerEvent(eventType EventType, path string) {
	// For deletions, we can't discover the container anymore, so send a minimal event
	if eventType == EventTypeContainerDeleted {
		cw.sendEvent(RuntimeEvent{
			Type:      eventType,
			Timestamp: time.Now(),
			Data: ContainerEvent{
				Container: containers.Container{
					ID:         cw.extractContainerIDFromPath(path),
					CgroupPath: path,
				},
				Action: "deleted",
			},
		})
		return
	}

	// For creates/updates, try to discover full container information
	allContainers, err := cw.discovery.DiscoverAllContainers()
	if err != nil {
		cw.logger.Error(err, "Failed to discover containers after filesystem event", "path", path)
		return
	}

	// Find the container that matches this path
	for _, container := range allContainers {
		if cw.pathMatchesContainer(path, container) {
			action := "created"
			if eventType == EventTypeContainerUpdated {
				action = "updated"
			}

			cw.sendEvent(RuntimeEvent{
				Type:      eventType,
				Timestamp: time.Now(),
				Data: ContainerEvent{
					Container: container,
					Action:    action,
				},
			})
			return
		}
	}

	cw.logger.V(1).Info("Could not match filesystem event to container", "path", path)
}

// extractContainerIDFromPath attempts to extract container ID from filesystem path
func (cw *ContainerWatcher) extractContainerIDFromPath(path string) string {
	filename := filepath.Base(path)
	
	// Common patterns for container IDs in cgroup paths
	patterns := []string{
		"docker-",
		"containerd-",
		"crio-",
		"podman-",
	}

	for _, pattern := range patterns {
		if strings.HasPrefix(filename, pattern) {
			// Extract ID after pattern and before .scope
			id := strings.TrimPrefix(filename, pattern)
			id = strings.TrimSuffix(id, ".scope")
			id = strings.TrimSuffix(id, ".service")
			return id
		}
	}

	return filename // Fallback to filename
}

// pathMatchesContainer checks if a filesystem path corresponds to a container
func (cw *ContainerWatcher) pathMatchesContainer(path string, container containers.Container) bool {
	// Direct cgroup path match
	if strings.Contains(path, container.CgroupPath) {
		return true
	}

	// Match by container ID in path
	if container.ID != "" && strings.Contains(path, container.ID) {
		return true
	}

	return false
}

// sendEvent sends a runtime event through the channel (non-blocking)
func (cw *ContainerWatcher) sendEvent(event RuntimeEvent) {
	select {
	case cw.events <- event:
		cw.logger.V(1).Info("Sent container event", "type", event.Type)
	default:
		cw.logger.V(1).Info("Event channel full, dropping container event", "type", event.Type)
	}
}