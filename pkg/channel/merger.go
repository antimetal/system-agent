// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt
package channel

import (
	"reflect"
	"slices"
)

// Merger merges multiple input channels into a single output channel.
// Message delivery order is guaranteed within a single input channel.
type Merger[T any] struct {
	out   chan T
	addCh chan (<-chan T)
	done  chan struct{}
}

// NewMerger creates a new Merger with initial input channels.
func NewMerger[T any](inputs ...<-chan T) *Merger[T] {
	buf := 0
	for _, ch := range inputs {
		if cap(ch) > buf {
			buf = cap(ch)
		}
	}

	m := &Merger[T]{
		out:   make(chan T, buf),
		addCh: make(chan (<-chan T)),
		done:  make(chan struct{}),
	}

	go m.run(inputs)

	return m
}

// Add adds a new input channel.
//
// When the input channel is closed, the output channel
// will stop receiving messages on that channel.
//
// This method can be called in multiple goroutines.
//
// Calling Add() after Close() is called will result in
// a panic.
func (m *Merger[T]) Add(input <-chan T) {
	m.addCh <- input
}

// Out returns the output channel. Values from all input channels are
// merged into this channel. If all input channels are unbuffered,
// then the output channel will also be unbuffered; if at least 1 input
// channel is buffered, then the output channel will be buffered and
// will be set to the maximum buffer size of all buffered input channels.
//
// The output channel closes when Close() is called.
// A closed channel will be returned if Close is called.
func (m *Merger[T]) Out() <-chan T {
	return m.out
}

// Close the merger. This closes the output channel returned by Out()
// Calling Close again after the first call will result in a panic
func (m *Merger[T]) Close() {
	close(m.addCh)
	close(m.done)
}

func (m *Merger[T]) run(initialInputs []<-chan T) {
	defer close(m.out)

	cases := make([]reflect.SelectCase, 0, len(initialInputs)+2)
	for _, ch := range initialInputs {
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(ch),
		})
	}

	cases = append(cases,
		reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(m.addCh),
		},
		reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(m.done),
		},
	)

	for {
		chosen, value, ok := reflect.Select(cases)

		switch chosen {
		case len(cases) - 1:
			// m.done was selected
			return
		case len(cases) - 2:
			// m.addCh was selected
			if !ok {
				return
			}
			newCh := value.Interface().(<-chan T)
			newCase := reflect.SelectCase{
				Dir:  reflect.SelectRecv,
				Chan: reflect.ValueOf(newCh),
			}
			cases = slices.Insert(cases, len(cases)-2, newCase)
		default:
			// Input channel was selected
			if !ok {
				cases = slices.Delete(cases, chosen, chosen+1)
				continue
			}
			m.out <- value.Interface().(T)
		}
	}
}
