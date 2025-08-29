// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/antimetal/agent/internal/runtime/graph"
	"github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/resource"
	"github.com/antimetal/agent/pkg/resource/store"
	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Helper functions for creating test objects
func createTestStore(t *testing.T) resource.Store {
	store, err := store.New("")
	require.NoError(t, err)
	return store
}

func createMockPerfManager() *performance.Manager {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)
	perfManager, _ := performance.NewManager(performance.ManagerOptions{
		Config: performance.CollectionConfig{},
		Logger: logger,
	})
	return perfManager
}

func TestNewManager(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	tests := []struct {
		name    string
		config  ManagerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: ManagerConfig{
				Store:              createTestStore(t),
				PerformanceManager: createMockPerfManager(),
				UpdateInterval:     30 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "missing store",
			config: ManagerConfig{
				PerformanceManager: createMockPerfManager(),
			},
			wantErr: true,
			errMsg:  "resource store is required",
		},
		{
			name: "missing performance manager",
			config: ManagerConfig{
				Store: createTestStore(t),
			},
			wantErr: true,
			errMsg:  "performance manager is required",
		},
		{
			name: "default interval when zero",
			config: ManagerConfig{
				Store:              createTestStore(t),
				PerformanceManager: createMockPerfManager(),
				UpdateInterval:     0,
			},
			wantErr: false,
		},
		{
			name: "custom cgroup path",
			config: ManagerConfig{
				Store:              createTestStore(t),
				PerformanceManager: createMockPerfManager(),
				CgroupPath:         "/custom/cgroup/path",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewManager(logger, tt.config)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, manager)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, manager)
				
				// Verify defaults are set
				if tt.config.UpdateInterval == 0 {
					assert.Equal(t, 30*time.Second, manager.interval)
				} else {
					assert.Equal(t, tt.config.UpdateInterval, manager.interval)
				}
			}
		})
	}
}

func TestManager_CollectRuntimeSnapshot(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	rsrcStore := createTestStore(t)
	
	// Create a mock cgroup directory
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "cgroup")
	
	// Create mock cgroup structure for a container
	containerPath := filepath.Join(cgroupPath, "system.slice", "docker.service", "docker", "test1")
	os.MkdirAll(containerPath, 0755)
	os.WriteFile(filepath.Join(containerPath, "cgroup.controllers"), []byte("cpu memory"), 0644)

	manager := &Manager{
		logger:      logger,
		store:       rsrcStore,
		perfManager: createMockPerfManager(),
		builder:     graph.NewBuilder(logger, rsrcStore),
		discovery:   containers.NewDiscovery(cgroupPath),
		interval:    30 * time.Second,
		metrics:     &DiscoveryMetrics{},
	}

	ctx := context.Background()
	snapshot, err := manager.collectRuntimeSnapshot(ctx)

	require.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.NotNil(t, snapshot.ProcessStats)
	// Should discover the container we created
	assert.GreaterOrEqual(t, len(snapshot.Containers), 1)
}

func TestManager_UpdateRuntimeGraph(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	rsrcStore := createTestStore(t)
	
	// Create a mock cgroup directory with a container
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "cgroup")
	containerPath := filepath.Join(cgroupPath, "docker", "abc123")
	os.MkdirAll(containerPath, 0755)
	os.WriteFile(filepath.Join(containerPath, "cgroup.controllers"), []byte("cpu memory"), 0644)

	manager := &Manager{
		logger:      logger,
		store:       rsrcStore,
		perfManager: createMockPerfManager(),
		builder:     graph.NewBuilder(logger, rsrcStore),
		discovery:   containers.NewDiscovery(cgroupPath),
		interval:    30 * time.Second,
		metrics:     &DiscoveryMetrics{},
	}

	ctx := context.Background()
	err := manager.updateRuntimeGraph(ctx)

	require.NoError(t, err)
	
	// Check metrics were updated
	metrics := manager.GetMetrics()
	assert.GreaterOrEqual(t, metrics.LastContainerCount, 1)
	assert.Equal(t, uint64(1), metrics.TotalDiscoveries)
	assert.Equal(t, 0, metrics.LastDiscoveryErrors)
	assert.Greater(t, metrics.LastDiscoveryDuration, time.Duration(0))
}

func TestManager_Start(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	rsrcStore := createTestStore(t)
	
	// Create empty cgroup directory for testing
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "cgroup")
	os.MkdirAll(cgroupPath, 0755)

	manager := &Manager{
		logger:      logger,
		store:       rsrcStore,
		perfManager: createMockPerfManager(),
		builder:     graph.NewBuilder(logger, rsrcStore),
		discovery:   containers.NewDiscovery(cgroupPath),
		interval:    100 * time.Millisecond, // Short interval for testing
		metrics:     &DiscoveryMetrics{},
	}

	// Create a context that we'll cancel after a short time
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	// Start the manager in a goroutine
	done := make(chan error, 1)
	go func() {
		done <- manager.Start(ctx)
	}()

	// Wait for completion
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Manager.Start did not complete in time")
	}
	
	// Check metrics show multiple discoveries (initial + periodic)
	metrics := manager.GetMetrics()
	assert.GreaterOrEqual(t, metrics.TotalDiscoveries, uint64(2))
}

