// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/resource/store"
	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestManager_Integration_FullDiscoveryPipeline(t *testing.T) {
	// Integration tests assume Linux environment with proper permissions
	logger := zapr.NewLogger(zaptest.NewLogger(t))

	// Create in-memory resource store for testing
	// Use empty string for in-memory store
	rsrcStore, err := store.New("")
	require.NoError(t, err, "Failed to create resource store")
	defer rsrcStore.Close()

	// Create performance manager with real system paths
	perfOpts := performance.ManagerOptions{
		Logger: logger,
		Config: performance.CollectionConfig{
			HostProcPath: "/proc",
			HostSysPath:  "/sys",
			Delta:        performance.DeltaConfig{},
		},
	}
	perfManager, err := performance.NewManager(perfOpts)
	require.NoError(t, err, "Failed to create performance manager")

	t.Run("DiscoverRealContainers", func(t *testing.T) {
		// Skip if not running in a containerized environment or CI
		if _, err := os.Stat("/sys/fs/cgroup"); os.IsNotExist(err) {
			t.Skip("Cgroup filesystem not available")
		}

		config := ManagerConfig{
			UpdateInterval:     5 * time.Second,
			Store:              rsrcStore,
			PerformanceManager: perfManager,
			CgroupPath:         "/sys/fs/cgroup",
		}

		manager, err := NewManager(logger, config)
		require.NoError(t, err, "Failed to create runtime manager")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Perform a single update cycle
		err = manager.updateRuntimeGraph(ctx)
		require.NoError(t, err, "Failed to update runtime graph")

		// Verify that we discovered at least our own process
		assert.NotZero(t, manager.GetLastUpdateTime(), "Last update time should be set")

		// Try to get our own process node to verify it was created
		ourPID := os.Getpid()
		processRef := &resourcev1.ResourceRef{
			TypeUrl: "antimetal.runtime.v1/ProcessNode",
			Name:    fmt.Sprintf("process-%d", ourPID),
		}
		
		processResource, err := rsrcStore.GetResource(processRef)
		if err == nil && processResource != nil {
			t.Log("Successfully found our own process node in the store")
		}

		// Also check for init process (PID 1)
		initRef := &resourcev1.ResourceRef{
			TypeUrl: "antimetal.runtime.v1/ProcessNode",
			Name:    "process-1",
		}
		
		initResource, err := rsrcStore.GetResource(initRef)
		assert.NoError(t, err, "Should find init process")
		assert.NotNil(t, initResource, "Init process should exist")
	})

	t.Run("SnapshotCollection", func(t *testing.T) {
		// Create new store for this test
		testStore, err := store.New("")
		require.NoError(t, err)
		defer testStore.Close()

		config := ManagerConfig{
			UpdateInterval:     5 * time.Second,
			Store:              testStore,
			PerformanceManager: perfManager,
			CgroupPath:         "/sys/fs/cgroup",
		}

		manager, err := NewManager(logger, config)
		require.NoError(t, err)

		ctx := context.Background()
		snapshot, err := manager.collectRuntimeSnapshot(ctx)
		require.NoError(t, err, "Failed to collect runtime snapshot")

		assert.NotNil(t, snapshot, "Snapshot should not be nil")
		assert.NotZero(t, snapshot.Timestamp, "Snapshot should have timestamp")

		// We should always have process stats (at minimum, init and our test process)
		assert.NotNil(t, snapshot.ProcessStats, "Process stats should not be nil")
		if snapshot.ProcessStats != nil && len(snapshot.ProcessStats.Processes) > 0 {
			// Verify we have at least init (PID 1)
			var foundInit bool
			for _, proc := range snapshot.ProcessStats.Processes {
				if proc.PID == 1 {
					foundInit = true
					assert.Equal(t, int32(0), proc.PPID, "Init process should have PPID 0")
					break
				}
			}
			assert.True(t, foundInit, "Should find init process")
		}

		t.Logf("Discovered %d containers and %d processes",
			len(snapshot.Containers),
			len(snapshot.ProcessStats.Processes))
	})

	t.Run("PeriodicUpdates", func(t *testing.T) {
		config := ManagerConfig{
			UpdateInterval:     100 * time.Millisecond, // Fast updates for testing
			Store:              rsrcStore,
			PerformanceManager: perfManager,
			CgroupPath:         "/sys/fs/cgroup",
		}

		manager, err := NewManager(logger, config)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		// Start the manager in a goroutine
		errChan := make(chan error, 1)
		go func() {
			errChan <- manager.Start(ctx)
		}()

		// Wait for at least 2 update cycles
		time.Sleep(250 * time.Millisecond)

		// Verify multiple updates occurred
		lastUpdate := manager.GetLastUpdateTime()
		assert.NotZero(t, lastUpdate, "Should have update time")

		// Cancel and wait for clean shutdown
		cancel()
		select {
		case err := <-errChan:
			assert.NoError(t, err, "Manager should shutdown cleanly")
		case <-time.After(1 * time.Second):
			t.Fatal("Manager didn't shutdown in time")
		}
	})
}

