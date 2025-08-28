// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package metrics

import (
	"time"
)

// MetricType represents the type of performance metric
type MetricType string

const (
	MetricTypeLoad        MetricType = "load"
	MetricTypeMemory      MetricType = "memory"
	MetricTypeCPU         MetricType = "cpu"
	MetricTypeProcess     MetricType = "process"
	MetricTypeDisk        MetricType = "disk"
	MetricTypeNetwork     MetricType = "network"
	MetricTypeTCP         MetricType = "tcp"
	MetricTypeKernel      MetricType = "kernel"
	MetricTypeSystem      MetricType = "system"
	MetricTypeCPUInfo     MetricType = "cpu_info"
	MetricTypeMemoryInfo  MetricType = "memory_info"
	MetricTypeDiskInfo    MetricType = "disk_info"
	MetricTypeNetworkInfo MetricType = "network_info"
	MetricTypeNUMAStats   MetricType = "numa_stats"
)

// MetricEvent represents a generic metrics event that can be consumed by multiple backends
type MetricEvent struct {
	// Event metadata
	Timestamp   time.Time
	Source      string // e.g., "performance-collector", "kubernetes-controller"
	NodeName    string
	ClusterName string

	// Metric identification
	MetricType MetricType
	EventType  EventType

	// Metric data (contains the actual performance data)
	Data any
}

// EventType indicates the nature of the metric event
type EventType string

const (
	EventTypeGauge     EventType = "gauge"     // Point-in-time value
	EventTypeCounter   EventType = "counter"   // Monotonically increasing value
	EventTypeHistogram EventType = "histogram" // Distribution of values
	EventTypeTiming    EventType = "timing"    // Duration measurements
	EventTypeSet       EventType = "set"       // Unique value counting
	EventTypeSnapshot  EventType = "snapshot"  // Complete snapshot of data
)

// Publisher defines the interface for emitting metrics events
type Publisher interface {
	// Publish emits a metrics event to all registered consumers
	Publish(event MetricEvent) error

	// PublishBatch emits multiple metrics events efficiently
	PublishBatch(events []MetricEvent) error
}
