// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !linux

// SPDX-License-Identifier: PolyForm-Shield-1.0.0

package collectors

import (
	"context"
	"errors"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// ProfilerConfig contains configuration for the eBPF profiler (stub implementation)
type ProfilerConfig struct {
	Events     []string `json:"events"`
	SampleRate int      `json:"sampleRate"`
	StackDepth int      `json:"stackDepth"`
}

// DefaultProfilerConfig returns the default profiler configuration (stub implementation)
func DefaultProfilerConfig() *ProfilerConfig {
	return &ProfilerConfig{
		Events:     []string{},
		SampleRate: 99,
		StackDepth: 127,
	}
}

// PerfEventInfo contains metadata about a perf event (stub implementation)
type PerfEventInfo struct {
	Name        string
	Type        uint32
	Config      uint64
	Description string
	Available   bool
	PMURequired bool
}

// ProfilerStatistics contains runtime statistics for the profiler (stub implementation)
type ProfilerStatistics struct {
	EventsReceived    uint64            `json:"eventsReceived"`
	EventsProcessed   uint64            `json:"eventsProcessed"`
	EventsDropped     uint64            `json:"eventsDropped"`
	EventsFiltered    uint64            `json:"eventsFiltered"`
	BytesReceived     uint64            `json:"bytesReceived"`
	BytesProcessed    uint64            `json:"bytesProcessed"`
	ProcessingLatency time.Duration     `json:"processingLatency"`
	RingBufferUsage   float64           `json:"ringBufferUsage"`
	BatchProcessCount uint64            `json:"batchProcessCount"`
	AverageEventSize  uint64            `json:"averageEventSize"`
	ParseErrors       uint64            `json:"parseErrors"`
	RingBufferErrors  uint64            `json:"ringBufferErrors"`
	AttachmentErrors  uint64            `json:"attachmentErrors"`
	ValidationErrors  uint64            `json:"validationErrors"`
	StartTime         time.Time         `json:"startTime"`
	LastEventTime     time.Time         `json:"lastEventTime"`
	UptimeDuration    time.Duration     `json:"uptimeDuration"`
	LastResetTime     time.Time         `json:"lastResetTime"`
	EventTypeStats    map[string]uint64 `json:"eventTypeStats"`
}

// ProfilerCollector stub implementation for non-Linux platforms
type ProfilerCollector struct {
	performance.BaseContinuousCollector
	logger logr.Logger
	config *ProfilerConfig
}

// NewProfilerCollector creates a new eBPF profiler collector (stub implementation)
func NewProfilerCollector(logger logr.Logger, config performance.CollectionConfig, sysPath, procPath string) (*ProfilerCollector, error) {
	// Note: logr.Logger cannot be nil, so we don't check for it

	// Define empty capabilities for stub
	caps := performance.CollectorCapabilities{
		SupportsOneShot:      false,
		SupportsContinuous:   false,
		RequiredCapabilities: nil,
		MinKernelVersion:     "",
	}

	return &ProfilerCollector{
		BaseContinuousCollector: performance.NewBaseContinuousCollector(
			performance.MetricTypeProfiler, // MetricType
			"profiler",                     // Name
			logger,
			config,
			caps,
		),
		logger: logger.WithName("profiler"),
		config: DefaultProfilerConfig(),
	}, nil
}

// Setup configures the profiler with the provided configuration (stub implementation)
func (p *ProfilerCollector) Setup(config interface{}) error {
	profilerConfig, ok := config.(*ProfilerConfig)
	if !ok {
		return errors.New("eBPF profiler not supported on this platform")
	}

	p.config = profilerConfig
	return errors.New("eBPF profiler not supported on this platform")
}

// Start begins continuous profiling (stub implementation)
func (p *ProfilerCollector) Start(ctx context.Context) (<-chan any, error) {
	return nil, errors.New("eBPF profiler not supported on this platform")
}

// Stop halts continuous profiling (stub implementation)
func (p *ProfilerCollector) Stop() error {
	return errors.New("eBPF profiler not supported on this platform")
}

// GetStatistics returns current profiler statistics (stub implementation)
func (p *ProfilerCollector) GetStatistics() *ProfilerStatistics {
	return &ProfilerStatistics{
		EventTypeStats: make(map[string]uint64),
		StartTime:      time.Now(),
	}
}

// ResetStatistics resets all statistics counters (stub implementation)
func (p *ProfilerCollector) ResetStatistics() {
	// No-op for stub implementation
}

// GetAvailableEvents returns all discovered perf events (stub implementation)
func (p *ProfilerCollector) GetAvailableEvents() map[string]*PerfEventInfo {
	return make(map[string]*PerfEventInfo)
}

// ValidateEvents checks if the provided events are available and accessible (stub implementation)
func (p *ProfilerCollector) ValidateEvents(events []string) error {
	return errors.New("eBPF profiler not supported on this platform")
}

// Compile-time interface compliance checks
var (
	_ performance.ContinuousCollector = (*ProfilerCollector)(nil)
)
