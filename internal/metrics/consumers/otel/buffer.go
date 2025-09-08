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

	// Notification channel for new events
	notify chan struct{}
}

// NewMetricsBuffer creates a new thread-safe metrics buffer
func NewMetricsBuffer(capacity int) (*MetricsBuffer, error) {
	rb, err := ringbuffer.New[metrics.MetricEvent](capacity)
	if err != nil {
		return nil, err
	}

	return &MetricsBuffer{
		rb:     rb,
		notify: make(chan struct{}, 1), // Buffered to avoid blocking
	}, nil
}

// Push adds an event to the buffer, overwriting the oldest if full.
// This method never blocks.
func (b *MetricsBuffer) Push(event metrics.MetricEvent) {
	b.mu.Lock()
	b.rb.Push(event)
	b.mu.Unlock()

	// Non-blocking notification
	select {
	case b.notify <- struct{}{}:
	default:
		// Channel already has a notification pending
	}
}

// Drain removes and returns up to maxItems from the buffer.
// Returns nil if the buffer is empty.
func (b *MetricsBuffer) Drain(maxItems int) []metrics.MetricEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.rb.Len() == 0 {
		return nil
	}

	// Get all items from the ring buffer
	all := b.rb.GetAll()

	// Determine how many to return
	count := len(all)
	if maxItems > 0 && maxItems < count {
		count = maxItems
	}

	// Clear the buffer and return the drained items
	b.rb.Clear()

	// If we're only returning some items, push the rest back
	if count < len(all) {
		for i := count; i < len(all); i++ {
			b.rb.Push(all[i])
		}
	}

	return all[:count]
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

