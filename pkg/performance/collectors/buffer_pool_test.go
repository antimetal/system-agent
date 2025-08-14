// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt


package collectors

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinimalPool_MemoryLimit(t *testing.T) {
	// Force GC to get clean baseline
	runtime.GC()
	runtime.GC()

	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	pool := NewMinimalPool()

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	heapGrowth := m2.HeapAlloc - m1.HeapAlloc

	// 3 buffers * 1KB + channel overhead + struct
	// Should be less than 4KB total
	assert.Less(t, heapGrowth, uint64(4096),
		"Pool memory usage exceeds limit: %d bytes", heapGrowth)

	// Verify we have exactly 3 buffers
	assert.Equal(t, 3, pool.Available())
}

func TestMinimalPool_GetPut(t *testing.T) {
	pool := NewMinimalPool()

	// Get a buffer
	buf := pool.Get()
	require.NotNil(t, buf)
	assert.Equal(t, 0, len(buf))
	assert.Equal(t, BufferSize, cap(buf))

	// Pool should have 2 buffers left
	assert.Equal(t, 2, pool.Available())

	// Use the buffer
	buf = append(buf, []byte("test data")...)

	// Return it
	pool.Put(buf)

	// Pool should have 3 buffers again
	assert.Equal(t, 3, pool.Available())

	// Get it again - should be reset
	buf2 := pool.Get()
	assert.Equal(t, 0, len(buf2))
	assert.Equal(t, BufferSize, cap(buf2))
}

func TestMinimalPool_Exhaustion(t *testing.T) {
	pool := NewMinimalPool()

	// Get all 3 buffers
	b1 := pool.Get()
	b2 := pool.Get()
	b3 := pool.Get()

	require.NotNil(t, b1)
	require.NotNil(t, b2)
	require.NotNil(t, b3)

	// Pool should be empty
	assert.Equal(t, 0, pool.Available())

	// Fourth request should return nil
	b4 := pool.Get()
	assert.Nil(t, b4)

	// Stats should show a miss
	stats := pool.GetStats()
	assert.Equal(t, uint64(3), stats.Gets)
	assert.Equal(t, uint64(1), stats.Misses)
	assert.Equal(t, int32(3), stats.InUse)

	// Return one buffer
	pool.Put(b1)

	// Should be able to get one again
	b5 := pool.Get()
	assert.NotNil(t, b5)

	// Clean up
	pool.Put(b2)
	pool.Put(b3)
	pool.Put(b5)

	// All buffers should be back
	assert.Equal(t, 3, pool.Available())
}

func TestMinimalPool_ConcurrentAccess(t *testing.T) {
	pool := NewMinimalPool()
	const numGoroutines = 10
	const numOperations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				buf := pool.Get()
				if buf != nil {
					// Simulate some work
					buf = append(buf, byte(j))
					time.Sleep(time.Microsecond)
					pool.Put(buf)
				} else {
					// Pool exhausted, wait a bit
					time.Sleep(time.Microsecond * 10)
				}
			}
		}()
	}

	wg.Wait()

	// Verify pool integrity
	assert.Equal(t, 3, pool.Available())

	stats := pool.GetStats()
	assert.Equal(t, stats.Gets, stats.Puts)
	assert.Equal(t, int32(0), stats.InUse)

	// Should have some misses due to contention
	assert.Greater(t, stats.Misses, uint64(0))
}

func TestMinimalPool_InvalidBufferPanic(t *testing.T) {
	pool := NewMinimalPool()

	// Returning wrong size buffer should panic
	assert.Panics(t, func() {
		wrongBuf := make([]byte, 0, 2048) // Wrong size
		pool.Put(wrongBuf)
	})
}

func TestMinimalPool_NilBuffer(t *testing.T) {
	pool := NewMinimalPool()

	// Putting nil should not panic
	assert.NotPanics(t, func() {
		pool.Put(nil)
	})

	// Pool should still have 3 buffers
	assert.Equal(t, 3, pool.Available())
}

func TestMinimalPool_MissRate(t *testing.T) {
	pool := NewMinimalPool()

	// Initially no misses
	assert.Equal(t, 0.0, pool.MissRate())

	// Get all buffers
	b1 := pool.Get()
	b2 := pool.Get()
	b3 := pool.Get()

	// Try to get more (will miss)
	pool.Get() // miss 1
	pool.Get() // miss 2

	missRate := pool.MissRate()
	// 2 misses out of 5 total attempts = 40%
	assert.Equal(t, 40.0, missRate)

	// Clean up
	pool.Put(b1)
	pool.Put(b2)
	pool.Put(b3)
}

func TestMinimalPool_ZeroAllocations(t *testing.T) {
	pool := NewMinimalPool()

	// Pre-get and return all buffers to warm up
	bufs := make([][]byte, 3)
	for i := 0; i < 3; i++ {
		bufs[i] = pool.Get()
	}
	for i := 0; i < 3; i++ {
		pool.Put(bufs[i])
	}

	// Now measure allocations
	allocs := testing.AllocsPerRun(100, func() {
		buf := pool.Get()
		if buf != nil {
			pool.Put(buf)
		}
	})

	assert.Equal(t, float64(0), allocs, "Get/Put should have zero allocations")
}

func BenchmarkMinimalPool_GetPut(b *testing.B) {
	pool := NewMinimalPool()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get()
			if buf != nil {
				pool.Put(buf)
			}
		}
	})

	b.StopTimer()
	stats := pool.GetStats()
	b.ReportMetric(float64(stats.Misses)/float64(stats.Gets+stats.Misses)*100, "miss_rate_%")
}