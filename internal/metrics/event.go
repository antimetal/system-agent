// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package metrics

import (
	"time"
)

// MetricType represents the type of performance metric.
// IMPORTANT: This type is duplicated from pkg/performance/types.go to avoid import cycles.
// The performance package needs to import internal/metrics for the Router interface,
// so we cannot import performance here without creating a circular dependency.
// Any updates to MetricType constants must be synchronized between both files.
type MetricType string

const (
	// Runtime System Statistics
	MetricTypeLoad      MetricType = "load"
	MetricTypeMemory    MetricType = "memory"
	MetricTypeCPU       MetricType = "cpu"
	MetricTypeProcess   MetricType = "process"
	MetricTypeDisk      MetricType = "disk"
	MetricTypeNetwork   MetricType = "network"
	MetricTypeTCP       MetricType = "tcp"
	MetricTypeKernel    MetricType = "kernel"
	MetricTypeSystem    MetricType = "system"
	MetricTypeNUMAStats MetricType = "numa_stats"
	// Runtime Container Statistics
	MetricTypeCgroupCPU     MetricType = "cgroup_cpu"
	MetricTypeCgroupMemory  MetricType = "cgroup_memory"
	MetricTypeCgroupIO      MetricType = "cgroup_io"      // Future
	MetricTypeCgroupNetwork MetricType = "cgroup_network" // Future
	// Hardware configuration collectors
	MetricTypeCPUInfo     MetricType = "cpu_info"
	MetricTypeMemoryInfo  MetricType = "memory_info"
	MetricTypeDiskInfo    MetricType = "disk_info"
	MetricTypeNetworkInfo MetricType = "network_info"
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
// The Data field contains the actual metric payload, typically one of the
// performance collector types from pkg/performance (all using pointers for efficiency):
//   - *performance.LoadStats for MetricTypeLoad
//   - *performance.MemoryStats for MetricTypeMemory
//   - []*performance.CPUStats for MetricTypeCPU
//   - []*performance.ProcessStats for MetricTypeProcess
//   - []*performance.DiskStats for MetricTypeDisk
//   - []*performance.NetworkStats for MetricTypeNetwork
//   - *performance.TCPStats for MetricTypeTCP
//   - []*performance.KernelMessage for MetricTypeKernel
//   - *performance.SystemStats for MetricTypeSystem
//   - *performance.CPUInfo for MetricTypeCPUInfo
//   - *performance.MemoryInfo for MetricTypeMemoryInfo
//   - []*performance.DiskInfo for MetricTypeDiskInfo
//   - []*performance.NetworkInfo for MetricTypeNetworkInfo
//   - *performance.NUMAStatistics for MetricTypeNUMAStats
//   - []*performance.CgroupCPUStats for MetricTypeCgroupCPU
//   - []*performance.CgroupMemoryStats for MetricTypeCgroupMemory
//
// Note: Consumers determine how to interpret metrics (gauge, counter, histogram, etc.)
// based on the actual field semantics within the Data payload, as many metric types
// contain mixed data (e.g., network stats have both gauges and counters).
type MetricEvent struct {
	// Event metadata
	Timestamp   time.Time
	Source      string // e.g., "performance-collector", "kubernetes-controller"
	NodeName    string
	ClusterName string

	// Metric identification
	MetricType MetricType

	// Metric data (contains the actual performance data)
	Data any
}

// Router defines the interface for routing metrics events to consumers
type Router interface {
	// Publish emits a metrics event to all registered consumers
	Publish(event MetricEvent) error

	// PublishBatch emits multiple metrics events efficiently
	PublishBatch(events []MetricEvent) error
}
