// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otel

import (
	"sync"

	"github.com/antimetal/agent/internal/metrics"
	"github.com/antimetal/agent/pkg/ringbuffer"
)

// MetricsBuffer provides a thread-safe ring buffer for metric events.
// It automatically overwrites the oldest events when capacity is reached.
type MetricsBuffer struct {
	rb *ringbuffer.RingBuffer[metrics.MetricEvent]
	mu sync.Mutex

	notifyThreshold int
	notify          chan struct{}
}

// NewMetricsBuffer creates a new thread-safe metrics buffer
func NewMetricsBuffer(capacity, notifyThreshold int) (*MetricsBuffer, error) {
	rb, err := ringbuffer.New[metrics.MetricEvent](capacity)
	if err != nil {
		return nil, err
	}

	return &MetricsBuffer{
		rb:              rb,
		notifyThreshold: notifyThreshold,
		notify:          make(chan struct{}, 1), // Buffered to avoid blocking
	}, nil
}

// Push adds an event to the buffer, overwriting the oldest if full.
// This method never blocks.
func (b *MetricsBuffer) Push(event metrics.MetricEvent) {
	b.mu.Lock()
	b.rb.Push(event)
	currentLen := b.rb.Len()
	b.mu.Unlock()

	if currentLen >= b.notifyThreshold {
		// Non-blocking notification
		select {
		case b.notify <- struct{}{}:
		default:
			// Channel already has a notification pending
		}
	}
}

// Drain removes and returns all of the buffered items.
// Returns nil if the buffer is empty.
func (b *MetricsBuffer) Drain() []metrics.MetricEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.rb.Len() == 0 {
		return nil
	}

	all := b.rb.GetAll()
	b.rb.Clear()
	return all
}

// NotifyChannel returns a channel that receives notifications when new events are added
func (b *MetricsBuffer) NotifyChannel() <-chan struct{} {
	return b.notify
}

// Len returns the current number of events in the buffer
func (b *MetricsBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rb.Len()
}

// Cap returns the capacity of the buffer
func (b *MetricsBuffer) Cap() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rb.Cap()
}
