// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package metrics

import "context"

// Consumer represents a metrics consumer that processes metric events.
// Each consumer receives events via direct method calls and decides how to handle them.
type Consumer interface {
	// Name returns the unique name of this consumer
	Name() string

	// HandleEvent processes a single metric event (non-blocking)
	// Consumers should handle buffering/batching internally if needed
	HandleEvent(event MetricEvent) error

	// Start initializes the consumer (e.g., start background workers)
	Start(ctx context.Context) error

	// Health returns the current health status
	Health() ConsumerHealth
}

type ConsumerHealth struct {
	Healthy     bool
	LastError   error
	EventsCount uint64
	ErrorsCount uint64
}
