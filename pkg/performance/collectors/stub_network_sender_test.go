// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStubNetworkSender_Basic(t *testing.T) {
	sender := NewStubNetworkSender("test-node", true)
	ctx := context.Background()

	// Create some test events
	events := []ProfileEvent{
		{
			Timestamp:     uint64(time.Now().UnixNano()),
			PID:           1234,
			TID:           5678,
			UserStackID:   42,
			KernelStackID: 84,
			CPU:           0,
			Flags:         ProfileFlagUserStackTruncated,
		},
		{
			Timestamp:     uint64(time.Now().UnixNano()),
			PID:           2345,
			TID:           6789,
			UserStackID:   99,
			KernelStackID: 88,
			CPU:           1,
			Flags:         0,
		},
	}

	// Send batch
	err := sender.SendBatch(ctx, events)
	require.NoError(t, err)

	// Check stats
	batches, eventCount, dropped, bytes := sender.GetStats()
	assert.Equal(t, uint64(1), batches)
	assert.Equal(t, uint64(2), eventCount)
	assert.Equal(t, uint64(0), dropped)
	assert.Greater(t, bytes, uint64(0))
}

func TestStubNetworkSender_PoolExhaustion(t *testing.T) {
	sender := NewStubNetworkSender("test-node", false)
	ctx := context.Background()

	events := make([]ProfileEvent, 10)
	for i := range events {
		events[i] = ProfileEvent{
			Timestamp: uint64(i),
			PID:       int32(i),
			CPU:       uint32(i % 4),
		}
	}

	// Exhaust the pool by not returning buffers
	var wg sync.WaitGroup
	exhausted := 0

	// Get all 3 buffers and hold them
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := sender.pool.Get()
			if buf != nil {
				time.Sleep(100 * time.Millisecond)
				sender.pool.Put(buf)
			}
		}()
	}

	// Wait a bit for goroutines to get buffers
	time.Sleep(10 * time.Millisecond)

	// This should fail due to pool exhaustion
	err := sender.SendBatch(ctx, events)
	if err == ErrPoolExhausted {
		exhausted++
	}

	wg.Wait()

	// Now it should work
	err = sender.SendBatch(ctx, events)
	assert.NoError(t, err)

	// Verify we saw exhaustion
	assert.Equal(t, 1, exhausted)
}

func TestStubNetworkSender_LargeVolume(t *testing.T) {
	sender := NewStubNetworkSender("test-node", false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start periodic stats
	sender.StartPeriodicStats(ctx)

	// Generate events continuously
	go func() {
		batch := make([]ProfileEvent, 20)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Fill batch with events
				now := uint64(time.Now().UnixNano())
				for i := range batch {
					batch[i] = ProfileEvent{
						Timestamp:     now,
						PID:           int32(i + 1000),
						TID:           int32(i + 2000),
						UserStackID:   int32(i),
						KernelStackID: int32(i + 10),
						CPU:           uint32(i % 8),
						Flags:         0,
					}
				}

				// Send batch
				sender.SendBatch(ctx, batch)
				
				// Small delay to simulate realistic rate
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Wait for context to expire
	<-ctx.Done()
	
	// Get final stats
	batches, events, dropped, bytes := sender.GetStats()
	
	fmt.Printf("\nTest completed: %d batches, %d events, %d bytes\n", 
		batches, events, bytes)
	
	// Should have processed a reasonable amount
	assert.Greater(t, batches, uint64(100))
	assert.Greater(t, events, uint64(1000))
	assert.Equal(t, uint64(0), dropped) // Should not drop with 20-event batches
	
	sender.Close()
}

func TestStubNetworkSender_Decoding(t *testing.T) {
	sender := NewStubNetworkSender("test-node", false)

	// Create events with specific values
	events := []ProfileEvent{
		{
			Timestamp:     123456789,
			PID:           1111,
			TID:           2222,
			UserStackID:   42,
			KernelStackID: 84,
			CPU:           3,
			Flags:         ProfileFlagKernelStackTruncated,
		},
	}

	// Get buffer and encode
	buf := sender.pool.Get()
	require.NotNil(t, buf)
	defer sender.pool.Put(buf)

	batch, _, err := sender.encoder.EncodeToProtobuf(events, buf)
	require.NoError(t, err)

	// Decode and verify
	decoded, err := DecodePackedEvents(batch)
	require.NoError(t, err)
	require.Len(t, decoded, 1)

	// Verify the decoded event matches
	assert.Equal(t, events[0], decoded[0])
}

func ExampleStubNetworkSender() {
	// Create sender with verbose output
	sender := NewStubNetworkSender("example-node", true)

	// Create sample events
	events := []ProfileEvent{
		{
			Timestamp:     uint64(time.Now().UnixNano()),
			PID:           1234,
			TID:           5678,
			UserStackID:   100,
			KernelStackID: 200,
			CPU:           0,
			Flags:         0,
		},
		{
			Timestamp:     uint64(time.Now().UnixNano()),
			PID:           2345,
			TID:           6789,
			UserStackID:   101,
			KernelStackID: 201,
			CPU:           1,
			Flags:         ProfileFlagUserStackTruncated,
		},
	}

	// Send batch - will print details
	err := sender.SendBatch(context.Background(), events)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	// Print final stats
	sender.Close()

	// Output will show batch details and decoded events
}