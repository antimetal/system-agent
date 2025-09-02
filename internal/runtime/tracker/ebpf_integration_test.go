// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration && linux

package tracker

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/kernel"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestEBPFTrackerIntegration tests the eBPF tracker with real kernel events
func TestEBPFTrackerIntegration(t *testing.T) {
	// Check kernel version for eBPF support
	kv, err := kernel.GetCurrentVersion()
	require.NoError(t, err, "Failed to get kernel version")
	
	if !kv.IsAtLeast(4, 18) {
		t.Skip("eBPF tracker requires kernel 4.18+")
	}

	// Create logger
	zapLogger, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLogger)

	// Create tracker config
	config := TrackerConfig{
		ReconciliationInterval: 5 * time.Minute,
		EventBufferSize:        1000,
		DebounceInterval:       50 * time.Millisecond,
		CgroupPath:             "/sys/fs/cgroup",
	}

	// Create eBPF tracker
	tracker, err := NewEBPFTracker(logger, config)
	require.NoError(t, err, "Failed to create eBPF tracker")

	// Start tracker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	events, err := tracker.Run(ctx)
	require.NoError(t, err, "Failed to start eBPF tracker")

	// Collect events for a bit
	collectedEvents := make([]RuntimeEvent, 0)
	eventCollector := func() {
		timeout := time.After(2 * time.Second)
		for {
			select {
			case event := <-events:
				collectedEvents = append(collectedEvents, event)
			case <-timeout:
				return
			}
		}
	}

	// Start collecting events in background
	go eventCollector()

	// Generate some process events
	time.Sleep(100 * time.Millisecond) // Let collector start

	// Execute some commands to generate events
	testCommands := []string{
		"echo 'test1'",
		"ls /tmp",
		"cat /proc/version",
	}

	for _, cmd := range testCommands {
		exec.Command("sh", "-c", cmd).Run()
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for events to be collected
	time.Sleep(2 * time.Second)

	// Verify we got some events
	assert.NotEmpty(t, collectedEvents, "Should have collected some events")

	// Check event types
	hasExecEvent := false
	hasExitEvent := false

	for _, event := range collectedEvents {
		t.Logf("Event: Type=%v PID=%d Command=%s", 
			event.Type, event.ProcessPID, event.ProcessComm)
		
		switch event.Type {
		case EventTypeProcessCreate:
			hasExecEvent = true
			// Verify event has required fields
			assert.NotZero(t, event.ProcessPID, "Process PID should not be zero")
			assert.NotEmpty(t, event.ProcessComm, "Process command should not be empty")
			assert.NotZero(t, event.Timestamp, "Timestamp should not be zero")
		case EventTypeProcessExit:
			hasExitEvent = true
			assert.NotZero(t, event.ProcessPID, "Exit event should have PID")
		}
	}

	assert.True(t, hasExecEvent, "Should have captured at least one exec event")
	assert.True(t, hasExitEvent, "Should have captured at least one exit event")
}

// TestEBPFTrackerSnapshot tests the snapshot functionality
func TestEBPFTrackerSnapshot(t *testing.T) {
	// Check kernel version
	kv, err := kernel.GetCurrentVersion()
	require.NoError(t, err)
	
	if !kv.IsAtLeast(4, 18) {
		t.Skip("eBPF tracker requires kernel 4.18+")
	}

	zapLogger, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLogger)

	config := TrackerConfig{
		CgroupPath: "/sys/fs/cgroup",
	}

	tracker, err := NewEBPFTracker(logger, config)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = tracker.Run(ctx)
	require.NoError(t, err)

	// Take a snapshot
	snapshot, err := tracker.Snapshot(ctx)
	require.NoError(t, err, "Snapshot should succeed")
	
	assert.NotNil(t, snapshot, "Snapshot should not be nil")
	assert.NotZero(t, snapshot.Timestamp, "Snapshot should have timestamp")
	
	// The snapshot should at least discover some containers if running in container
	// But we won't fail if there are none (might be running on bare metal)
	t.Logf("Snapshot found %d containers", len(snapshot.Containers))
}

// TestTrackerDefaultMode tests default tracker creation
func TestTrackerDefaultMode(t *testing.T) {
	zapLogger, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLogger)

	// Create tracker with default config
	config := TrackerConfig{
		CgroupPath: "/sys/fs/cgroup",
	}

	tracker, err := NewTracker(logger, config)
	require.NoError(t, err, "Should create a tracker with default config")
	
	// Verify we can get a snapshot from the tracker
	ctx := context.Background()
	snapshot, err := tracker.Snapshot(ctx)
	require.NoError(t, err, "Should be able to get snapshot")
	assert.NotNil(t, snapshot, "Snapshot should not be nil")
	
	t.Logf("Tracker created successfully")
}