// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"context"
	"sync"
)

// MockReceiver is a test implementation of the Receiver interface
// It stores all received data for verification in tests
type MockReceiver struct {
	mu          sync.Mutex
	name        string
	AcceptCalls []AcceptCall
	AcceptFunc  func(data any) error
}

// AcceptCall records a single call to Accept
type AcceptCall struct {
	Data any
}

// NewMockReceiver creates a new mock receiver
func NewMockReceiver(name string) *MockReceiver {
	return &MockReceiver{
		name:        name,
		AcceptCalls: make([]AcceptCall, 0),
	}
}

// Accept implements the Receiver interface
func (m *MockReceiver) Accept(data any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.AcceptCalls = append(m.AcceptCalls, AcceptCall{
		Data: data,
	})

	if m.AcceptFunc != nil {
		return m.AcceptFunc(data)
	}

	return nil
}

// Name implements the Receiver interface
func (m *MockReceiver) Name() string {
	return m.name
}

// GetAcceptCalls returns all recorded Accept calls
func (m *MockReceiver) GetAcceptCalls() []AcceptCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Return a copy to avoid race conditions
	calls := make([]AcceptCall, len(m.AcceptCalls))
	copy(calls, m.AcceptCalls)
	return calls
}

// GetCallCount returns the number of times Accept was called
func (m *MockReceiver) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.AcceptCalls)
}

// GetLastCall returns the most recent Accept call, or nil if no calls
func (m *MockReceiver) GetLastCall() *AcceptCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.AcceptCalls) == 0 {
		return nil
	}

	call := m.AcceptCalls[len(m.AcceptCalls)-1]
	return &call
}

// Clear resets all recorded calls
func (m *MockReceiver) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AcceptCalls = make([]AcceptCall, 0)
}

// WaitForCall waits until at least n calls have been made or context is cancelled
func (m *MockReceiver) WaitForCall(ctx context.Context, n int) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if m.GetCallCount() >= n {
				return nil
			}
			// Small sleep to avoid busy waiting
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-make(chan struct{}):
				// This will block briefly
			}
		}
	}
}
