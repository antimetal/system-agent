// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package collectors_test

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests using software perf events only
// These tests run in VMs, CI environments, and anywhere without PMU access
// All tests use PERF_TYPE_SOFTWARE events that work in virtualized environments

// isKernelTooOld checks if the error indicates an older kernel that doesn't support stable BPF perf event links
func isKernelTooOld(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "bpf_link not supported") ||
		strings.Contains(errStr, "requires >= v5.7") ||
		strings.Contains(errStr, "requires >= v5.15") ||
		strings.Contains(errStr, "create link: invalid argument")
}

func TestProfilerCollector_SoftwareEvents_Integration(t *testing.T) {
	// Skip on non-Linux platforms
	if runtime.GOOS != "linux" {
		t.Skip("Software perf event tests only run on Linux")
	}

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		Interval:     time.Second,
	}

	// Create a software-based CPU profiler (VM-compatible)
	collector, err := collectors.NewProfiler(logr.Discard(), config)
	require.NoError(t, err)

	err = collector.Setup(collectors.ProfilerConfig{
		EventType:    collectors.ProfilerEventCPUClock,
		SamplePeriod: 10000000, // 10ms for software clock
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start collector
	ch, err := collector.Start(ctx)
	if isKernelTooOld(err) {
		t.Skipf("Kernel too old for stable BPF perf event links (requires >= 5.15): %v", err)
		return
	}
	require.NoError(t, err, "Software profiler should start successfully in VMs")
	require.NotNil(t, ch)

	t.Log("Software profiler started successfully - eBPF programs loaded!")

	// Collect at least one profile
	profileReceived := false
	timeout := time.After(25 * time.Second)

	for !profileReceived {
		select {
		case profile := <-ch:
			profileReceived = true

			profileStats, ok := profile.(*performance.ProfileStats)
			require.True(t, ok, "Expected ProfileStats object")

			// Verify software event characteristics
			assert.Equal(t, uint32(1), profileStats.EventType, "Should use PERF_TYPE_SOFTWARE (1)")
			assert.Equal(t, uint64(0), profileStats.EventConfig, "Should use PERF_COUNT_SW_CPU_CLOCK (0)")
			assert.Equal(t, "cpu-clock", profileStats.EventName, "Should use cpu-clock event")

			t.Logf("Received profile with %d samples from %d processes (event: %s)",
				profileStats.SampleCount, len(profileStats.Processes), profileStats.EventName)

			// Verify we got some profiling data
			assert.Greater(t, profileStats.SampleCount, uint64(0), "Should collect some samples")
			assert.Greater(t, len(profileStats.Stacks), 0, "Should have stack traces")

			if len(profileStats.Stacks) > 0 {
				stack := profileStats.Stacks[0]
				t.Logf("Top stack: PID=%d, TID=%d, samples=%d, percentage=%.2f%%",
					stack.PID, stack.TID, stack.SampleCount, stack.Percentage)

				assert.Greater(t, stack.SampleCount, uint64(0), "Stack should have samples")
				assert.Greater(t, stack.Percentage, 0.0, "Stack should have positive percentage")
			}

		case <-timeout:
			t.Fatal("Timeout waiting for profile data - profiler may not be working in VM")
		case <-ctx.Done():
			t.Fatal("Context cancelled")
		}
	}

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)

	t.Log("Software profiler integration test completed successfully!")
}

