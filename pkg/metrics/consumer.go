// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package metrics

// MetricEvent represents a metrics event flowing through the pipeline
type MetricEvent struct {
	Timestamp   interface{}
	Source      string
	NodeName    string
	ClusterName string
	MetricType  string
	EventType   string
	Data        any // Contains performance types (LoadStats, MemoryStats, etc.)
	Tags        map[string]string
}

type ConsumerInterface interface {
	Name() string
	Start(events <-chan MetricEvent) error
	Stop() error
	Health() ConsumerHealth
}

type ConsumerHealth struct {
	Healthy     bool
	LastError   error
	EventsCount uint64
	ErrorsCount uint64
}
