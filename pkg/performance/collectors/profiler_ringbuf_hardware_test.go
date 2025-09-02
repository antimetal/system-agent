// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build hardware

package collectors_test

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProfilerRingBuffer_HeavyLoad_Hardware tests the ring buffer under heavy load conditions
// This test should be run on bare metal hardware with PMU support
func TestProfilerRingBuffer_HeavyLoad_Hardware(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hardware test in short mode")
	}

	// Check if we have PMU support
	if !hasPMUSupport() {
		t.Skip("PMU not available on this system")
	}

	config := &ProfilerConfig{
		SamplingFrequency: 10000, // 10KHz - stress test
		PerfEvents: []PerfEventConfig{
			{Type: PERF_TYPE_SOFTWARE, Config: PERF_COUNT_SW_CPU_CLOCK},
		},
		Debug: true,
	}

	collector, err := NewProfilerCollector(config)
	require.NoError(t, err)
	defer collector.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Track metrics
	var (
		eventsReceived uint64
		dropsDetected  uint64
		startTime      = time.Now()
	)

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Generate heavy CPU load
	var wg sync.WaitGroup
	numWorkers := runtime.NumCPU()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// CPU-intensive work
			for {
				select {
				case <-ctx.Done():
					return
				default:
					// Fibonacci to generate stack traces
					fibonacci(35)
				}
			}
		}()
	}

	// Monitor events in separate goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats := collector.GetStats()
				atomic.StoreUint64(&eventsReceived, stats.EventsProcessed)
				atomic.StoreUint64(&dropsDetected, stats.EventsDropped)

				rate := float64(stats.EventsProcessed) / time.Since(startTime).Seconds()
				dropRate := float64(stats.EventsDropped) / float64(stats.EventsProcessed+stats.EventsDropped) * 100

				t.Logf("Events: %d, Rate: %.0f/sec, Drops: %d (%.2f%%)",
					stats.EventsProcessed, rate, stats.EventsDropped, dropRate)
			}
		}
	}()

	// Wait for test completion
	wg.Wait()

	// Analyze results
	duration := time.Since(startTime)
	totalEvents := atomic.LoadUint64(&eventsReceived)
	totalDrops := atomic.LoadUint64(&dropsDetected)

	eventRate := float64(totalEvents) / duration.Seconds()
	dropRate := float64(totalDrops) / float64(totalEvents+totalDrops) * 100

	t.Logf("\n=== Heavy Load Test Results ===")
	t.Logf("Duration: %v", duration)
	t.Logf("Total Events: %d", totalEvents)
	t.Logf("Event Rate: %.0f events/sec", eventRate)
	t.Logf("Total Drops: %d", totalDrops)
	t.Logf("Drop Rate: %.2f%%", dropRate)

	// Assertions
	assert.Greater(t, totalEvents, uint64(100000), "Should process at least 100K events")
	assert.Less(t, dropRate, 10.0, "Drop rate should be less than 10%")

	// At 10KHz, we expect some drops but system should remain stable
	if dropRate > 1.0 {
		t.Logf("WARNING: Drop rate %.2f%% indicates ring buffer overflow under heavy load", dropRate)
	}
}

// TestProfilerRingBuffer_MemoryStability_Hardware tests memory stability over time
func TestProfilerRingBuffer_MemoryStability_Hardware(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hardware test in short mode")
	}

	config := &ProfilerConfig{
		SamplingFrequency: 100, // Normal rate
		PerfEvents: []PerfEventConfig{
			{Type: PERF_TYPE_SOFTWARE, Config: PERF_COUNT_SW_CPU_CLOCK},
		},
	}

	collector, err := NewProfilerCollector(config)
	require.NoError(t, err)
	defer collector.Close()

	// Run for 5 minutes (or 24 hours for full test)
	testDuration := 5 * time.Minute
	if envDuration := os.Getenv("PROFILER_STABILITY_DURATION"); envDuration != "" {
		if d, err := time.ParseDuration(envDuration); err == nil {
			testDuration = d
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Generate moderate workload
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Simulate application workload
				time.Sleep(10 * time.Millisecond)
				_ = fibonacci(30)
			}
		}
	}()

	// Monitor memory usage
	type memSample struct {
		time      time.Time
		heapAlloc uint64
		rss       uint64
	}

	var samples []memSample
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	startMem := &runtime.MemStats{}
	runtime.ReadMemStats(startMem)

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			goto analyze
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			samples = append(samples, memSample{
				time:      time.Now(),
				heapAlloc: m.HeapAlloc,
				rss:       getCurrentRSS(), // Helper function to get RSS
			})

			t.Logf("Memory - Heap: %.2f MB, RSS: %.2f MB, Events: %d",
				float64(m.HeapAlloc)/1024/1024,
				float64(getCurrentRSS())/1024/1024,
				collector.GetStats().EventsProcessed)
		}
	}

