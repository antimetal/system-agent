//go:build integration && linux

// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/resource"
	"github.com/antimetal/agent/pkg/resource/store"
	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Helper functions for integration tests
func createIntegrationTestStore(t *testing.T) resource.Store {
	store, err := store.New("")
	require.NoError(t, err)
	return store
}

func createIntegrationMockPerfManager() *performance.Manager {
	// Create a minimal performance manager for integration tests
	// This would normally connect to real system resources
	return &performance.Manager{}
}

// TestManager_EBPFIntegration tests the full eBPF-based manager with real kernel integration
func TestManager_EBPFIntegration(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	rsrcStore := createIntegrationTestStore(t)
	
	config := ManagerConfig{
		Store:              rsrcStore,
		PerformanceManager: createIntegrationMockPerfManager(),
		UpdateInterval:     5 * time.Minute,
		CgroupPath:         "/sys/fs/cgroup",
		EventBufferSize:    1000,
		DebounceInterval:   100 * time.Millisecond,
	}

	manager, err := NewManager(logger, config)
	require.NoError(t, err)
	assert.NotNil(t, manager)

	// Test eBPF tracker initialization
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start manager (should initialize eBPF tracker)
	done := make(chan error, 1)
	go func() {
		done <- manager.Start(ctx)
	}()

	// Let it run briefly to test eBPF initialization
	time.Sleep(2 * time.Second)
	cancel()

	// Wait for completion
	select {
	case err := <-done:
		// eBPF tracker should handle context cancellation gracefully
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Manager.Start did not complete in time")
	}

	// Test that we can get metrics (even if empty)
	metrics := manager.GetMetrics()
	assert.NotNil(t, metrics)
}

// TestManager_EBPFSnapshot tests the snapshot functionality with real cgroups
func TestManager_EBPFSnapshot(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	rsrcStore := createIntegrationTestStore(t)
	
	config := ManagerConfig{
		Store:              rsrcStore,
		PerformanceManager: createIntegrationMockPerfManager(),
		CgroupPath:         "/sys/fs/cgroup", // Real cgroup path
	}

	manager, err := NewManager(logger, config)
	require.NoError(t, err)

	// Test snapshot functionality
	ctx := context.Background()
	err = manager.ForceUpdate(ctx)
	
	// Should work even if no containers are found
	require.NoError(t, err)
	
	lastUpdate := manager.GetLastUpdateTime()
	assert.False(t, lastUpdate.IsZero(), "Last update time should be set after ForceUpdate")
}

// TestManager_EBPFContainerDiscovery tests container discovery with mock cgroup structures  
func TestManager_EBPFContainerDiscovery(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	rsrcStore := createIntegrationTestStore(t)
	
	// Create a mock cgroup directory structure
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "cgroup")
	
	// Create mock cgroup structure for a container
	// Use a valid hex container ID (at least 12 chars)
	containerPath := filepath.Join(cgroupPath, "docker", "1234567890abcdef")
	require.NoError(t, os.MkdirAll(containerPath, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(containerPath, "cgroup.controllers"), []byte("cpu memory"), 0644))
	// Add cgroup.procs to make it a valid container
	require.NoError(t, os.WriteFile(filepath.Join(containerPath, "cgroup.procs"), []byte("1234\n5678\n"), 0644))

	config := ManagerConfig{
		Store:              rsrcStore,
		PerformanceManager: createIntegrationMockPerfManager(),
		CgroupPath:         cgroupPath, // Use our mock cgroup path
		EventBufferSize:    1000,
		DebounceInterval:   50 * time.Millisecond,
	}

	manager, err := NewManager(logger, config)
	require.NoError(t, err)

	// Test snapshot with mock container structure
	ctx := context.Background()
	err = manager.ForceUpdate(ctx)
	require.NoError(t, err)

	// Check that discovery worked
	lastUpdate := manager.GetLastUpdateTime()
	assert.False(t, lastUpdate.IsZero(), "Should have updated after discovering containers")
	
	metrics := manager.GetMetrics()
	// Integration test should be able to discover containers or processes
	t.Logf("Discovered containers: %d, processes: %d", metrics.LastContainerCount, metrics.LastProcessCount)
}