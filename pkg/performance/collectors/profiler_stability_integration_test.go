// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package collectors_test

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProfiler_MemoryStability_Integration tests memory stability over time
func TestProfiler_MemoryStability_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Get test duration from environment or default to 5 minutes
	duration := 5 * time.Minute
	if d := getTestDuration(); d > 0 {
		duration = d
	}

	t.Logf("Running memory stability test for %v", duration)

	// Create profiler with software event (works in VMs)
	logger := logr.Discard()
	config := performance.CollectionConfig{
		Interval:    time.Second,
		HostSysPath: "/sys",
	}

	profiler, err := collectors.NewProfiler(logger, config)
	require.NoError(t, err)
	defer profiler.Stop()

	// Setup with CPU clock event (software, works everywhere)
	setupConfig := collectors.ProfilerConfig{
		Event: collectors.PerfEventConfig{
			Name:         "cpu-clock",
			Type:         collectors.PERF_TYPE_SOFTWARE,
			Config:       collectors.PERF_COUNT_SW_CPU_CLOCK,
			SamplePeriod: 1000000, // 1ms
		},
	}

	err = profiler.Setup(setupConfig)
	require.NoError(t, err)

	// Record initial memory
	var initialMem runtime.MemStats
	runtime.ReadMemStats(&initialMem)
	t.Logf("Initial heap: %.2f MB", float64(initialMem.HeapAlloc)/1024/1024)

	// Start profiling
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	outputChan, err := profiler.Start(ctx)
	require.NoError(t, err)

	// Monitor memory periodically
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var maxHeap uint64
	var eventCount int64
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			goto done
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			if m.HeapAlloc > maxHeap {
				maxHeap = m.HeapAlloc
			}

			elapsed := time.Since(startTime)
			t.Logf("[%v] Heap: %.2f MB, Events: %d",
				elapsed.Round(time.Second),
				float64(m.HeapAlloc)/1024/1024,
				eventCount)
		case event := <-outputChan:
			if event != nil {
				eventCount++
			}
		}
	}

done:
	// Final memory check
	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)

	heapGrowth := int64(finalMem.HeapAlloc) - int64(initialMem.HeapAlloc)

	t.Logf("\n=== Memory Stability Results ===")
	t.Logf("Duration: %v", duration)
	t.Logf("Initial Heap: %.2f MB", float64(initialMem.HeapAlloc)/1024/1024)
	t.Logf("Final Heap: %.2f MB", float64(finalMem.HeapAlloc)/1024/1024)
	t.Logf("Max Heap: %.2f MB", float64(maxHeap)/1024/1024)
	t.Logf("Heap Growth: %.2f MB", float64(heapGrowth)/1024/1024)
	t.Logf("Total Events: %d", eventCount)

	// Assert reasonable memory growth
	maxGrowth := int64(10 * 1024 * 1024) // 10MB
	assert.Less(t, heapGrowth, maxGrowth,
		"Memory growth %.2f MB exceeds limit of %.2f MB",
		float64(heapGrowth)/1024/1024,
		float64(maxGrowth)/1024/1024)
}

// TestProfiler_HeavyLoad_Integration tests under heavy load
func TestProfiler_HeavyLoad_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := logr.Discard()
	config := performance.CollectionConfig{
		Interval:    100 * time.Millisecond, // Fast collection
		HostSysPath: "/sys",
	}

	profiler, err := collectors.NewProfiler(logger, config)
	require.NoError(t, err)
	defer profiler.Stop()

	// Setup with high frequency sampling
	setupConfig := collectors.ProfilerConfig{
		Event: collectors.PerfEventConfig{
			Name:         "cpu-clock",
			Type:         collectors.PERF_TYPE_SOFTWARE,
			Config:       collectors.PERF_COUNT_SW_CPU_CLOCK,
			SamplePeriod: 100000, // 100 microseconds - 10KHz
		},
	}

	err = profiler.Setup(setupConfig)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	outputChan, err := profiler.Start(ctx)
	require.NoError(t, err)

	// Generate CPU load
	numWorkers := runtime.NumCPU()
	for i := 0; i < numWorkers; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					// CPU intensive work
					_ = fibonacci(30)
				}
			}
		}()
	}

	// Count events
	var eventCount int64
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-outputChan:
				if event != nil {
					eventCount++
				}
			}
		}
	}()

	<-ctx.Done()

	eventRate := float64(eventCount) / 30.0
	t.Logf("\n=== Heavy Load Results ===")
	t.Logf("Total Events: %d", eventCount)
	t.Logf("Event Rate: %.0f events/sec", eventRate)

	// Should handle high load
	assert.Greater(t, eventCount, int64(10000), "Should process many events under load")
}

func fibonacci(n int) int {
	if n <= 1 {
		return n
	}
	return fibonacci(n-1) + fibonacci(n-2)
}

func getTestDuration() time.Duration {
	if d := os.Getenv("PROFILER_STABILITY_DURATION"); d != "" {
		if duration, err := time.ParseDuration(d); err == nil {
			return duration
		}
	}
	return 0
}
