// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package collectors_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/kernel"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/cilium/ebpf/rlimit"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecSnoopCollector_Integration(t *testing.T) {
	// Check kernel version first - ExecSnoop requires 5.8+ for ring buffer support
	currentKernel, err := kernel.GetCurrentVersion()
	require.NoError(t, err, "Failed to get current kernel version")

	if !currentKernel.IsAtLeast(5, 8) {
		t.Skipf("ExecSnoop collector requires kernel 5.8+ for ring buffer support, current kernel is %s", currentKernel.String())
	}

	// Remove memory limit for eBPF
	err = rlimit.RemoveMemlock()
	require.NoError(t, err, "Failed to remove memlock limit - integration tests require proper permissions")

	logger := logr.Discard()
	config := performance.DefaultCollectionConfig()

	// Test loading and capturing real exec events
	t.Run("Capture Real Exec Events", func(t *testing.T) {
		collector, err := collectors.NewExecSnoopCollector(logger, config, "")
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Start the collector
		eventChan, err := collector.Start(ctx)
		require.NoError(t, err, "Failed to start execsnoop collector")
		// Context cancellation will trigger cleanup

		// Collect events
		events := make([]*collectors.ExecEvent, 0)
		done := make(chan struct{})

		go func() {
			for {
				select {
				case event := <-eventChan:
					if execEvent, ok := event.(*collectors.ExecEvent); ok {
						events = append(events, execEvent)
						// Look for our test commands
						if execEvent.Command == "echo" || execEvent.Command == "true" {
							t.Logf("Captured test command: %s (PID: %d, Args: %v)",
								execEvent.Command, execEvent.PID, execEvent.Args)
						}
					}
				case <-ctx.Done():
					close(done)
					return
				}
			}
		}()

		// Give the BPF program time to attach
		time.Sleep(100 * time.Millisecond)

		// Trigger some exec events
		testCommands := [][]string{
			{"echo", "test1"},
			{"true"},
			{"echo", "test2", "with", "args"},
		}

		for _, cmdArgs := range testCommands {
			cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
			_ = cmd.Run() // Ignore errors - we just want to trigger exec events
		}

		// Wait a bit for events to be captured
		time.Sleep(500 * time.Millisecond)
		cancel()
		<-done

		// Verify we captured some events
		assert.NotEmpty(t, events, "Should have captured at least one exec event")

		// Check if we captured our test commands
		foundEcho := false
		foundTrue := false
		for _, event := range events {
			if event.Command == "echo" {
				foundEcho = true
				t.Logf("Found echo command: PID=%d, Args=%v", event.PID, event.Args)
			}
			if event.Command == "true" {
				foundTrue = true
				t.Logf("Found true command: PID=%d", event.PID)
			}

			// Verify event fields are populated
			assert.NotZero(t, event.Timestamp)
			assert.NotEmpty(t, event.Command)
			assert.NotZero(t, event.PID)
		}

		assert.True(t, foundEcho || foundTrue, "Should have captured at least one of our test commands")
	})

	// Test BPF program lifecycle
	t.Run("BPF Program Lifecycle", func(t *testing.T) {
		collector, err := collectors.NewExecSnoopCollector(logger, config, "")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start should succeed
		eventChan, err := collector.Start(ctx)
		require.NoError(t, err)
		require.NotNil(t, eventChan)

		// Verify collector is active
		assert.Equal(t, performance.CollectorStatusActive, collector.Status())

		// Context cancellation should trigger cleanup
		cancel()
		// Give time for cleanup
		time.Sleep(100 * time.Millisecond)

		// Verify collector is disabled after context cancellation
		assert.Equal(t, performance.CollectorStatusDisabled, collector.Status())
	})
}
