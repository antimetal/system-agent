// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package ringbuffer

import "fmt"

// RingBuffer is a generic, thread-unsafe circular buffer implementation that
// overwrites oldest elements when capacity is reached.
//
// This implementation is useful for scenarios where you want to keep only
// the most recent N items, such as:
//   - Recent log entries
//   - Latest measurements or samples
//   - Rolling window of events
//
// Note: This implementation is NOT thread-safe. If concurrent access is needed,
// synchronization must be handled externally.
type RingBuffer[T any] struct {
	data []T
	head int // next write position
	size int // current number of elements
}

// New creates a new ring buffer with the given capacity
func New[T any](capacity int) (*RingBuffer[T], error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("capacity must be greater than 0, got %d", capacity)
	}
	return &RingBuffer[T]{
		data: make([]T, capacity),
	}, nil
}

// Push adds an element to the ring buffer, overwriting oldest if full
func (r *RingBuffer[T]) Push(item T) {
	r.data[r.head] = item
	r.head = (r.head + 1) % cap(r.data)
	if r.size < cap(r.data) {
		r.size++
	}
}

// GetAll returns all elements in chronological order (oldest to newest)
func (r *RingBuffer[T]) GetAll() []T {
	if r.size == 0 {
		return []T{}
	}

	result := make([]T, r.size)

	// If buffer is not full, elements are from 0 to head-1
	if r.size < cap(r.data) {
		copy(result, r.data[:r.size])
		return result
	}

	// If buffer is full, oldest element is at head position
	// Copy from head to end
	n := copy(result, r.data[r.head:])
	// Copy from beginning to head
	copy(result[n:], r.data[:r.head])

	return result
}

// Len returns the current number of elements in the buffer
func (r *RingBuffer[T]) Len() int {
	return r.size
}

// Cap returns the capacity of the buffer
func (r *RingBuffer[T]) Cap() int {
	return cap(r.data)
}

// Clear removes all elements from the buffer
func (r *RingBuffer[T]) Clear() {
	r.size = 0
	r.head = 0
	// Clear the underlying data to help GC
	clear(r.data)
}
