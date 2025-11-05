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
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProfilerRingBuffer_MemoryStability_Hardware tests memory stability over time
// This test can be run for extended periods (24h) by setting PROFILER_STABILITY_DURATION
func TestProfilerRingBuffer_MemoryStability_Hardware(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hardware test in short mode")
	}

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		Interval:     time.Second,
	}

	collector, err := collectors.NewProfiler(logr.Discard(), config)
	require.NoError(t, err)

	// Configure with software events (cpu-clock) for stability testing
	err = collector.Setup(collectors.NewProfilerConfigWithSamplePeriod(
		collectors.CPUClockEvent,
		10000000, // 10ms
	))
	require.NoError(t, err)

	// Run for 5 minutes (or longer if specified)
	testDuration := 5 * time.Minute
	if envDuration := os.Getenv("PROFILER_STABILITY_DURATION"); envDuration != "" {
		if d, err := time.ParseDuration(envDuration); err == nil {
			testDuration = d
		}
	}

	t.Logf("Running stability test for %v", testDuration)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	ch, err := collector.Start(ctx)
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Generate moderate workload
	stopWorkload := make(chan struct{})
	defer close(stopWorkload)

	go func() {
		for {
			select {
			case <-stopWorkload:
				return
			default:
				// Simulate application workload
				time.Sleep(10 * time.Millisecond)
				data := make([]byte, 1024)
				_ = data[0]
			}
		}
	}()

	// Monitor memory usage
	type memSample struct {
		time      time.Time
		heapAlloc uint64
		numGC     uint32
	}

	var samples []memSample
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	startMem := &runtime.MemStats{}
	runtime.ReadMemStats(startMem)

	t.Logf("Initial memory: Heap=%d MB", startMem.HeapAlloc/1024/1024)

	profileCount := 0

	for {
		select {
		case <-ctx.Done():
			// Test complete
			t.Logf("Stability test completed after %v", testDuration)

			// Analyze memory trend
			if len(samples) > 2 {
				firstSample := samples[0]
				lastSample := samples[len(samples)-1]

				heapGrowth := float64(lastSample.heapAlloc) - float64(firstSample.heapAlloc)
				heapGrowthMB := heapGrowth / 1024 / 1024
				gcCount := lastSample.numGC - firstSample.numGC

				t.Logf("\n=== Memory Stability Results ===")
				t.Logf("Duration: %v", testDuration)
				t.Logf("Profiles collected: %d", profileCount)
				t.Logf("Initial heap: %d MB", firstSample.heapAlloc/1024/1024)
				t.Logf("Final heap: %d MB", lastSample.heapAlloc/1024/1024)
				t.Logf("Heap growth: %.2f MB", heapGrowthMB)
				t.Logf("GC cycles: %d", gcCount)
				t.Logf("Samples collected: %d", len(samples))

				// Assert reasonable memory growth (< 100MB for long tests)
				maxGrowthMB := 100.0
				if testDuration < time.Hour {
					maxGrowthMB = 50.0
				}

				assert.Less(t, heapGrowthMB, maxGrowthMB,
					"Heap growth should be less than %.0f MB over %v", maxGrowthMB, testDuration)
			}
			return

		case <-ticker.C:
			// Sample memory
			mem := &runtime.MemStats{}
			runtime.ReadMemStats(mem)

			samples = append(samples, memSample{
				time:      time.Now(),
				heapAlloc: mem.HeapAlloc,
				numGC:     mem.NumGC,
			})

			t.Logf("Memory: Heap=%d MB, GC=%d, Profiles=%d",
				mem.HeapAlloc/1024/1024, mem.NumGC, profileCount)

		case event := <-ch:
			profileCount++

			// Verify we're getting valid profiles
			if profileCount%10 == 0 {
				profileStats, ok := event.Data.(*performance.ProfileStats)
				if ok {
					t.Logf("Profile #%d: %d samples, %d stacks",
						profileCount, profileStats.SampleCount, len(profileStats.Stacks))
				}
			}
		}
	}
}
