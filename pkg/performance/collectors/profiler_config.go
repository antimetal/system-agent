// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import "time"

// ProfilerEventType represents the type of perf event to profile
type ProfilerEventType int

const (
	ProfilerEventCPUCycles ProfilerEventType = iota
	ProfilerEventCacheMisses
	ProfilerEventCPUClock
	ProfilerEventPageFaults
)

// String returns human-readable event type name
func (t ProfilerEventType) String() string {
	switch t {
	case ProfilerEventCPUCycles:
		return "cpu-cycles"
	case ProfilerEventCacheMisses:
		return "cache-misses"
	case ProfilerEventCPUClock:
		return "cpu-clock"
	case ProfilerEventPageFaults:
		return "page-faults"
	default:
		return "unknown"
	}
}

// ProfilerConfig specifies how the profiler should be configured
type ProfilerConfig struct {
	// Event configuration:
	Event PerfEventConfig // Perf event to profile (required)

	// Optional configuration:
	Interval time.Duration // Profile collection interval (optional, uses CollectionConfig.Interval if 0)
}

// ProfilerSetup interface for configuring profiler before starting
type ProfilerSetup interface {
	Setup(config ProfilerConfig) error
}

// PerfEventConfig defines a perf event configuration
type PerfEventConfig struct {
	Name         string // Human-readable name
	Type         uint32 // PERF_TYPE_*
	Config       uint64 // Event-specific config
	SamplePeriod uint64 // Sample every N events (or nanoseconds) (required, must be > 0)
}

// StackKeyEvent represents the key for stack count map
type StackKeyEvent struct {
	PID           int32
	TID           int32
	UserStackId   int32
	KernelStackId int32
}

// StackCountEvent represents stack count value
type StackCountEvent struct {
	Count uint64
	Cpu   uint32
	_     uint32 // Padding
}

// Profile event flags
const (
	ProfileFlagUserStackTruncated   = 1 << 0
	ProfileFlagKernelStackTruncated = 1 << 1
	ProfileFlagStackCollision       = 1 << 2
)