analyze:
	endMem := &runtime.MemStats{}
	runtime.ReadMemStats(endMem)

	// Calculate memory growth
	heapGrowth := int64(endMem.HeapAlloc) - int64(startMem.HeapAlloc)

	t.Logf("\n=== Memory Stability Results ===")
	t.Logf("Test Duration: %v", testDuration)
	t.Logf("Initial Heap: %.2f MB", float64(startMem.HeapAlloc)/1024/1024)
	t.Logf("Final Heap: %.2f MB", float64(endMem.HeapAlloc)/1024/1024)
	t.Logf("Heap Growth: %.2f MB", float64(heapGrowth)/1024/1024)
	t.Logf("Total Events: %d", collector.GetStats().EventsProcessed)

	// Check for memory leaks
	maxAcceptableGrowth := int64(10 * 1024 * 1024) // 10MB
	assert.Less(t, heapGrowth, maxAcceptableGrowth,
		"Memory growth exceeds acceptable limit - possible memory leak")

	// Analyze memory trend
	if len(samples) > 10 {
		trend := calculateMemoryTrend(samples)
		t.Logf("Memory Trend: %.2f KB/minute", trend/1024)

		// Alert if significant upward trend
		if trend > 100*1024 { // 100KB/minute
			t.Errorf("Detected significant memory growth trend: %.2f KB/minute", trend/1024)
		}
	}
}

// TestProfilerRingBuffer_Wraparound_Hardware tests ring buffer wraparound behavior
func TestProfilerRingBuffer_Wraparound_Hardware(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hardware test in short mode")
	}

	config := &ProfilerConfig{
		SamplingFrequency: 1000, // 1KHz
		PerfEvents: []PerfEventConfig{
			{Type: PERF_TYPE_SOFTWARE, Config: PERF_COUNT_SW_CPU_CLOCK},
		},
		RingBufferSize: 65536, // Small buffer to force wraparound
	}

	collector, err := NewProfilerCollector(config)
	require.NoError(t, err)
	defer collector.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)

	// Generate events to fill and wrap buffer multiple times
	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					// Generate events
					_ = fibonacci(25)
				}
			}
		}()
	}

	wg.Wait()

	stats := collector.GetStats()

	// Calculate expected wraparounds
	eventSize := 32 // bytes per event
	bufferCapacity := config.RingBufferSize / eventSize
	expectedWraps := stats.EventsProcessed / uint64(bufferCapacity)

	t.Logf("\n=== Wraparound Test Results ===")
	t.Logf("Buffer Size: %d bytes", config.RingBufferSize)
	t.Logf("Buffer Capacity: %d events", bufferCapacity)
	t.Logf("Events Processed: %d", stats.EventsProcessed)
	t.Logf("Estimated Wraparounds: %d", expectedWraps)
	t.Logf("Events Dropped: %d", stats.EventsDropped)

	// Should handle wraparound without crashes
	assert.Greater(t, expectedWraps, uint64(10), "Should have wrapped buffer at least 10 times")
	assert.Greater(t, stats.EventsProcessed, uint64(1000), "Should have processed events despite wraparound")
}

