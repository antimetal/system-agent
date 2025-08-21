// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tracker

import (
	"context"
	"time"

	"github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/performance"
)

// RuntimeTracker provides a pluggable interface for runtime (container/process) discovery.
// Different implementations can provide different discovery methods (polling, event-driven, eBPF).
type RuntimeTracker interface {
	// Start begins runtime tracking
	Start(ctx context.Context) error
	
	// Stop stops runtime tracking and cleans up resources
	Stop() error
	
	// GetSnapshot returns the current runtime state
	GetSnapshot() (*RuntimeSnapshot, error)
	
	// Events returns a channel of runtime events (for event-driven implementations)
	Events() <-chan RuntimeEvent
	
	// IsEventDriven returns true if this tracker provides real-time events
	IsEventDriven() bool
}

// RuntimeSnapshot contains all runtime information collected at a point in time.
// This maintains compatibility with the existing runtime manager interface.
type RuntimeSnapshot struct {
	Timestamp  time.Time
	Containers []containers.Container
	// Process information will be integrated with existing performance collectors
	ProcessStats *performance.ProcessSnapshot
}

// RuntimeEvent represents a runtime change event (container/process lifecycle)
type RuntimeEvent struct {
	Type      EventType     `json:"type"`
	Timestamp time.Time     `json:"timestamp"`
	Data      interface{}   `json:"data"` // ContainerEvent or ProcessEvent
}

// EventType represents the type of runtime event
type EventType string

const (
	// Container lifecycle events
	EventTypeContainerCreated EventType = "container_created"
	EventTypeContainerDeleted EventType = "container_deleted"
	EventTypeContainerUpdated EventType = "container_updated"
	
	// Process lifecycle events  
	EventTypeProcessCreated EventType = "process_created"
	EventTypeProcessExited  EventType = "process_exited"
	EventTypeProcessUpdated EventType = "process_updated"
	
	// Error events
	EventTypeError EventType = "error"
)

// ContainerEvent represents a container lifecycle event
type ContainerEvent struct {
	Container containers.Container `json:"container"`
	Action    string               `json:"action"` // "created", "deleted", "updated"
}

// ProcessEvent represents a process lifecycle event
type ProcessEvent struct {
	PID    int32  `json:"pid"`
	PPID   int32  `json:"ppid"`
	Action string `json:"action"` // "created", "exited", "updated"
	Path   string `json:"path"`   // Process path that triggered the event
}

// ErrorEvent represents an error during runtime tracking
type ErrorEvent struct {
	Error   error  `json:"error"`
	Context string `json:"context"` // Additional context about the error
}

// TrackerCapabilities describes what tracking methods are available on the system
type TrackerCapabilities struct {
	// FsnotifyAvailable indicates if fsnotify can watch filesystem changes
	FsnotifyAvailable bool
	
	// ProcfsAvailable indicates if /proc filesystem is accessible
	ProcfsAvailable bool
	
	// CgroupfsAvailable indicates if cgroup filesystem is accessible
	CgroupfsAvailable bool
	
	// CanWatchProc indicates if we can watch /proc for process events
	CanWatchProc bool
	
	// CanWatchCgroups indicates if we can watch cgroup directories
	CanWatchCgroups bool
	
	// Limitations contains any detected limitations or warnings
	Limitations []string
}

// TrackerMode specifies which tracking method to use
type TrackerMode string

const (
	// TrackerModeAuto automatically selects the best available tracker
	TrackerModeAuto TrackerMode = "auto"
	
	// TrackerModeEventDriven uses fsnotify-based event tracking
	TrackerModeEventDriven TrackerMode = "event-driven"
	
	// TrackerModePolling uses traditional periodic polling
	TrackerModePolling TrackerMode = "polling"
	
	// TrackerModeEBPF uses eBPF-based kernel event tracking (future)
	TrackerModeEBPF TrackerMode = "ebpf"
)

// TrackerConfig contains configuration for runtime trackers
type TrackerConfig struct {
	// Mode specifies which tracking method to use
	Mode TrackerMode
	
	// CgroupPath is the root cgroup filesystem path
	CgroupPath string
	
	// UpdateInterval is the polling interval (for polling tracker)
	UpdateInterval time.Duration
	
	// EventBufferSize is the size of the event channel buffer
	EventBufferSize int
	
	// DebounceInterval is how long to wait before processing events
	// This helps avoid event storms during rapid container creation/deletion
	DebounceInterval time.Duration
}

// DefaultTrackerConfig returns a sensible default configuration
func DefaultTrackerConfig() TrackerConfig {
	return TrackerConfig{
		Mode:             TrackerModeAuto,
		CgroupPath:       "/sys/fs/cgroup",
		UpdateInterval:   30 * time.Second,
		EventBufferSize:  1000,
		DebounceInterval: 10 * time.Millisecond,
	}
}