func TestManager_GetLastUpdateTime(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	rsrcStore := createTestStore(t)
	tmpDir := t.TempDir()

	manager := &Manager{
		logger:      logger,
		store:       rsrcStore,
		perfManager: createMockPerfManager(),
		builder:     graph.NewBuilder(logger, rsrcStore),
		discovery:   containers.NewDiscovery(filepath.Join(tmpDir, "cgroup")),
		interval:    30 * time.Second,
		metrics:     &DiscoveryMetrics{},
	}

	// Initially should be zero
	assert.True(t, manager.GetLastUpdateTime().IsZero())

	// Update the graph
	ctx := context.Background()
	beforeUpdate := time.Now()
	err := manager.updateRuntimeGraph(ctx)
	require.NoError(t, err)
	afterUpdate := time.Now()

	// Last update time should be between before and after
	lastUpdate := manager.GetLastUpdateTime()
	assert.True(t, lastUpdate.After(beforeUpdate) || lastUpdate.Equal(beforeUpdate))
	assert.True(t, lastUpdate.Before(afterUpdate) || lastUpdate.Equal(afterUpdate))
}

func TestManager_ForceUpdate(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	rsrcStore := createTestStore(t)
	tmpDir := t.TempDir()

	manager := &Manager{
		logger:      logger,
		store:       rsrcStore,
		perfManager: createMockPerfManager(),
		builder:     graph.NewBuilder(logger, rsrcStore),
		discovery:   containers.NewDiscovery(filepath.Join(tmpDir, "cgroup")),
		interval:    30 * time.Second,
		metrics:     &DiscoveryMetrics{},
	}

	ctx := context.Background()
	
	// Force an update
	err := manager.ForceUpdate(ctx)
	require.NoError(t, err)
	
	// Verify metrics show discovery was performed
	metrics := manager.GetMetrics()
	assert.Equal(t, uint64(1), metrics.TotalDiscoveries)
	
	// Force another update
	err = manager.ForceUpdate(ctx)
	require.NoError(t, err)
	
	// Verify metrics show discovery was performed again
	metrics = manager.GetMetrics()
	assert.Equal(t, uint64(2), metrics.TotalDiscoveries)
}

func TestManager_Metrics(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	rsrcStore := createTestStore(t)
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "cgroup")
	
	// Create multiple containers in cgroup structure
	for i := 0; i < 3; i++ {
		containerPath := filepath.Join(cgroupPath, "docker", fmt.Sprintf("container%d", i))
		os.MkdirAll(containerPath, 0755)
		os.WriteFile(filepath.Join(containerPath, "cgroup.controllers"), []byte("cpu memory"), 0644)
	}

	manager := &Manager{
		logger:      logger,
		store:       rsrcStore,
		perfManager: createMockPerfManager(),
		builder:     graph.NewBuilder(logger, rsrcStore),
		discovery:   containers.NewDiscovery(cgroupPath),
		interval:    30 * time.Second,
		metrics:     &DiscoveryMetrics{},
	}

	ctx := context.Background()

	// Perform multiple updates
	for i := 0; i < 3; i++ {
		err := manager.updateRuntimeGraph(ctx)
		require.NoError(t, err)
	}

	metrics := manager.GetMetrics()
	
	// Verify metrics are tracked correctly
	assert.Equal(t, uint64(3), metrics.TotalDiscoveries)
	assert.Equal(t, uint64(0), metrics.TotalDiscoveryErrors)
	assert.GreaterOrEqual(t, metrics.LastContainerCount, 3)
	assert.Equal(t, 0, metrics.LastDiscoveryErrors)
	assert.Greater(t, metrics.LastDiscoveryDuration, time.Duration(0))
	assert.False(t, metrics.LastDiscoveryTimestamp.IsZero())
}

func TestManager_MetricsConcurrency(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	rsrcStore := createTestStore(t)
	tmpDir := t.TempDir()

	manager := &Manager{
		logger:      logger,
		store:       rsrcStore,
		perfManager: createMockPerfManager(),
		builder:     graph.NewBuilder(logger, rsrcStore),
		discovery:   containers.NewDiscovery(filepath.Join(tmpDir, "cgroup")),
		interval:    30 * time.Second,
		metrics:     &DiscoveryMetrics{},
	}

	ctx := context.Background()

	// Run multiple goroutines updating and reading metrics concurrently
	done := make(chan bool, 20)
	
	// 10 goroutines updating
	for i := 0; i < 10; i++ {
		go func() {
			_ = manager.updateRuntimeGraph(ctx)
			done <- true
		}()
	}
	
	// 10 goroutines reading
	for i := 0; i < 10; i++ {
		go func() {
			_ = manager.GetMetrics()
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 20; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Concurrent operations did not complete in time")
		}
	}

	// Verify final state is consistent
	metrics := manager.GetMetrics()
	assert.GreaterOrEqual(t, metrics.TotalDiscoveries, uint64(1))
}