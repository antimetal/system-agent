// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package metrics

// MetricEvent represents a metrics event flowing through the pipeline
//
// MetricType examples:
//   - "system.cpu": CPU utilization metrics
//   - "system.memory": Memory usage metrics
//   - "system.disk": Disk I/O metrics
//   - "system.network": Network interface metrics
//   - "system.process": Process-level metrics
//   - "system.hardware": Hardware topology and capabilities
//
// EventType examples:
//   - "snapshot": Point-in-time metric capture
//   - "delta": Incremental change since last collection
//   - "aggregate": Aggregated metrics over a time window
//
// The Data field contains the actual metric payload, typically one of the
// performance collector types from pkg/performance (LoadStats, MemoryStats,
// DiskStats, NetworkStats, ProcessStats, etc.)
type MetricEvent struct {
	Timestamp   any
	Source      string
	NodeName    string
	ClusterName string
	MetricType  string
	EventType   string
	Data        any // Contains performance types (LoadStats, MemoryStats, etc.)
	Tags        map[string]string
}

// Consumer represents a metrics consumer that processes metric events.
// Lifecycle is managed through the events channel - when it's closed, the consumer stops.
type Consumer interface {
	Name() string
	Start(events <-chan MetricEvent) error
	Health() ConsumerHealth
}

type ConsumerHealth struct {
	Healthy     bool
	LastError   error
	EventsCount uint64
	ErrorsCount uint64
}
