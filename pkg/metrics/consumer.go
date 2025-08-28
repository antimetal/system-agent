// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package metrics

// Consumer defines the interface for processing metrics events
type Consumer interface {
	// Name returns a unique identifier for this consumer
	Name() string

	// Start begins consuming metrics events from the provided channel
	Start(events <-chan MetricEvent) error

	// Stop gracefully shuts down the consumer
	Stop() error

	// Health returns the current health status of the consumer
	Health() ConsumerHealth
}

// ConsumerHealth represents the health status of a consumer
type ConsumerHealth struct {
	Healthy     bool
	LastError   error
	EventsCount uint64 // Total events processed
	ErrorsCount uint64 // Total errors encountered
}