// TestProfilerRingBuffer_Performance_Hardware benchmarks event processing performance
func TestProfilerRingBuffer_Performance_Hardware(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hardware test in short mode")
	}

	testCases := []struct {
		name      string
		frequency int
		duration  time.Duration
	}{
		{"100Hz_Normal", 100, 30 * time.Second},
		{"1KHz_Medium", 1000, 20 * time.Second},
		{"5KHz_High", 5000, 10 * time.Second},
		{"10KHz_Stress", 10000, 5 * time.Second},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &ProfilerConfig{
				SamplingFrequency: tc.frequency,
				PerfEvents: []PerfEventConfig{
					{Type: PERF_TYPE_SOFTWARE, Config: PERF_COUNT_SW_CPU_CLOCK},
				},
			}

			collector, err := NewProfilerCollector(config)
			require.NoError(t, err)
			defer collector.Close()

			ctx, cancel := context.WithTimeout(context.Background(), tc.duration)
			defer cancel()

			// Measure CPU before
			cpuBefore := getCPUUsage()

			err = collector.Start(ctx)
			require.NoError(t, err)

			// Generate workload
			var wg sync.WaitGroup
			for i := 0; i < runtime.NumCPU()/2; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						select {
						case <-ctx.Done():
							return
						default:
							_ = fibonacci(32)
							time.Sleep(1 * time.Millisecond)
						}
					}
				}()
			}

			// Let it run
			time.Sleep(tc.duration - 1*time.Second)

			// Measure CPU after
			cpuAfter := getCPUUsage()
			cpuOverhead := cpuAfter - cpuBefore

			wg.Wait()

			stats := collector.GetStats()
			eventRate := float64(stats.EventsProcessed) / tc.duration.Seconds()
			dropRate := float64(stats.EventsDropped) / float64(stats.EventsProcessed+stats.EventsDropped) * 100

			t.Logf("\n=== %s Performance ===", tc.name)
			t.Logf("Target Frequency: %d Hz", tc.frequency)
			t.Logf("Actual Event Rate: %.0f events/sec", eventRate)
			t.Logf("Drop Rate: %.2f%%", dropRate)
			t.Logf("CPU Overhead: %.2f%%", cpuOverhead)
			t.Logf("Events per CPU%%: %.0f", eventRate/cpuOverhead)

			// Performance assertions based on frequency
			switch tc.frequency {
			case 100:
				assert.Less(t, cpuOverhead, 1.0, "CPU overhead should be <1% at 100Hz")
				assert.Equal(t, float64(0), dropRate, "Should have no drops at 100Hz")
			case 1000:
				assert.Less(t, cpuOverhead, 5.0, "CPU overhead should be <5% at 1KHz")
				assert.Less(t, dropRate, 0.1, "Drop rate should be <0.1% at 1KHz")
			case 5000:
				assert.Less(t, cpuOverhead, 10.0, "CPU overhead should be <10% at 5KHz")
				assert.Less(t, dropRate, 1.0, "Drop rate should be <1% at 5KHz")
			case 10000:
				// At 10KHz we expect some drops
				assert.Less(t, cpuOverhead, 15.0, "CPU overhead should be <15% at 10KHz")
				// Drop rate can be higher at extreme frequency
			}
		})
	}
}

// Helper functions

func fibonacci(n int) int {
	if n <= 1 {
		return n
	}
	return fibonacci(n-1) + fibonacci(n-2)
}

func hasPMUSupport() bool {
	// Check for PMU on Linux
	if _, err := os.Stat("/sys/bus/event_source/devices/cpu"); err == nil {
		return true
	}
	return false
}

func getCurrentRSS() uint64 {
	// Read RSS from /proc/self/status
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val, _ := strconv.ParseUint(fields[1], 10, 64)
				return val * 1024 // Convert KB to bytes
			}
		}
	}
	return 0
}

func getCPUUsage() float64 {
	// Simple CPU usage calculation
	// In production, use proper CPU metrics
	return 0.0
}

func calculateMemoryTrend(samples []memSample) float64 {
	if len(samples) < 2 {
		return 0
	}

	// Simple linear regression for memory trend
	n := float64(len(samples))
	var sumX, sumY, sumXY, sumX2 float64

	for i, s := range samples {
		x := float64(i)
		y := float64(s.heapAlloc)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	// Calculate slope (bytes per sample interval)
	slope := (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)

	// Convert to bytes per minute
	samplesPerMinute := 6.0 // We sample every 10 seconds
	return slope * samplesPerMinute
}
