// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package metrics

// Consumer represents a metrics consumer that processes metric events.
// Lifecycle is managed through the events channel - when it's closed, the consumer stops.
type Consumer interface {
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