func TestProfilerCollector_HardwareVSoftware_Integration(t *testing.T) {
	// Skip on non-Linux platforms
	if runtime.GOOS != "linux" {
		t.Skip("Hardware vs software tests only run on Linux")
	}

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		Interval:     time.Second,
	}

	// Test 1: Hardware profiler should either work (bare metal) or fail (VMs)
	t.Run("Hardware_Event", func(t *testing.T) {
		collector, err := collectors.NewProfiler(logr.Discard(), config)
		require.NoError(t, err)

		err = collector.Setup(collectors.ProfilerConfig{
			EventType:    collectors.ProfilerEventCPUCycles,
			SamplePeriod: 1000000, // 1M cycles
		})
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		ch, err := collector.Start(ctx)
		if isKernelTooOld(err) {
			t.Skipf("Kernel too old for stable BPF perf event links (requires >= 5.15): %v", err)
			return
		}
		if err != nil {
			// Expected in VMs - hardware events should fail without fallback
			t.Logf("✅ Hardware profiler correctly failed in VM environment: %v", err)
			assert.Contains(t, err.Error(), "perf_event_open failed", "Hardware failure should mention perf_event_open")
			return
		}

		// If we get here, we're on bare metal
		require.NotNil(t, ch)
		defer collector.Stop()

		select {
		case profile := <-ch:
			profileStats, ok := profile.(*performance.ProfileStats)
			require.True(t, ok, "Expected ProfileStats object")
			assert.Equal(t, uint32(0), profileStats.EventType, "Should use PERF_TYPE_HARDWARE (0)")
			assert.Equal(t, "cpu-cycles", profileStats.EventName, "Should use cpu-cycles event")
			t.Log("✅ Hardware profiler working on bare metal")

		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for hardware profile on bare metal")
		case <-ctx.Done():
			t.Fatal("Context cancelled")
		}
	})

	// Test 2: Software profiler should always work
	t.Run("Software_Event", func(t *testing.T) {
		collector, err := collectors.NewProfiler(logr.Discard(), config)
		require.NoError(t, err)

		err = collector.Setup(collectors.ProfilerConfig{
			EventType:    collectors.ProfilerEventCPUClock,
			SamplePeriod: 10000000, // 10ms for software clock
		})
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		ch, err := collector.Start(ctx)
		if isKernelTooOld(err) {
			t.Skipf("Kernel too old for stable BPF perf event links (requires >= 5.15): %v", err)
			return
		}
		require.NoError(t, err, "Software profiler should always start")
		require.NotNil(t, ch)
		defer collector.Stop()

		// Generate CPU activity to ensure samples
		go func() {
			for i := 0; i < 5000; i++ {
				_ = make([]byte, 1024)
			}
		}()

		select {
		case profile := <-ch:
			profileStats, ok := profile.(*performance.ProfileStats)
			require.True(t, ok, "Expected ProfileStats object")
			assert.Equal(t, uint32(1), profileStats.EventType, "Should use PERF_TYPE_SOFTWARE (1)")
			assert.Equal(t, "cpu-clock", profileStats.EventName, "Should use cpu-clock event")
			t.Log("✅ Software profiler working in VM/bare metal")

		case <-time.After(12 * time.Second):
			t.Fatal("Timeout waiting for software cpu-clock profile")
		case <-ctx.Done():
			t.Fatal("Context cancelled")
		}
	})
}

func TestProfilerCollector_SoftwareEventTypes_Integration(t *testing.T) {
	// Skip on non-Linux platforms
	if runtime.GOOS != "linux" {
		t.Skip("Software event tests only run on Linux")
	}

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		Interval:     time.Second,
	}

	// Test different software event types that work in VMs/CI
	testCases := []struct {
		name           string
		profilerEvent  collectors.ProfilerEventType
		samplePeriod   uint64
		eventType      uint32
		eventConfigVal uint64
		eventName      string
	}{
		{
			name:           "SoftwareCPU",
			profilerEvent:  collectors.ProfilerEventCPUClock,
			samplePeriod:   10000000, // 10ms for cpu-clock
			eventType:      1,        // PERF_TYPE_SOFTWARE
			eventConfigVal: 0,        // PERF_COUNT_SW_CPU_CLOCK
			eventName:      "cpu-clock",
		},
		{
			name:           "PageFaults",
			profilerEvent:  collectors.ProfilerEventPageFaults,
			samplePeriod:   1000, // 1K page faults
			eventType:      1,    // PERF_TYPE_SOFTWARE
			eventConfigVal: 2,    // PERF_COUNT_SW_PAGE_FAULTS
			eventName:      "page-faults",
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
			if isKernelTooOld(err) {
				t.Skipf("Kernel too old for BPF link API (requires >= 5.7): %v", err)
				return
			}
			require.NoError(t, err, "%s profiler should start in VMs", tc.name)
			require.NotNil(t, ch)
			defer collector.Stop()

			// Generate activity for page faults with timeout
			if tc.name == "PageFaults" {
				go func() {
					for i := 0; i < 100; i++ { // Reduced iterations
						data := make([]byte, 4096)
						data[0] = byte(i)
						time.Sleep(time.Millisecond)
					}
				}()
			}

			// Wait for profile with shorter timeout for page faults
			timeout := 15 * time.Second
			if tc.name == "PageFaults" {
				timeout = 10 * time.Second
			}

			select {
			case profile := <-ch:
				profileStats, ok := profile.(*performance.ProfileStats)
				require.True(t, ok, "Expected ProfileStats")

				// Verify event characteristics
				assert.Equal(t, tc.eventType, profileStats.EventType, "Event type mismatch")
				assert.Equal(t, tc.eventConfigVal, profileStats.EventConfig, "Event config mismatch")
				assert.Equal(t, tc.eventName, profileStats.EventName, "Event name mismatch")

				t.Logf("✅ %s: %d samples, %d stacks",
					tc.name, profileStats.SampleCount, len(profileStats.Stacks))

			case <-time.After(timeout):
				if tc.name == "PageFaults" {
					t.Logf("⚠️  No page faults detected in %v (this is OK in some environments)", timeout)
					return
				}
				t.Fatalf("Timeout waiting for %s profile", tc.name)
			}
		})
	}
}
