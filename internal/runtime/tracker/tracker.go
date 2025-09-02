//go:build linux

// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tracker

import (
	"context"
	"time"

	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
)

// EventType represents the type of runtime event
type EventType int

const (
	EventTypeProcessCreate EventType = iota
	EventTypeProcessExit
	EventTypeContainerCreate
	EventTypeContainerDestroy
)

// RuntimeEvent represents a change in the runtime topology
type RuntimeEvent struct {
	Type      EventType
	Timestamp time.Time

	// Process events
	ProcessPID  int32
	ProcessPPID int32
	ProcessComm string
	ProcessArgs []string
	ProcessUID  uint32
	ProcessGID  uint32

	// Container events
	ContainerID      string
	ContainerRuntime runtimev1.ContainerRuntime
	CgroupPath       string
}

// RuntimeSnapshot represents the full state of runtime at a point in time
type RuntimeSnapshot struct {
	Timestamp  time.Time
	Containers []*runtimev1.ContainerNode
	Processes  []*runtimev1.ProcessNode
}

// TrackerConfig configures the runtime tracker
type TrackerConfig struct {
	// ReconciliationInterval is the interval for full reconciliation scans
	ReconciliationInterval time.Duration

	// EventBufferSize is the size of the event channel buffer
	EventBufferSize int

	// DebounceInterval is the time to wait before processing batched events
	DebounceInterval time.Duration

	// CgroupPath is the root cgroup path
	CgroupPath string
}

// RuntimeTracker is the interface for runtime discovery mechanisms
type RuntimeTracker interface {
	// Run starts tracking runtime changes and blocks until context is cancelled
	// Events should be sent to the returned channel
	Run(ctx context.Context) (<-chan RuntimeEvent, error)

	// Snapshot returns the current full state of runtime
	// This is used for initial population and periodic reconciliation
	Snapshot(ctx context.Context) (*RuntimeSnapshot, error)
}