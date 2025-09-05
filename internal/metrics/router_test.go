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
	name    string
	events  []MetricEvent
	mu      sync.Mutex
	started bool
	stopped bool
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

func (m *mockConsumer) HandleEvent(event MetricEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockConsumer) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	m.started = true
	return nil
}

// Stop is no longer needed - lifecycle managed by context

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

func TestMetricsRouter_ConcurrentPublish(t *testing.T) {
	// Test that concurrent publishes don't cause race conditions
	router := NewMetricsRouter(logr.Discard())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the router
	go func() {
		err := router.Start(ctx)
		assert.NoError(t, err)
	}()

	// Give router time to start
	time.Sleep(10 * time.Millisecond)

	// Register a consumer
	consumer := newMockConsumer("test-consumer")
	err := router.RegisterConsumer(ctx, consumer)
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
				err := router.Publish(event)
				assert.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()

	// Give time for events to be processed
	time.Sleep(100 * time.Millisecond)

	// Check that consumer received all events
	consumerEvents := consumer.getEvents()
	assert.Equal(t, numGoroutines*eventsPerGoroutine, len(consumerEvents))
}

func TestMetricsRouter_PublishAfterClose(t *testing.T) {
	// Test that publishing after close returns an error
	router := NewMetricsRouter(logr.Discard())

	ctx, cancel := context.WithCancel(context.Background())

	// Start the router
	go func() {
		err := router.Start(ctx)
		assert.NoError(t, err)
	}()

	// Give router time to start
	time.Sleep(10 * time.Millisecond)

	// Publish an event successfully
	event := MetricEvent{
		Timestamp:  time.Now(),
		Source:     "test",
		MetricType: MetricType("test"),
		Data:       "test data",
	}
	err := router.Publish(event)
	require.NoError(t, err)

	// Stop the router
	cancel()
	time.Sleep(50 * time.Millisecond)

	// Try to publish after close
	err = router.Publish(event)
	assert.Equal(t, ErrRouterClosed, err)
}

func TestMetricsRouter_DirectDelivery(t *testing.T) {
	// Test that events are directly delivered to consumers without buffering at router level
	router := NewMetricsRouter(logr.Discard())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the router
	go func() {
		err := router.Start(ctx)
		assert.NoError(t, err)
	}()

	// Give router time to start
	time.Sleep(10 * time.Millisecond)

	// Register multiple consumers
	consumer1 := newMockConsumer("consumer1")
	consumer2 := newMockConsumer("consumer2")

	err := router.RegisterConsumer(ctx, consumer1)
	require.NoError(t, err)
	err = router.RegisterConsumer(ctx, consumer2)
	require.NoError(t, err)

	// Publish events
	numEvents := 100
	for i := 0; i < numEvents; i++ {
		event := MetricEvent{
			Timestamp:  time.Now(),
			Source:     "test",
			MetricType: MetricType("test"),
			Data:       i,
		}
		err := router.Publish(event)
		require.NoError(t, err)
	}

	// Check that both consumers received all events immediately (no buffering)
	assert.Equal(t, numEvents, len(consumer1.getEvents()))
	assert.Equal(t, numEvents, len(consumer2.getEvents()))
}

func TestMetricsRouter_ConsumerRegistration(t *testing.T) {
	router := NewMetricsRouter(logr.Discard())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the router
	go func() {
		err := router.Start(ctx)
		assert.NoError(t, err)
	}()

	// Give router time to start
	time.Sleep(10 * time.Millisecond)

	// Register first consumer
	consumer1 := newMockConsumer("consumer1")
	err := router.RegisterConsumer(ctx, consumer1)
	require.NoError(t, err)

	// Try to register duplicate
	consumer2 := newMockConsumer("consumer1")
	err = router.RegisterConsumer(ctx, consumer2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")

	// Register different consumer
	consumer3 := newMockConsumer("consumer2")
	err = router.RegisterConsumer(ctx, consumer3)
	require.NoError(t, err)

	// Check stats
	stats := router.GetStats()
	assert.Equal(t, 2, stats.ConsumerCount)

	// Unregister consumer
	err = router.UnregisterConsumer("consumer1")
	require.NoError(t, err)

	stats = router.GetStats()
	assert.Equal(t, 1, stats.ConsumerCount)

	// Try to unregister non-existent consumer
	err = router.UnregisterConsumer("non-existent")
	assert.Error(t, err)
}

func TestMetricsRouter_EventDelivery(t *testing.T) {
	router := NewMetricsRouter(logr.Discard())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the router
	go func() {
		err := router.Start(ctx)
		assert.NoError(t, err)
	}()

	// Give router time to start
	time.Sleep(10 * time.Millisecond)

	// Register consumers
	consumer1 := newMockConsumer("consumer1")
	consumer2 := newMockConsumer("consumer2")

	err := router.RegisterConsumer(ctx, consumer1)
	require.NoError(t, err)
	err = router.RegisterConsumer(ctx, consumer2)
	require.NoError(t, err)

	// Publish events
	events := []MetricEvent{
		{Timestamp: time.Now(), Source: "test", MetricType: "test", Data: "event1"},
		{Timestamp: time.Now(), Source: "test", MetricType: "test", Data: "event2"},
		{Timestamp: time.Now(), Source: "test", MetricType: "test", Data: "event3"},
	}

	for _, event := range events {
		err := router.Publish(event)
		require.NoError(t, err)
	}

	// Wait for delivery
	time.Sleep(50 * time.Millisecond)

	// Check that both consumers received all events
	assert.Eventually(t, func() bool {
		return len(consumer1.getEvents()) == 3 && len(consumer2.getEvents()) == 3
	}, 100*time.Millisecond, 10*time.Millisecond)
}
