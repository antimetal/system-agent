// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package integration

import (
	"context"
	"os"
	"path/filepath"
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

func TestCollectorFileSystemPaths(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Collector path tests only run on Linux")
	}

	// Test different proc/sys paths that might exist in containers
	testCases := []struct {
		name        string
		procPath    string
		sysPath     string
		shouldExist bool
	}{
		{
			name:        "Standard paths",
			procPath:    "/proc",
			sysPath:     "/sys",
			shouldExist: true,
		},
		{
			name:        "Container host paths",
			procPath:    "/host/proc",
			sysPath:     "/host/sys",
			shouldExist: false, // Usually not in test environment
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := performance.CollectionConfig{
				HostProcPath: tc.procPath,
				HostSysPath:  tc.sysPath,
				HostDevPath:  "/dev",
				Interval:     time.Second,
			}

			// Check if paths exist
			procExists := dirExists(tc.procPath)
			sysExists := dirExists(tc.sysPath)

			if tc.shouldExist {
				assert.True(t, procExists, "%s should exist", tc.procPath)
				assert.True(t, sysExists, "%s should exist", tc.sysPath)
			}

			if !procExists || !sysExists {
				t.Skipf("Paths don't exist: proc=%v, sys=%v", procExists, sysExists)
			}

			// Try to create collectors with these paths
			logger := logr.Discard()

			// Test LoadCollector
			loadCollector, err := collectors.NewLoadCollector(logger, config)
			assert.NoError(t, err, "Should create LoadCollector with valid paths")
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()

				result, err := loadCollector.Collect(ctx)
				assert.NoError(t, err, "Should collect load data")
				assert.NotNil(t, result, "Load data should not be nil")
			}
		})
	}
}

func TestCollectorDataIntegrity(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Collector data tests only run on Linux")
	}

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
		Interval:     time.Second,
	}

	t.Run("LoadCollector Data", func(t *testing.T) {
		logger := logr.Discard()
		ctx := context.Background()

		collector, err := collectors.NewLoadCollector(logger, config)
		require.NoError(t, err)

		result, err := collector.Collect(ctx)
		require.NoError(t, err)

		loadStats, ok := result.(*performance.LoadStats)
		require.True(t, ok, "Result should be *LoadStats")

		// Validate data ranges
		assert.GreaterOrEqual(t, loadStats.Load1Min, float64(0), "1-minute load should be non-negative")
		assert.GreaterOrEqual(t, loadStats.Load5Min, float64(0), "5-minute load should be non-negative")
		assert.GreaterOrEqual(t, loadStats.Load15Min, float64(0), "15-minute load should be non-negative")

		// Running processes should be positive
		assert.Greater(t, loadStats.RunningProcs, int32(0), "Should have at least one running process")
		assert.GreaterOrEqual(t, loadStats.TotalProcs, loadStats.RunningProcs,
			"Total processes should be >= running processes")
	})

	t.Run("MemoryCollector Data", func(t *testing.T) {
		logger := logr.Discard()
		ctx := context.Background()

		collector, err := collectors.NewMemoryCollector(logger, config)
		require.NoError(t, err)

		result, err := collector.Collect(ctx)
		require.NoError(t, err)

		memStats, ok := result.(*performance.MemoryStats)
		require.True(t, ok, "Result should be *MemoryStats")

		// Basic sanity checks
		assert.Greater(t, memStats.MemTotal, uint64(0), "Total memory should be positive")
		assert.Greater(t, memStats.MemFree, uint64(0), "Free memory should be positive")
		if memStats.MemAvailable > 0 {
			assert.LessOrEqual(t, memStats.MemAvailable, memStats.MemTotal, "Available memory should be <= total")
		}

		// Check swap if present
		if memStats.SwapTotal > 0 {
			assert.LessOrEqual(t, memStats.SwapFree, memStats.SwapTotal, "Free swap should be <= total swap")
		}
	})

	t.Run("CPUCollector Data", func(t *testing.T) {
		logger := logr.Discard()
		ctx := context.Background()

		collector, err := collectors.NewCPUCollector(logger, config)
		require.NoError(t, err)

		result, err := collector.Collect(ctx)
		require.NoError(t, err)

		cpuStats, ok := result.([]*performance.CPUStats)
		require.True(t, ok, "Result should be []*CPUStats")

		// Should have at least one CPU entry (the aggregate "cpu" line)
		assert.Greater(t, len(cpuStats), 0, "Should have at least one CPU entry")

		// Find the aggregate CPU stats (CPUIndex == -1)
		var totalStats *performance.CPUStats
		for _, stat := range cpuStats {
			if stat.CPUIndex == -1 {
				totalStats = stat
				break
			}
		}
		require.NotNil(t, totalStats, "Should have aggregate CPU stats")

		// Check aggregate stats
		assert.Greater(t, totalStats.User, uint64(0), "Should have some user time")
		assert.GreaterOrEqual(t, totalStats.System, uint64(0), "System time should be non-negative")
		assert.Greater(t, totalStats.Idle, uint64(0), "Should have some idle time")

		// Per-CPU validation
		for _, stat := range cpuStats {
			if stat.CPUIndex >= 0 {
				assert.GreaterOrEqual(t, stat.User, uint64(0), "CPU %d user time should be non-negative", stat.CPUIndex)
				assert.GreaterOrEqual(t, stat.System, uint64(0), "CPU %d system time should be non-negative", stat.CPUIndex)
				assert.GreaterOrEqual(t, stat.Idle, uint64(0), "CPU %d idle time should be non-negative", stat.CPUIndex)
			}
		}
	})
}

