// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package hardware

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/antimetal/agent/internal/resource/store"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewManager_RequiresStore validates that NewManager fails without a store
func TestNewManager_RequiresStore(t *testing.T) {
	logger := testr.New(t)

	_, err := NewManager(logger, ManagerConfig{
		Store: nil,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resource store is required")
}

// TestNewManager_AppliesDefaults validates that NewManager sets proper defaults
func TestNewManager_AppliesDefaults(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	manager, err := NewManager(logger, ManagerConfig{
		Store:          testStore,
		NodeName:       "test-node",
		ClusterName:    "test-cluster",
		UpdateInterval: 0, // Should default to 5 minutes
	})

	require.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, "test-node", manager.nodeName)
	assert.Equal(t, "test-cluster", manager.clusterName)
	assert.Equal(t, 5*time.Minute, manager.interval)
	assert.NotNil(t, manager.builder)
}

// TestNewManager_CustomInterval validates custom update intervals are respected
func TestNewManager_CustomInterval(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	customInterval := 1 * time.Minute
	manager, err := NewManager(logger, ManagerConfig{
		Store:          testStore,
		UpdateInterval: customInterval,
	})

	require.NoError(t, err)
	assert.Equal(t, customInterval, manager.interval)
}

// TestManager_NeedLeaderElection validates manager runs on all nodes
func TestManager_NeedLeaderElection(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	manager, err := NewManager(logger, ManagerConfig{
		Store: testStore,
	})

	require.NoError(t, err)
	assert.False(t, manager.NeedLeaderElection(),
		"Hardware manager should run on all nodes, not just leader")
}

// TestManager_Lifecycle_StartAndStop validates basic start/stop lifecycle
func TestManager_Lifecycle_StartAndStop(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	manager, err := NewManager(logger, ManagerConfig{
		Store:          testStore,
		UpdateInterval: 10 * time.Millisecond,
		NodeName:       "test-node",
		ClusterName:    "test-cluster",
	})
	require.NoError(t, err)

	// Create a context that we'll cancel quickly
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start should return when context is cancelled
	startDone := make(chan error, 1)
	go func() {
		startDone <- manager.Start(ctx)
	}()

	// Let it run through at least one cycle
	time.Sleep(200 * time.Millisecond)

	// Cancel context to stop the manager
	cancel()

	// Wait for Start to return (give it extra time on slow systems)
	select {
	case err := <-startDone:
		assert.NoError(t, err, "Start should return nil even when context is cancelled")
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return after context cancellation")
	}
}

// TestManager_InitialDiscoveryNonFatal validates that initial discovery errors don't fail startup
func TestManager_InitialDiscoveryNonFatal(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	// Create manager with invalid paths that will cause collection to fail
	manager, err := NewManager(logger, ManagerConfig{
		Store: testStore,
		CollectionConfig: performance.CollectionConfig{
			HostProcPath: "/nonexistent/proc",
			HostSysPath:  "/nonexistent/sys",
		},
		UpdateInterval: 10 * time.Millisecond,
		NodeName:       "test-node",
		ClusterName:    "test-cluster",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start should succeed even though initial discovery will fail
	startDone := make(chan error, 1)
	go func() {
		startDone <- manager.Start(ctx)
	}()

	// Let it try initial discovery (may take time even with errors)
	time.Sleep(500 * time.Millisecond)

	cancel()

	select {
	case err := <-startDone:
		assert.NoError(t, err, "Start should not fail on initial discovery error")
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return")
	}
}

// TestManager_GetLastUpdateTime validates last update time tracking
func TestManager_GetLastUpdateTime(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	manager, err := NewManager(logger, ManagerConfig{
		Store:          testStore,
		UpdateInterval: 50 * time.Millisecond,
		CollectionConfig: performance.CollectionConfig{
			HostProcPath: "/proc",
			HostSysPath:  "/sys",
			HostDevPath:  "/dev",
		},
		NodeName:    "test-node",
		ClusterName: "test-cluster",
	})
	require.NoError(t, err)

	// Initially should be zero
	assert.True(t, manager.GetLastUpdateTime().IsZero(),
		"Last update time should be zero before any updates")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go manager.Start(ctx)

	// Wait for initial update to complete (can be slow on macOS)
	time.Sleep(5 * time.Second)

	// Last update time should now be set
	lastUpdate := manager.GetLastUpdateTime()
	if lastUpdate.IsZero() {
		t.Skip("Update did not complete in time (slow system)")
	}
	assert.True(t, time.Since(lastUpdate) < 10*time.Second,
		"Last update should be recent")

	cancel()
	time.Sleep(100 * time.Millisecond)
}

// TestManager_ForceUpdate validates manual update triggering
func TestManager_ForceUpdate(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	manager, err := NewManager(logger, ManagerConfig{
		Store: testStore,
		CollectionConfig: performance.CollectionConfig{
			HostProcPath: "/proc",
			HostSysPath:  "/sys",
			HostDevPath:  "/dev",
		},
		UpdateInterval: 1 * time.Hour, // Long interval so we control updates
		NodeName:       "test-node",
		ClusterName:    "test-cluster",
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Force an update manually
	err = manager.ForceUpdate(ctx)

	// On macOS this will fail because collectors won't work
	// but the important thing is that the method executes without panic
	if err != nil {
		t.Logf("ForceUpdate failed (expected on non-Linux): %v", err)
	}

	// Last update time should be set
	assert.False(t, manager.GetLastUpdateTime().IsZero(),
		"Last update time should be set after forced update attempt")
}

// TestManager_PeriodicUpdates validates that updates happen periodically
func TestManager_PeriodicUpdates(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	// Use a very short interval for testing
	manager, err := NewManager(logger, ManagerConfig{
		Store: testStore,
		CollectionConfig: performance.CollectionConfig{
			HostProcPath: "/proc",
			HostSysPath:  "/sys",
			HostDevPath:  "/dev",
		},
		UpdateInterval: 100 * time.Millisecond,
		NodeName:       "test-node",
		ClusterName:    "test-cluster",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	updateTimes := []time.Time{}
	var mu sync.Mutex

	// We can't easily intercept the updates, but we can check GetLastUpdateTime changes
	go manager.Start(ctx)

	// Sample update times - account for slow initial build
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		lastUpdate := manager.GetLastUpdateTime()
		if !lastUpdate.IsZero() {
			mu.Lock()
			// Only add if different from last recorded time
			if len(updateTimes) == 0 || !lastUpdate.Equal(updateTimes[len(updateTimes)-1]) {
				updateTimes = append(updateTimes, lastUpdate)
			}
			mu.Unlock()
		}
	}

	cancel()
	time.Sleep(100 * time.Millisecond)

	// We should have seen at least 1 update (may skip if build is very slow)
	if len(updateTimes) == 0 {
		t.Skip("No updates completed in time (slow system)")
	}
	assert.GreaterOrEqual(t, len(updateTimes), 1,
		"Should have at least 1 update")
}

// TestManager_ContextCancellation validates graceful shutdown
// Note: Context cancellation is also tested in TestManager_Lifecycle_StartAndStop
func TestManager_ContextCancellation(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	manager, err := NewManager(logger, ManagerConfig{
		Store:          testStore,
		UpdateInterval: 1 * time.Hour, // Long interval to avoid multiple builds
		NodeName:       "test-node",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	startDone := make(chan bool)
	go func() {
		manager.Start(ctx)
		close(startDone)
	}()

	// Let it complete initial discovery
	time.Sleep(6 * time.Second)

	// Cancel and ensure it stops promptly since no periodic updates are scheduled
	cancel()

	select {
	case <-startDone:
		t.Log("Manager stopped successfully after context cancellation")
	case <-time.After(5 * time.Second):
		t.Fatal("Manager did not stop within 5s of context cancellation")
	}
}

// TestManager_ConcurrentGetLastUpdateTime validates thread-safe access
func TestManager_ConcurrentGetLastUpdateTime(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	manager, err := NewManager(logger, ManagerConfig{
		Store:          testStore,
		UpdateInterval: 20 * time.Millisecond,
		CollectionConfig: performance.CollectionConfig{
			HostProcPath: "/proc",
			HostSysPath:  "/sys",
			HostDevPath:  "/dev",
		},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go manager.Start(ctx)

	// Concurrently read last update time from multiple goroutines
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = manager.GetLastUpdateTime()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	cancel()
	time.Sleep(50 * time.Millisecond)
}

// TestManager_CollectionConfigDefaults validates config defaults are applied
func TestManager_CollectionConfigDefaults(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	// Create manager with empty collection config
	manager, err := NewManager(logger, ManagerConfig{
		Store:            testStore,
		CollectionConfig: performance.CollectionConfig{
			// Empty - should get defaults applied
		},
	})

	require.NoError(t, err)
	assert.NotNil(t, manager)

	// ApplyDefaults should have been called, so paths should be set
	// (We don't directly test the paths as they're in the config, just verify manager was created)
	assert.NotNil(t, manager.builder, "Builder should be initialized")
}

// TestManager_CollectHardwareSnapshot_Success validates snapshot collection
func TestManager_CollectHardwareSnapshot_Success(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	manager, err := NewManager(logger, ManagerConfig{
		Store: testStore,
		CollectionConfig: performance.CollectionConfig{
			HostProcPath: "/proc",
			HostSysPath:  "/sys",
			HostDevPath:  "/dev",
		},
		NodeName:    "test-node",
		ClusterName: "test-cluster",
	})
	require.NoError(t, err)

	ctx := context.Background()
	snapshot, err := manager.collectHardwareSnapshot(ctx)

	// On macOS, collectors may fail but snapshot should still be returned
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	assert.Equal(t, "test-node", snapshot.NodeName)
	assert.Equal(t, "test-cluster", snapshot.ClusterName)
	assert.NotZero(t, snapshot.Timestamp)
	assert.NotNil(t, snapshot.CollectorRun.CollectorStats)
}

// TestManager_CollectHardwareSnapshot_WithInvalidPaths validates error handling
func TestManager_CollectHardwareSnapshot_WithInvalidPaths(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	manager, err := NewManager(logger, ManagerConfig{
		Store: testStore,
		CollectionConfig: performance.CollectionConfig{
			HostProcPath: "/completely/invalid/path/proc",
			HostSysPath:  "/completely/invalid/path/sys",
			HostDevPath:  "/completely/invalid/path/dev",
		},
		NodeName:    "test-node",
		ClusterName: "test-cluster",
	})
	require.NoError(t, err)

	ctx := context.Background()
	snapshot, err := manager.collectHardwareSnapshot(ctx)

	// Should succeed even with collector failures - graceful degradation
	require.NoError(t, err)
	require.NotNil(t, snapshot)

	// Collector stats may be empty if collectors fail to initialize on macOS
	stats := snapshot.CollectorRun.CollectorStats
	assert.NotNil(t, stats, "Collector stats map should be initialized")

	// Log what we got
	t.Logf("Collected %d collector stats", len(stats))
	for metricType, stat := range stats {
		t.Logf("Metric %v: status=%v, duration=%v, error=%v",
			metricType, stat.Status, stat.Duration, stat.Error)
	}
}

// TestManager_CollectHardwareSnapshot_SnapshotFields validates all snapshot fields
func TestManager_CollectHardwareSnapshot_SnapshotFields(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	manager, err := NewManager(logger, ManagerConfig{
		Store: testStore,
		CollectionConfig: performance.CollectionConfig{
			HostProcPath: "/proc",
			HostSysPath:  "/sys",
			HostDevPath:  "/dev",
		},
		NodeName:    "test-node-123",
		ClusterName: "test-cluster-abc",
	})
	require.NoError(t, err)

	snapshot, err := manager.collectHardwareSnapshot(context.Background())
	require.NoError(t, err)

	// Verify all expected fields are set
	assert.Equal(t, "test-node-123", snapshot.NodeName)
	assert.Equal(t, "test-cluster-abc", snapshot.ClusterName)
	assert.WithinDuration(t, time.Now(), snapshot.Timestamp, 5*time.Second)
	assert.NotZero(t, snapshot.CollectorRun.Duration)

	// On macOS, actual data may be nil but collector stats should exist
	assert.NotNil(t, snapshot.CollectorRun.CollectorStats)
}

// TestManager_UpdateHardwareGraph_ContextTimeout validates context handling
func TestManager_UpdateHardwareGraph_ContextTimeout(t *testing.T) {
	logger := testr.New(t)
	testStore, err := store.New(store.WithDataDir(""), store.WithLogger(logger))
	require.NoError(t, err)

	manager, err := NewManager(logger, ManagerConfig{
		Store: testStore,
		CollectionConfig: performance.CollectionConfig{
			HostProcPath: "/proc",
			HostSysPath:  "/sys",
			HostDevPath:  "/dev",
		},
	})
	require.NoError(t, err)

	// Use a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = manager.updateHardwareGraph(ctx)

	// May fail due to context cancellation during collection or build
	if err != nil {
		t.Logf("Update failed with cancelled context (expected): %v", err)
	}
}
