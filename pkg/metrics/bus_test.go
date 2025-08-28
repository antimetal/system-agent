// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package metrics

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockConsumer implements the Consumer interface for testing
type mockConsumer struct {
	name      string
	events    []MetricEvent
	mu        sync.Mutex
	started   bool
	stopped   bool
	eventChan chan MetricEvent
}

func newMockConsumer(name string) *mockConsumer {
	return &mockConsumer{
		name:   name,
		events: make([]MetricEvent, 0),
	}
}

func (m *mockConsumer) Name() string {
	return m.name
}

func (m *mockConsumer) Start(events <-chan MetricEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.started {
		return nil
	}
	
	m.started = true
	m.eventChan = make(chan MetricEvent, 100)
	
	// Start goroutine to consume events
	go func() {
		for event := range events {
			m.mu.Lock()
			m.events = append(m.events, event)
			m.mu.Unlock()
		}
	}()
	
	return nil
}

func (m *mockConsumer) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	return nil
}

func (m *mockConsumer) Health() ConsumerHealth {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	return ConsumerHealth{
		Healthy:     m.started && !m.stopped,
		EventsCount: uint64(len(m.events)),
	}
}

func (m *mockConsumer) getEvents() []MetricEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]MetricEvent{}, m.events...)
}

func TestMetricsBus_ConcurrentPublish(t *testing.T) {
	// Test that concurrent publishes don't cause race conditions
	config := DefaultBusConfig()
	config.BufferSize = 1000
	
	bus := NewMetricsBus(config, logr.Discard())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Start the bus
	go func() {
		err := bus.Start(ctx)
		assert.NoError(t, err)
	}()
	
	// Give bus time to start
	time.Sleep(10 * time.Millisecond)
	
	// Register a consumer
	consumer := newMockConsumer("test-consumer")
	err := bus.RegisterConsumer(consumer)
	require.NoError(t, err)
	
	// Publish events concurrently
	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 100
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				event := MetricEvent{
					Timestamp:  time.Now(),
					Source:     "test",
					MetricType: MetricType("test"),
					Data:       id*eventsPerGoroutine + j,
				}
				err := bus.Publish(event)
				assert.NoError(t, err)
			}
		}(i)
	}
	
	wg.Wait()
	
	// Give time for events to be processed
	time.Sleep(100 * time.Millisecond)
	
	// Check stats
	stats := bus.GetStats()
	assert.Equal(t, uint64(numGoroutines*eventsPerGoroutine), stats.TotalEvents)
	assert.Equal(t, uint64(0), stats.DroppedEvents)
}

func TestMetricsBus_PublishAfterClose(t *testing.T) {
	// Test that publishing after close returns an error
	config := DefaultBusConfig()
	bus := NewMetricsBus(config, logr.Discard())
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Start the bus
	go func() {
		err := bus.Start(ctx)
		assert.NoError(t, err)
	}()
	
	// Give bus time to start
	time.Sleep(10 * time.Millisecond)
	
	// Publish an event successfully
	event := MetricEvent{
		Timestamp:  time.Now(),
		Source:     "test",
		MetricType: MetricType("test"),
		Data:       "test data",
	}
	err := bus.Publish(event)
	require.NoError(t, err)
	
	// Stop the bus
	cancel()
	time.Sleep(50 * time.Millisecond)
	
	// Try to publish after close
	err = bus.Publish(event)
	assert.Equal(t, ErrBusClosed, err)
}

func TestMetricsBus_DropPolicies(t *testing.T) {
	tests := []struct {
		name       string
		dropPolicy DropPolicy
		expectDrop bool
	}{
		{"DropOldest", DropPolicyOldest, true},
		{"DropNewest", DropPolicyNewest, true},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultBusConfig()
			config.BufferSize = 2 // Very small buffer to trigger drops
			config.DropPolicy = tt.dropPolicy
			config.FlushInterval = 1 * time.Hour // Don't flush automatically
			
			bus := NewMetricsBus(config, logr.Discard())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			
			// Start the bus
			go func() {
				err := bus.Start(ctx)
				assert.NoError(t, err)
			}()
			
			// Give bus time to start
			time.Sleep(10 * time.Millisecond)
			
			// Fill the buffer
			for i := 0; i < 10; i++ {
				event := MetricEvent{
					Timestamp:  time.Now(),
					Source:     "test",
					MetricType: MetricType("test"),
					Data:       i,
				}
				_ = bus.Publish(event)
			}
			
			// Check that events were dropped
			stats := bus.GetStats()
			if tt.expectDrop {
				assert.Greater(t, stats.DroppedEvents, uint64(0))
			}
			assert.Greater(t, stats.TotalEvents, uint64(0))
		})
	}
}

func TestMetricsBus_ConsumerRegistration(t *testing.T) {
	config := DefaultBusConfig()
	bus := NewMetricsBus(config, logr.Discard())
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Start the bus
	go func() {
		err := bus.Start(ctx)
		assert.NoError(t, err)
	}()
	
	// Give bus time to start
	time.Sleep(10 * time.Millisecond)
	
	// Register first consumer
	consumer1 := newMockConsumer("consumer1")
	err := bus.RegisterConsumer(consumer1)
	require.NoError(t, err)
	
	// Try to register duplicate
	consumer2 := newMockConsumer("consumer1")
	err = bus.RegisterConsumer(consumer2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
	
	// Register different consumer
	consumer3 := newMockConsumer("consumer2")
	err = bus.RegisterConsumer(consumer3)
	require.NoError(t, err)
	
	// Check stats
	stats := bus.GetStats()
	assert.Equal(t, 2, stats.ConsumerCount)
	
	// Unregister consumer
	err = bus.UnregisterConsumer("consumer1")
	require.NoError(t, err)
	
	stats = bus.GetStats()
	assert.Equal(t, 1, stats.ConsumerCount)
	
	// Try to unregister non-existent consumer
	err = bus.UnregisterConsumer("non-existent")
	assert.Error(t, err)
}

func TestMetricsBus_EventDelivery(t *testing.T) {
	config := DefaultBusConfig()
	config.FlushInterval = 10 * time.Millisecond
	config.MaxBatchSize = 2
	
	bus := NewMetricsBus(config, logr.Discard())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Start the bus
	go func() {
		err := bus.Start(ctx)
		assert.NoError(t, err)
	}()
	
	// Give bus time to start
	time.Sleep(10 * time.Millisecond)
	
	// Register consumers
	consumer1 := newMockConsumer("consumer1")
	consumer2 := newMockConsumer("consumer2")
	
	err := bus.RegisterConsumer(consumer1)
	require.NoError(t, err)
	err = bus.RegisterConsumer(consumer2)
	require.NoError(t, err)
	
	// Publish events
	events := []MetricEvent{
		{Timestamp: time.Now(), Source: "test", MetricType: "test", Data: "event1"},
		{Timestamp: time.Now(), Source: "test", MetricType: "test", Data: "event2"},
		{Timestamp: time.Now(), Source: "test", MetricType: "test", Data: "event3"},
	}
	
	for _, event := range events {
		err := bus.Publish(event)
		require.NoError(t, err)
	}
	
	// Wait for delivery
	time.Sleep(50 * time.Millisecond)
	
	// Check that both consumers received all events
	assert.Eventually(t, func() bool {
		return len(consumer1.getEvents()) == 3 && len(consumer2.getEvents()) == 3
	}, 100*time.Millisecond, 10*time.Millisecond)
}