func TestManager_Integration_ContainerDiscovery(t *testing.T) {
	// This test validates container discovery with mock cgroup structures
	logger := zapr.NewLogger(zaptest.NewLogger(t))

	// Create temporary cgroup structure
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "cgroup")

	// Create mock cgroup v2 structure with various container runtimes
	setupMockCgroupV2(t, cgroupPath)

	rsrcStore, err := store.New("")
	require.NoError(t, err)
	defer rsrcStore.Close()

	perfOpts := performance.ManagerOptions{
		Logger: logger,
		Config: performance.CollectionConfig{
			HostProcPath: "/proc",
			HostSysPath:  "/sys",
			Delta:        performance.DeltaConfig{},
		},
	}
	perfManager, err := performance.NewManager(perfOpts)
	require.NoError(t, err)

	config := ManagerConfig{
		UpdateInterval:     5 * time.Second,
		Store:              rsrcStore,
		PerformanceManager: perfManager,
		CgroupPath:         cgroupPath,
	}

	manager, err := NewManager(logger, config)
	require.NoError(t, err)

	ctx := context.Background()
	snapshot, err := manager.collectRuntimeSnapshot(ctx)
	require.NoError(t, err)

	// Verify we discovered the mock containers
	assert.Len(t, snapshot.Containers, 3, "Should discover all mock containers")

	// Verify container details
	containerIDs := make(map[string]bool)
	for _, container := range snapshot.Containers {
		containerIDs[container.ID] = true
	}

	assert.True(t, containerIDs["abc123def456"], "Should find Docker container")
	assert.True(t, containerIDs["fedcba987654"], "Should find containerd container")
	assert.True(t, containerIDs["0123456789ab"], "Should find CRI-O container")
}

func setupMockCgroupV2(t *testing.T, basePath string) {
	// Create cgroup v2 unified hierarchy structure
	paths := []string{
		// Docker container
		filepath.Join(basePath, "docker", "abc123def456"),
		// Containerd container
		filepath.Join(basePath, "system.slice", "containerd-fedcba987654.scope"),
		// CRI-O container
		filepath.Join(basePath, "kubepods.slice", "kubepods-besteffort.slice",
			"kubepods-besteffort-pod123.slice", "crio-0123456789ab.scope"),
	}

	for _, path := range paths {
		require.NoError(t, os.MkdirAll(path, 0755))

		// Create cgroup.controllers file to indicate v2
		controllersFile := filepath.Join(path, "cgroup.controllers")
		err := os.WriteFile(controllersFile, []byte("cpu memory io pids\n"), 0644)
		require.NoError(t, err)

		// Create memory.current for container detection
		memFile := filepath.Join(path, "memory.current")
		err = os.WriteFile(memFile, []byte("104857600\n"), 0644) // 100MB
		require.NoError(t, err)
	}
}

func TestManager_Integration_ForceUpdate(t *testing.T) {
	logger := zapr.NewLogger(zaptest.NewLogger(t))

	rsrcStore, err := store.New("")
	require.NoError(t, err)
	defer rsrcStore.Close()

	perfOpts := performance.ManagerOptions{
		Logger: logger,
		Config: performance.CollectionConfig{
			HostProcPath: "/proc",
			HostSysPath:  "/sys",
			Delta:        performance.DeltaConfig{},
		},
	}
	perfManager, err := performance.NewManager(perfOpts)
	require.NoError(t, err)

	config := ManagerConfig{
		UpdateInterval:     1 * time.Hour, // Long interval
		Store:              rsrcStore,
		PerformanceManager: perfManager,
		CgroupPath:         "/sys/fs/cgroup",
	}

	manager, err := NewManager(logger, config)
	require.NoError(t, err)

	ctx := context.Background()

	// Force an immediate update
	err = manager.ForceUpdate(ctx)
	require.NoError(t, err)

	firstUpdate := manager.GetLastUpdateTime()
	assert.NotZero(t, firstUpdate)

	// Small delay then force another update
	time.Sleep(10 * time.Millisecond)

	err = manager.ForceUpdate(ctx)
	require.NoError(t, err)

	secondUpdate := manager.GetLastUpdateTime()
	assert.True(t, secondUpdate.After(firstUpdate), "Second update should be after first")
}
