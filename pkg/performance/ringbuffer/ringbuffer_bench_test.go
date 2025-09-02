// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !integration

package ringbuffer_test

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/antimetal/agent/pkg/performance/ringbuffer"
)

// BenchmarkRingBuffer_Push benchmarks the Push operation
func BenchmarkRingBuffer_Push(b *testing.B) {
	sizes := []int{10, 100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			rb, _ := ringbuffer.New[int](size)
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				rb.Push(i)
			}
		})
	}
}

// BenchmarkRingBuffer_Push_Struct benchmarks pushing structs
func BenchmarkRingBuffer_Push_Struct(b *testing.B) {
	type event struct {
		Timestamp uint64
		PID       int32
		TID       int32
		CPU       uint32
	}

	rb, _ := ringbuffer.New[event](1000)
	e := event{
		Timestamp: 1234567890,
		PID:       1234,
		TID:       5678,
		CPU:       0,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rb.Push(e)
	}
}

// BenchmarkRingBuffer_GetAll benchmarks the GetAll operation
func BenchmarkRingBuffer_GetAll(b *testing.B) {
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			rb, _ := ringbuffer.New[int](size)

			// Fill the buffer
			for i := 0; i < size; i++ {
				rb.Push(i)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = rb.GetAll()
			}
		})
	}
}

// BenchmarkRingBuffer_PushAndGetAll benchmarks alternating push and getall
func BenchmarkRingBuffer_PushAndGetAll(b *testing.B) {
	rb, _ := ringbuffer.New[int](100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rb.Push(i)
		if i%10 == 0 {
			_ = rb.GetAll()
		}
	}
}

// BenchmarkRingBuffer_Clear benchmarks the Clear operation
func BenchmarkRingBuffer_Clear(b *testing.B) {
	rb, _ := ringbuffer.New[int](1000)

	// Fill the buffer
	for i := 0; i < 1000; i++ {
		rb.Push(i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rb.Clear()
		// Re-fill for next iteration
		for j := 0; j < 1000; j++ {
			rb.Push(j)
		}
	}
}

// BenchmarkRingBuffer_Memory measures memory usage for different sizes
func BenchmarkRingBuffer_Memory(b *testing.B) {
	sizes := []int{100, 1000, 10000, 100000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			var m1, m2 runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&m1)

			rb, _ := ringbuffer.New[int](size)

			// Fill the buffer
			for i := 0; i < size; i++ {
				rb.Push(i)
			}

			runtime.GC()
			runtime.ReadMemStats(&m2)

			allocated := m2.HeapAlloc - m1.HeapAlloc
			b.ReportMetric(float64(allocated), "bytes")
			b.ReportMetric(float64(allocated)/float64(size), "bytes/element")
		})
	}
}

// BenchmarkRingBuffer_ConcurrentAccess simulates concurrent access patterns
// Note: RingBuffer is NOT thread-safe, this benchmark uses external synchronization
func BenchmarkRingBuffer_ConcurrentAccess(b *testing.B) {
	rb, _ := ringbuffer.New[int](10000)
	var mu sync.Mutex

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			mu.Lock()
			rb.Push(i)
			if i%100 == 0 {
				_ = rb.GetAll()
			}
			mu.Unlock()
			i++
		}
	})
}

// BenchmarkRingBuffer_ProfileEvent benchmarks with actual ProfileEvent struct
func BenchmarkRingBuffer_ProfileEvent(b *testing.B) {
	// Simulate the actual ProfileEvent struct (32 bytes)
	type ProfileEvent struct {
		Timestamp     uint64 // 8 bytes
		PID           int32  // 4 bytes
		TID           int32  // 4 bytes
		UserStackID   int32  // 4 bytes
		KernelStackID int32  // 4 bytes
		CPU           uint32 // 4 bytes
		Flags         uint32 // 4 bytes
	}

	// 8MB ring buffer / 32 bytes per event = 262,144 events
	capacity := 262144
	rb, _ := ringbuffer.New[ProfileEvent](capacity)

	event := ProfileEvent{
		Timestamp:     1234567890,
		PID:           1234,
		TID:           5678,
		UserStackID:   100,
		KernelStackID: 200,
		CPU:           0,
		Flags:         0,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rb.Push(event)
	}

	// Report useful metrics
	b.ReportMetric(float64(capacity*32)/(1024*1024), "MB_capacity")
	b.ReportMetric(32, "bytes/event")
}

// BenchmarkRingBuffer_Overflow benchmarks performance when buffer overflows
func BenchmarkRingBuffer_Overflow(b *testing.B) {
	rb, _ := ringbuffer.New[int](100)

	// Pre-fill to ensure we're always overwriting
	for i := 0; i < 100; i++ {
		rb.Push(i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rb.Push(i)
	}
}

// BenchmarkRingBuffer_ZeroAllocation verifies zero allocations in hot path
func BenchmarkRingBuffer_ZeroAllocation(b *testing.B) {
	rb, _ := ringbuffer.New[int](1000)

	// Pre-fill
	for i := 0; i < 500; i++ {
		rb.Push(i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	// This should show 0 allocs/op
	for i := 0; i < b.N; i++ {
		rb.Push(i)
		_ = rb.Len()
		_ = rb.Cap()
	}
}
