// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package debug

import "time"

// LogEntry represents a structured log entry for JSON output
type LogEntry struct {
	Timestamp time.Time           `json:"timestamp,omitempty"`
	Level     string              `json:"level"`
	Consumer  string              `json:"consumer"`
	Message   string              `json:"message"`
	Event     *MetricEventSummary `json:"event,omitempty"`
	Data      interface{}         `json:"data,omitempty"`
	Tags      map[string]string   `json:"tags,omitempty"`
	Stats     *ConsumerStats      `json:"stats,omitempty"`
}

// MetricEventSummary provides a condensed view of metric events for logging
type MetricEventSummary struct {
	MetricType  string `json:"metric_type"`
	Source      string `json:"source"`
	NodeName    string `json:"node_name"`
	ClusterName string `json:"cluster_name"`
	DataType    string `json:"data_type"`
	DataSize    int    `json:"data_size,omitempty"`
}

// ConsumerStats provides runtime statistics for the debug consumer
type ConsumerStats struct {
	EventsProcessed uint64            `json:"events_processed"`
	ErrorsCount     uint64            `json:"errors_count"`
	Uptime          time.Duration     `json:"uptime"`
	EventsByType    map[string]uint64 `json:"events_by_type"`
	EventsBySource  map[string]uint64 `json:"events_by_source"`
}
