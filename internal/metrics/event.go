// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package metrics

import (
	"time"
)

// DropPolicy determines behavior when a consumer's buffer is full
type DropPolicy string

const (
	DropPolicyOldest DropPolicy = "oldest" // Drop oldest events (default)
	DropPolicyNewest DropPolicy = "newest" // Drop newest events
	DropPolicyBlock  DropPolicy = "block"  // Block until space available
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

// MetricEvent represents a metrics event flowing through the pipeline.
//
// MetricType indicates what kind of metric this is:
//   - "load": System load averages (1/5/15 min)
//   - "memory": Memory usage statistics
//   - "cpu": CPU utilization per core
//   - "process": Process-level metrics
//   - "disk": Disk I/O statistics
//   - "network": Network interface metrics
//   - "tcp": TCP connection statistics
//   - "kernel": Kernel-level metrics
//   - "system": System-wide metrics
//   - "cpu_info": Static CPU information
//   - "memory_info": Static memory information
//   - "disk_info": Static disk information
//   - "network_info": Static network information
//   - "numa_stats": NUMA node statistics
//
// EventType indicates how to interpret the metric:
//   - "gauge": Point-in-time value (e.g., current memory usage)
//   - "counter": Monotonically increasing value (e.g., bytes sent)
//   - "histogram": Distribution of values
//   - "timing": Duration measurements
//   - "set": Unique value counting
//   - "snapshot": Complete state capture
//
// The Data field contains the actual metric payload, typically one of the
// performance collector types from pkg/performance:
//   - *performance.LoadStats for MetricTypeLoad
//   - *performance.MemoryStats for MetricTypeMemory
//   - *performance.CPUStatsList for MetricTypeCPU
//   - *performance.ProcessStatsList for MetricTypeProcess
//   - *performance.DiskStatsList for MetricTypeDisk
//   - *performance.NetworkStatsList for MetricTypeNetwork
//   - *performance.TCPStats for MetricTypeTCP
//   - *performance.KernelStats for MetricTypeKernel
//   - *performance.SystemStats for MetricTypeSystem
//   - *performance.CPUInfo for MetricTypeCPUInfo
//   - *performance.MemoryInfo for MetricTypeMemoryInfo
//   - *performance.DiskInfo for MetricTypeDiskInfo
//   - *performance.NetworkInfo for MetricTypeNetworkInfo
//   - *performance.NUMAStats for MetricTypeNUMAStats
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

// Router defines the interface for routing metrics events to consumers
type Router interface {
	// Publish emits a metrics event to all registered consumers
	Publish(event MetricEvent) error

	// PublishBatch emits multiple metrics events efficiently
	PublishBatch(events []MetricEvent) error
}
