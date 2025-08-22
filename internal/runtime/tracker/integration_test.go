// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package tracker

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventDrivenTrackerIntegration(t *testing.T) {
	logger := logr.Discard()
	
	// Test with a config that should work on Linux systems
	config := TrackerConfig{
		Mode:             TrackerModeEventDriven,
		CgroupPath:       "/sys/fs/cgroup",
		EventBufferSize:  100,
		DebounceInterval: 5 * time.Millisecond,
	}

	tracker, err := NewEventDrivenTracker(logger, config)
	require.NoError(t, err)
	require.NotNil(t, tracker)

	// Test interface compliance
	assert.True(t, tracker.IsEventDriven())
	assert.NotNil(t, tracker.Events())

	// Test lifecycle
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = tracker.Start(ctx)
	if err != nil {
		// On non-Linux systems, this may fail due to missing /proc or /sys/fs/cgroup
		t.Skipf("Event-driven tracker requires Linux filesystem, got error: %v", err)
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Get snapshot
	snapshot, err := tracker.GetSnapshot()
	assert.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.False(t, snapshot.Timestamp.IsZero())

	// Check for events (there may or may not be any depending on system activity)
	select {
	case event := <-tracker.Events():
		t.Logf("Received event: %+v", event)
	case <-time.After(500 * time.Millisecond):
		t.Log("No events received within timeout (normal on quiet systems)")
	}

	// Stop tracker
	err = tracker.Stop()
	assert.NoError(t, err)
}

func TestFactoryCreatesEventDrivenTracker(t *testing.T) {
	logger := logr.Discard()
	
	config := TrackerConfig{
		Mode:             TrackerModeEventDriven,
		CgroupPath:       "/sys/fs/cgroup",
		EventBufferSize:  100,
		DebounceInterval: 5 * time.Millisecond,
	}

	tracker, err := NewRuntimeTracker(logger, config)
	if err != nil {
		// On non-Linux systems, this may fail
		t.Skipf("Event-driven tracker requires Linux filesystem, got error: %v", err)
	}

	require.NotNil(t, tracker)
	assert.True(t, tracker.IsEventDriven())
}

func TestCapabilityDetectionIntegration(t *testing.T) {
	// Test capability detection on the current system
	caps := DetectCapabilities("/sys/fs/cgroup")
	
	assert.NotNil(t, caps)
	
	// Verify the capability summary is well-formed
	summary := caps.GetCapabilitySummary()
	assert.Contains(t, summary, "Runtime Tracking Capabilities")
	assert.Contains(t, summary, "Recommended mode")
	
	// Log results for debugging
	t.Logf("Capability detection results:\n%s", summary)
	
	// The recommended mode should be one of the valid modes
	mode := caps.RecommendedMode()
	assert.Contains(t, []TrackerMode{
		TrackerModePolling,
		TrackerModeEventDriven,
	}, mode)
}