func TestCollectorContinuousMode(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Continuous collector tests only run on Linux")
	}

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
		Interval:     time.Second,
	}

	logger := logr.Discard()

	// Test continuous collection for dynamic metrics
	t.Run("Continuous Load Collection", func(t *testing.T) {
		constructor, err := performance.GetCollector(performance.MetricTypeLoad)
		require.NoError(t, err, "Load collector should be registered")

		collector, err := constructor(logger, config)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		ch, err := collector.Start(ctx)
		require.NoError(t, err)

		// Collect a few data points
		var dataPoints []*performance.LoadStats
		timeout := time.After(2 * time.Second)

	collectLoop:
		for {
			select {
			case data := <-ch:
				if loadStats, ok := data.(*performance.LoadStats); ok {
					dataPoints = append(dataPoints, loadStats)
					t.Logf("Collected load data: %.2f %.2f %.2f",
						loadStats.Load1Min, loadStats.Load5Min, loadStats.Load15Min)

					if len(dataPoints) >= 2 {
						break collectLoop
					}
				}
			case <-timeout:
				break collectLoop
			}
		}

		assert.GreaterOrEqual(t, len(dataPoints), 1, "Should collect at least one data point")

		// Stop the collector
		err = collector.Stop()
		assert.NoError(t, err)

		// Channel should be closed after stop
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "Channel should be closed after stop")
		case <-time.After(100 * time.Millisecond):
			// Channel might already be drained
		}
	})
}

func TestCollectorErrorHandling(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Collector error tests only run on Linux")
	}

	logger := logr.Discard()

	t.Run("Invalid Paths", func(t *testing.T) {
		config := performance.CollectionConfig{
			HostProcPath: "/nonexistent/proc",
			HostSysPath:  "/nonexistent/sys",
			HostDevPath:  "/nonexistent/dev",
			Interval:     time.Second,
		}

		// Some collectors should fail with invalid paths
		collector, err := collectors.NewLoadCollector(logger, config)
		require.NoError(t, err, "Constructor should succeed even with invalid paths")

		ctx := context.Background()
		_, err = collector.Collect(ctx)
		assert.Error(t, err, "Collect should fail with invalid proc path")
	})

	t.Run("Relative Paths", func(t *testing.T) {
		config := performance.CollectionConfig{
			HostProcPath: "proc", // Relative path
			HostSysPath:  "sys",  // Relative path
			HostDevPath:  "dev",  // Relative path
			Interval:     time.Second,
		}

		// Collectors should reject relative paths
		_, err := collectors.NewLoadCollector(logger, config)
		assert.Error(t, err, "Should reject relative proc path")
		assert.Contains(t, err.Error(), "absolute path", "Error should mention absolute path requirement")
	})
}

func TestCollectorKernelVersionSpecific(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Kernel version tests only run on Linux")
	}

	currentKernel, err := GetCurrentKernelVersion()
	require.NoError(t, err)

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
		Interval:     time.Second,
	}

	// Test pressure stall information (PSI) - requires kernel 4.20+
	t.Run("PSI Data", func(t *testing.T) {
		if currentKernel.Compare(KernelVersion{4, 20, 0}) < 0 {
			t.Skipf("PSI requires kernel 4.20+, current is %d.%d.%d",
				currentKernel.Major, currentKernel.Minor, currentKernel.Patch)
		}

		// Check if PSI files exist
		psiFiles := []string{
			filepath.Join(config.HostProcPath, "pressure", "cpu"),
			filepath.Join(config.HostProcPath, "pressure", "memory"),
			filepath.Join(config.HostProcPath, "pressure", "io"),
		}

		for _, file := range psiFiles {
			_, err := os.Stat(file)
			assert.NoError(t, err, "PSI file %s should exist on kernel 4.20+", file)
		}
	})

	// Test cgroup v2 features - better support in newer kernels
	t.Run("Cgroup v2", func(t *testing.T) {
		cgroupPath := filepath.Join(config.HostSysPath, "fs", "cgroup")
		info, err := os.Stat(cgroupPath)
		if err != nil {
			t.Skip("Cgroup not available")
		}

		if info.IsDir() {
			// Check if it's cgroup v2 (unified hierarchy)
			unifiedPath := filepath.Join(cgroupPath, "cgroup.controllers")
			if _, err := os.Stat(unifiedPath); err == nil {
				t.Log("✓ Cgroup v2 (unified hierarchy) detected")
			} else {
				t.Log("✓ Cgroup v1 (legacy hierarchy) detected")
			}
		}
	})
}

func TestCollectorMetadata(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Collector metadata tests only run on Linux")
	}

	// Test that collectors provide correct metadata
	metricTypes := []performance.MetricType{
		performance.MetricTypeLoad,
		performance.MetricTypeMemory,
		performance.MetricTypeCPU,
		performance.MetricTypeNetwork,
		performance.MetricTypeDisk,
		performance.MetricTypeProcess,
	}

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
		Interval:     time.Second,
	}

	logger := logr.Discard()

	for _, metricType := range metricTypes {
		t.Run(string(metricType), func(t *testing.T) {
			constructor, err := performance.GetCollector(metricType)
			if err != nil {
				t.Skipf("Collector for %s not registered: %v", metricType, err)
			}

			collector, err := constructor(logger, config)
			if err != nil {
				t.Skipf("Failed to create %s collector: %v", metricType, err)
			}

			// Check basic metadata
			assert.NotEmpty(t, collector.Name(), "Collector should have a name")
			assert.Contains(t, strings.ToLower(collector.Name()), strings.ToLower(string(metricType)),
				"Collector name should relate to metric type")

			// Stop if it was started
			_ = collector.Stop()
		})
	}
}

// Helper function to check if directory exists
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
