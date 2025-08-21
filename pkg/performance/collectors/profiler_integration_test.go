// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package collectors

import (
	"context"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/kernel"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfilerIntegration_BasicProfiling(t *testing.T) {
	// Check kernel version requirement (5.8+ for ring buffer)
	kernelVersion, err := kernel.GetCurrentVersion()
	require.NoError(t, err, "Failed to get kernel version")

	if !kernelVersion.IsAtLeast(5, 8) {
		t.Skipf("Profiler requires kernel 5.8+ for ring buffer support, current kernel is %s", kernelVersion.String())
	}

	logger := testr.New(t)
	config := performance.CollectionConfig{
		Interval:     time.Second,
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
	}

	collector, err := NewProfilerCollector(logger, config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start profiling
	dataChan, err := collector.Start(ctx)
	require.NoError(t, err, "Profiler should start successfully in integration test")
	require.NotNil(t, dataChan)

	// Collect some events
	eventCount := 0
	timeout := time.After(5 * time.Second)

	for eventCount < 10 {
		select {
		case event := <-dataChan:
			if event != nil {
				profileEvent, ok := event.(*ProfileEvent)
				require.True(t, ok, "Event should be ProfileEvent type")

				// Validate event structure
				assert.Greater(t, profileEvent.Timestamp, uint64(0), "Should have valid timestamp")
				assert.NotEqual(t, int32(0), profileEvent.PID, "Should have valid PID")
				assert.LessOrEqual(t, profileEvent.Cpu, uint32(1000), "CPU should be reasonable")

				eventCount++
				t.Logf("Received event %d: PID=%d, CPU=%d, Timestamp=%d",
					eventCount, profileEvent.PID, profileEvent.Cpu, profileEvent.Timestamp)
			}
		case <-timeout:
			t.Logf("Timeout after collecting %d events", eventCount)
			goto cleanup
		case <-ctx.Done():
			t.Logf("Context cancelled after collecting %d events", eventCount)
			goto cleanup
		}
	}

cleanup:
	// Stop profiling
	err = collector.Stop()
	assert.NoError(t, err)

	// Should have collected some events
	assert.Greater(t, eventCount, 0, "Should have collected at least some profiling events")
}

// TestProfilerIntegration_EventEnumeration is disabled - event enumeration was removed
/*
func TestProfilerIntegration_EventEnumeration(t *testing.T) {
	// Check kernel version requirement (5.8+ for ring buffer)
	kernelVersion, err := kernel.GetCurrentVersion()
	require.NoError(t, err, "Failed to get kernel version")

	if !kernelVersion.IsAtLeast(5, 8) {
		t.Skipf("Profiler requires kernel 5.8+ for ring buffer support, current kernel is %s", kernelVersion.String())
	}
	t.Skip("Event enumeration functions were removed")
}
*/

// TestProfilerIntegration_EventValidation is disabled - event validation was removed
/*
func TestProfilerIntegration_EventValidation(t *testing.T) {
	// Check kernel version requirement (5.8+ for ring buffer)
	kernelVersion, err := kernel.GetCurrentVersion()
	require.NoError(t, err, "Failed to get kernel version")

	if !kernelVersion.IsAtLeast(5, 8) {
		t.Skipf("Profiler requires kernel 5.8+ for ring buffer support, current kernel is %s", kernelVersion.String())
	}
	t.Skip("Event validation functions were removed")
}
*/

func TestProfilerIntegration_ErrorHandling(t *testing.T) {
	// Check kernel version requirement (5.8+ for ring buffer)
	kernelVersion, err := kernel.GetCurrentVersion()
	require.NoError(t, err, "Failed to get kernel version")

	if !kernelVersion.IsAtLeast(5, 8) {
		t.Skipf("Profiler requires kernel 5.8+ for ring buffer support, current kernel is %s", kernelVersion.String())
	}
	logger := testr.New(t)
	config := performance.CollectionConfig{
		Interval: time.Second,
	}

	collector, err := NewProfilerCollector(logger, config)
	require.NoError(t, err)

	// Event name lookup functions were removed - skip these tests

	// Test multiple start attempts (should fail on second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dataChan1, err := collector.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start profiler (expected in some environments): %v", err)
	}
	defer collector.Stop()

	require.NotNil(t, dataChan1)

	// Second start should fail
	dataChan2, err := collector.Start(ctx)
	assert.Error(t, err, "Second start should fail")
	assert.Nil(t, dataChan2)
}

func BenchmarkProfileEventParsing(b *testing.B) {
	// Create realistic 32-byte event data
	eventData := make([]byte, 32)
	// Fill with some realistic values
	timestamp := uint64(time.Now().UnixNano())
	pid := int32(1234)
	tid := int32(1235)
	cpu := uint32(0)

	// Pack into bytes (little endian)
	eventData[0] = byte(timestamp)
	eventData[1] = byte(timestamp >> 8)
	eventData[2] = byte(timestamp >> 16)
	eventData[3] = byte(timestamp >> 24)
	eventData[4] = byte(timestamp >> 32)
	eventData[5] = byte(timestamp >> 40)
	eventData[6] = byte(timestamp >> 48)
	eventData[7] = byte(timestamp >> 56)

	eventData[8] = byte(pid)
	eventData[9] = byte(pid >> 8)
	eventData[10] = byte(pid >> 16)
	eventData[11] = byte(pid >> 24)

	eventData[12] = byte(tid)
	eventData[13] = byte(tid >> 8)
	eventData[14] = byte(tid >> 16)
	eventData[15] = byte(tid >> 24)

	eventData[24] = byte(cpu)
	eventData[25] = byte(cpu >> 8)
	eventData[26] = byte(cpu >> 16)
	eventData[27] = byte(cpu >> 24)

	// Create a no-op logger for benchmarking
	logger := logr.Discard()
	config := performance.CollectionConfig{Interval: time.Second}
	collector, err := NewProfilerCollector(logger, config)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := collector.parseEvent(eventData)
		if err != nil {
			b.Fatalf("Parse error: %v", err)
		}
	}
}
