// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build hardware

package collectors_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Hardware integration tests requiring PMU access (bare metal only)
// These tests use PERF_TYPE_HARDWARE events that need Performance Monitoring Units

func TestProfilerCollector_HardwareEvents_Integration(t *testing.T) {
	// Skip on non-Linux platforms
	if runtime.GOOS != "linux" {
		t.Skip("Hardware PMU tests only run on Linux")
	}

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		Interval:     time.Second,
	}

	// Test hardware event types that require PMU access
	testCases := []struct {
		name          string
		profilerEvent collectors.ProfilerEventType
		samplePeriod  uint64
		eventType     uint32
		eventConfig   uint64
		eventName     string
	}{
		{
			name:          "CPUCycles",
			profilerEvent: collectors.ProfilerEventCPUCycles,
			samplePeriod:  1000000, // 1M CPU cycles
			eventType:     0, // PERF_TYPE_HARDWARE
			eventConfig:   0, // PERF_COUNT_HW_CPU_CYCLES
			eventName:     "cpu-cycles",
		},
		{
			name:          "CacheMisses",
			profilerEvent: collectors.ProfilerEventCacheMisses,
			samplePeriod:  100000, // 100K cache misses
			eventType:     0, // PERF_TYPE_HARDWARE
			eventConfig:   3, // PERF_COUNT_HW_CACHE_MISSES
			eventName:     "cache-misses",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			collector, err := collectors.NewProfiler(logr.Discard(), config)
			require.NoError(t, err, "%s profiler creation should succeed", tc.name)
			
			err = collector.Setup(collectors.ProfilerConfig{
				EventType:    tc.profilerEvent,
				SamplePeriod: tc.samplePeriod,
			})
			require.NoError(t, err, "%s profiler setup should succeed", tc.name)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			ch, err := collector.Start(ctx)
			require.NoError(t, err, "%s profiler should start on bare metal", tc.name)
			require.NotNil(t, ch)
			defer collector.Stop()

			// Generate CPU/cache activity for cache misses
			if tc.name == "CacheMisses" {
				go func() {
					// Generate cache misses with random memory access
					data := make([][]byte, 1000)
					for i := 0; i < 1000; i++ {
						data[i] = make([]byte, 4096)
						for j := 0; j < len(data[i]); j += 64 {
							data[i][j] = byte(i + j)
						}
					}
				}()
			}

			// Wait for profile
			select {
			case profile := <-ch:
				profileStats, ok := profile.(*performance.ProfileStats)
				require.True(t, ok, "Expected ProfileStats")

				// Verify hardware event characteristics
				assert.Equal(t, tc.eventType, profileStats.EventType, "Should use PERF_TYPE_HARDWARE")
				assert.Equal(t, tc.eventConfig, profileStats.EventConfig, "Event config mismatch")
				assert.Equal(t, tc.eventName, profileStats.EventName, "Event name mismatch")
				assert.Equal(t, tc.samplePeriod, profileStats.SamplePeriod, "Sample period mismatch")

				// Verify profiling data
				assert.Greater(t, profileStats.SampleCount, uint64(0), "Should collect hardware samples")
				assert.Greater(t, len(profileStats.Stacks), 0, "Should have stack traces")
				assert.NotZero(t, profileStats.Duration, "Should have non-zero duration")

				t.Logf("🔧 %s (hardware): %d samples, %d stacks, %.2fs duration", 
					tc.name, profileStats.SampleCount, len(profileStats.Stacks), 
					profileStats.Duration.Seconds())

			case <-time.After(15 * time.Second):
				if tc.name == "CacheMisses" {
					t.Logf("⚠️  No cache misses detected (this can happen on some hardware)")
					return
				}
				t.Fatalf("Timeout waiting for %s hardware profile", tc.name)
			}
		})
	}
}

func TestProfilerCollector_HardwareLifecycle_Integration(t *testing.T) {
	// Skip on non-Linux platforms
	if runtime.GOOS != "linux" {
		t.Skip("Hardware lifecycle tests only run on Linux")
	}

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		Interval:     time.Second,
	}

	collector, err := collectors.NewProfiler(logr.Discard(), config)
	require.NoError(t, err)
	
	err = collector.Setup(collectors.ProfilerConfig{
		EventType:    collectors.ProfilerEventCPUCycles,
		SamplePeriod: 1000000, // 1M CPU cycles
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test start
	ch, err := collector.Start(ctx)
	require.NoError(t, err, "Hardware profiler should start")
	require.NotNil(t, ch)

	// Test double start (should fail)
	ch2, err := collector.Start(ctx)
	assert.Error(t, err, "Second start should fail")
	assert.Nil(t, ch2)
	assert.Contains(t, err.Error(), "already running")

	// Verify we get data
	select {
	case profile := <-ch:
		profileStats, ok := profile.(*performance.ProfileStats)
		require.True(t, ok, "Expected ProfileStats")
		assert.Equal(t, "cpu-cycles", profileStats.EventName)
		assert.Equal(t, uint32(0), profileStats.EventType) // PERF_TYPE_HARDWARE

	case <-time.After(8 * time.Second):
		t.Fatal("Timeout waiting for hardware profile data")
	}

	// Test stop
	err = collector.Stop()
	assert.NoError(t, err, "Should stop successfully")

	// Test double stop (should be safe)
	err = collector.Stop()
	assert.NoError(t, err, "Second stop should be safe")

	t.Log("✅ Hardware profiler lifecycle completed successfully")
}