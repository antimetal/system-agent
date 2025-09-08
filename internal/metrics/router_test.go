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

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMetricData is a simple test data type
type mockMetricData struct {
	value any
}

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
	router := NewMetricsRouter(logr.Discard(), "test-node", "test-cluster", "test-source")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the router
	go func() {
		err := router.Start(ctx)
		assert.NoError(t, err)
	}()

	// Give router time to start
	time.Sleep(10 * time.Millisecond)

	// Create and start a consumer
	consumer := newMockConsumer("test-consumer")
	err := consumer.Start(ctx)
	require.NoError(t, err)

	// Register the consumer
	err = router.RegisterConsumer(consumer)
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
				err := router.Accept(id*eventsPerGoroutine + j)
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

// Test that publishing after close returns an error
func TestMetricsRouter_PublishAfterClose(t *testing.T) {
	router := NewMetricsRouter(logr.Discard(), "test-node", "test-cluster", "test-source")

	ctx, cancel := context.WithCancel(context.Background())

	// Start the router
	go func() {
		err := router.Start(ctx)
		assert.NoError(t, err)
	}()

	// Give router time to start
	time.Sleep(10 * time.Millisecond)

	// Publish an event successfully
	data := &mockMetricData{value: "test data"}
	err := router.Accept(data)
	require.NoError(t, err)

	// Stop the router
	cancel()
	time.Sleep(50 * time.Millisecond)

	// Try to publish after close
	data2 := &mockMetricData{value: "test data after close"}
	err = router.Accept(data2)
	assert.Equal(t, ErrRouterClosed, err)
}

func TestMetricsRouter_ConsumerRegistration(t *testing.T) {
	router := NewMetricsRouter(logr.Discard(), "test-node", "test-cluster", "test-source")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the router
	go func() {
		err := router.Start(ctx)
		assert.NoError(t, err)
	}()

	// Give router time to start
	time.Sleep(10 * time.Millisecond)

	// Create and start first consumer
	consumer1 := newMockConsumer("consumer1")
	err := consumer1.Start(ctx)
	require.NoError(t, err)
	err = router.RegisterConsumer(consumer1)
	require.NoError(t, err)

	// Try to register duplicate
	consumer2 := newMockConsumer("consumer1")
	err = consumer2.Start(ctx)
	require.NoError(t, err)
	err = router.RegisterConsumer(consumer2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")

	// Register different consumer
	consumer3 := newMockConsumer("consumer2")
	err = consumer3.Start(ctx)
	require.NoError(t, err)
	err = router.RegisterConsumer(consumer3)
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
	router := NewMetricsRouter(logr.Discard(), "test-node", "test-cluster", "test-source")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the router
	go func() {
		err := router.Start(ctx)
		assert.NoError(t, err)
	}()

	// Give router time to start
	time.Sleep(10 * time.Millisecond)

	// Create, start and register consumers
	consumer1 := newMockConsumer("consumer1")
	consumer2 := newMockConsumer("consumer2")

	err := consumer1.Start(ctx)
	require.NoError(t, err)
	err = router.RegisterConsumer(consumer1)
	require.NoError(t, err)

	err = consumer2.Start(ctx)
	require.NoError(t, err)
	err = router.RegisterConsumer(consumer2)
	require.NoError(t, err)

	// Publish events with different real performance data types
	// This tests that the router correctly identifies metric types from data
	events := []struct {
		name               string
		data               any
		expectedMetricType MetricType
	}{
		{
			name: "system_stats",
			data: &performance.SystemStats{
				Interrupts:      1000,
				ContextSwitches: 2000,
			},
			expectedMetricType: MetricTypeSystem,
		},
		{
			name: "cpu_stats",
			data: &performance.CPUStats{
				User:   50.0,
				System: 25.0,
				Idle:   25.0,
			},
			expectedMetricType: MetricTypeCPU,
		},
		{
			name: "memory_stats",
			data: &performance.MemoryStats{
				MemTotal:     8192,
				MemFree:      4096,
				MemAvailable: 4096,
			},
			expectedMetricType: MetricTypeMemory,
		},
		{
			name: "network_stats_slice",
			data: []performance.NetworkStats{
				{
					Interface: "eth0",
					RxBytes:   1024,
					TxBytes:   2048,
				},
			},
			expectedMetricType: MetricTypeNetwork,
		},
		{
			name:               "unknown_type",
			data:               &mockMetricData{value: "test"},
			expectedMetricType: MetricType("unknown"),
		},
	}

	for _, event := range events {
		err := router.Accept(event.data)
		require.NoError(t, err, "Failed to accept event: %s", event.name)
	}

	// Wait for delivery
	time.Sleep(50 * time.Millisecond)

	// Check that both consumers received all events
	assert.Eventually(t, func() bool {
		return len(consumer1.getEvents()) == len(events) && len(consumer2.getEvents()) == len(events)
	}, 100*time.Millisecond, 10*time.Millisecond)

	// Verify that the correct metric types were identified
	consumer1Events := consumer1.getEvents()
	for i, event := range events {
		assert.Equal(t, event.expectedMetricType, consumer1Events[i].MetricType,
			"Incorrect metric type for event %s", event.name)
		assert.Equal(t, event.data, consumer1Events[i].Data,
			"Incorrect data for event %s", event.name)
	}

	// Also verify consumer2 got the same events
	consumer2Events := consumer2.getEvents()
	for i, event := range events {
		assert.Equal(t, event.expectedMetricType, consumer2Events[i].MetricType,
			"Incorrect metric type for event %s in consumer2", event.name)
	}
}

// TestMetricsRouter_LifecycleManagement tests that consumers manage their own lifecycle
func TestMetricsRouter_LifecycleManagement(t *testing.T) {
	router := NewMetricsRouter(logr.Discard(), "test-node", "test-cluster", "test-source")

	// Create consumers
	consumer1 := newMockConsumer("consumer1")
	consumer2 := newMockConsumer("consumer2")

	// Start consumers with their own contexts
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	err := consumer1.Start(ctx1)
	require.NoError(t, err)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	err = consumer2.Start(ctx2)
	require.NoError(t, err)

	// Register started consumers
	err = router.RegisterConsumer(consumer1)
	require.NoError(t, err)
	err = router.RegisterConsumer(consumer2)
	require.NoError(t, err)

	// Start the router with its own context
	routerCtx, routerCancel := context.WithCancel(context.Background())
	defer routerCancel()

	err = router.Start(routerCtx)
	require.NoError(t, err)

	// Verify consumers can receive events
	data := &mockMetricData{value: "test data"}
	err = router.Accept(data)
	require.NoError(t, err)

	// Give time for event processing
	time.Sleep(50 * time.Millisecond)

	// Check that both consumers received the event
	assert.Equal(t, 1, len(consumer1.getEvents()))
	assert.Equal(t, 1, len(consumer2.getEvents()))

	// Cancel one consumer's context
	cancel1()
	time.Sleep(50 * time.Millisecond)

	// Unregister the stopped consumer (this is what should happen in practice)
	err = router.UnregisterConsumer("consumer1")
	require.NoError(t, err)

	// Send another event
	data2 := &mockMetricData{value: "test data 2"}
	err = router.Accept(data2)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Consumer1 should still have 1 event (unregistered), consumer2 should have 2
	assert.Equal(t, 1, len(consumer1.getEvents()))
	assert.Equal(t, 2, len(consumer2.getEvents()))
}
