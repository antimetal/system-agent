// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCgroupCPUCollector_Constructor(t *testing.T) {
	tests := []struct {
		name    string
		config  performance.CollectionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid absolute path",
			config: performance.CollectionConfig{
				HostSysPath: "/sys",
			},
			wantErr: false,
		},
		{
			name: "invalid relative path",
			config: performance.CollectionConfig{
				HostSysPath: "sys",
			},
			wantErr: true,
			errMsg:  "HostSysPath must be an absolute path",
		},
		{
			name: "empty path",
			config: performance.CollectionConfig{
				HostSysPath: "",
			},
			wantErr: true,
			errMsg:  "HostSysPath is required but not provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := collectors.NewCgroupCPUCollector(logr.Discard(), tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
			}
		})
	}
}

func TestCgroupCPUCollector_CgroupV1(t *testing.T) {
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "fs", "cgroup")
	require.NoError(t, os.MkdirAll(cgroupPath, 0755))

	// Create cgroup v1 structure
	setupCgroupV1CPU(t, cgroupPath)

	config := performance.CollectionConfig{
		HostSysPath: tmpDir,
	}

	collector, err := collectors.NewCgroupCPUCollector(logr.Discard(), config)
	require.NoError(t, err)

	receiver := performance.NewMockReceiver("test-receiver")
	err = collector.Collect(context.Background(), receiver)
	require.NoError(t, err)

	calls := receiver.GetAcceptCalls()
	require.Len(t, calls, 1, "Expected exactly one Accept call")

	stats, ok := calls[0].Data.([]performance.CgroupCPUStats)
	require.True(t, ok, "result should be []performance.CgroupCPUStats")
	require.Len(t, stats, 2, "should find 2 containers")

	// Check first container
	dockerContainer := findContainerByID(stats, "abc123def456")
	require.NotNil(t, dockerContainer)
	assert.Equal(t, uint64(1000), dockerContainer.NrPeriods)
	assert.Equal(t, uint64(50), dockerContainer.NrThrottled)
	assert.Equal(t, uint64(5000000000), dockerContainer.ThrottledTime)
	assert.Equal(t, uint64(123456789000), dockerContainer.UsageNanos)
	assert.Equal(t, uint64(1024), dockerContainer.CpuShares)
	assert.Equal(t, int64(50000), dockerContainer.CpuQuotaUs)
	assert.Equal(t, uint64(100000), dockerContainer.CpuPeriodUs)
	assert.Equal(t, 5.0, dockerContainer.ThrottlePercent)

	// Check systemd container
	systemdContainer := findContainerByID(stats, "789abc123def")
	require.NotNil(t, systemdContainer)
	assert.Equal(t, uint64(2000), systemdContainer.NrPeriods)
}

func TestCgroupCPUCollector_CgroupV2(t *testing.T) {
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "fs", "cgroup")
	require.NoError(t, os.MkdirAll(cgroupPath, 0755))

	// Create cgroup v2 structure
	setupCgroupV2(t, cgroupPath)

	config := performance.CollectionConfig{
		HostSysPath: tmpDir,
	}

	collector, err := collectors.NewCgroupCPUCollector(logr.Discard(), config)
	require.NoError(t, err)

	receiver := performance.NewMockReceiver("test-receiver")
	err = collector.Collect(context.Background(), receiver)
	require.NoError(t, err)

	calls := receiver.GetAcceptCalls()
	require.Len(t, calls, 1, "Expected exactly one Accept call")

	stats, ok := calls[0].Data.([]performance.CgroupCPUStats)
	require.True(t, ok, "result should be []performance.CgroupCPUStats")
	require.Len(t, stats, 2, "should find 2 containers")

	// Check docker container
	dockerContainer := findContainerByID(stats, "abc123def456")
	require.NotNil(t, dockerContainer)
	assert.Equal(t, uint64(150000000000), dockerContainer.UsageNanos) // 150000000 * 1000
	assert.Equal(t, uint64(1500), dockerContainer.NrPeriods)
	assert.Equal(t, uint64(75), dockerContainer.NrThrottled)
	assert.Equal(t, uint64(7500000000), dockerContainer.ThrottledTime) // 7500000 * 1000
	assert.Equal(t, int64(50000), dockerContainer.CpuQuotaUs)
	assert.Equal(t, uint64(100000), dockerContainer.CpuPeriodUs)
	assert.Equal(t, uint64(1024), dockerContainer.CpuShares) // (100 * 1024) / 100
	assert.Equal(t, 5.0, dockerContainer.ThrottlePercent)

	// Check kubepods container
	kubeContainer := findContainerByID(stats, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NotNil(t, kubeContainer)
	assert.Equal(t, int64(-1), kubeContainer.CpuQuotaUs) // "max" means unlimited
}

func TestCgroupCPUCollector_MissingCgroup(t *testing.T) {
	tmpDir := t.TempDir()
	// Don't create the fs/cgroup directory to simulate missing cgroup

	config := performance.CollectionConfig{
		HostSysPath: tmpDir,
	}

	collector, err := collectors.NewCgroupCPUCollector(logr.Discard(), config)
	require.NoError(t, err)

	receiver := performance.NewMockReceiver("test-receiver")
	err = collector.Collect(context.Background(), receiver)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to detect cgroup version")
}

func TestCgroupCPUCollector_PartialData(t *testing.T) {
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "fs", "cgroup")
	require.NoError(t, os.MkdirAll(cgroupPath, 0755))

	// Create cgroup v1 structure with partial data
	cpuPath := filepath.Join(cgroupPath, "cpu", "docker", "abc123def456789")
	require.NoError(t, os.MkdirAll(cpuPath, 0755))

	// Only create cpu.stat, missing other files
	createFile(t, filepath.Join(cpuPath, "cpu.stat"), `nr_periods 100
nr_throttled 10
throttled_time 1000000000`)

	// Create cpu controller marker
	require.NoError(t, os.MkdirAll(filepath.Join(cgroupPath, "cpu"), 0755))

	config := performance.CollectionConfig{
		HostSysPath: tmpDir,
	}

	collector, err := collectors.NewCgroupCPUCollector(logr.Discard(), config)
	require.NoError(t, err)

	receiver := performance.NewMockReceiver("test-receiver")
	err = collector.Collect(context.Background(), receiver)
	require.NoError(t, err)

	calls := receiver.GetAcceptCalls()
	require.Len(t, calls, 1, "Expected exactly one Accept call")

	stats, ok := calls[0].Data.([]performance.CgroupCPUStats)
	require.True(t, ok)
	require.Len(t, stats, 1)

	// Should have throttling data but no usage or limits
	assert.Equal(t, uint64(100), stats[0].NrPeriods)
	assert.Equal(t, uint64(10), stats[0].NrThrottled)
	assert.Equal(t, uint64(0), stats[0].UsageNanos) // Missing file
	assert.Equal(t, uint64(0), stats[0].CpuShares)  // Missing file
}

// Helper functions

func setupCgroupV1CPU(t *testing.T, basePath string) {
	// Create docker container
	dockerPath := filepath.Join(basePath, "cpu", "docker", "abc123def456")
	require.NoError(t, os.MkdirAll(dockerPath, 0755))

	createFile(t, filepath.Join(dockerPath, "cpu.stat"), `nr_periods 1000
nr_throttled 50
throttled_time 5000000000`)
	createFile(t, filepath.Join(dockerPath, "cpu.shares"), "1024")
	createFile(t, filepath.Join(dockerPath, "cpu.cfs_quota_us"), "50000")
	createFile(t, filepath.Join(dockerPath, "cpu.cfs_period_us"), "100000")

	// Create corresponding cpuacct path
	cpuacctPath := filepath.Join(basePath, "cpuacct", "docker", "abc123def456")
	require.NoError(t, os.MkdirAll(cpuacctPath, 0755))
	createFile(t, filepath.Join(cpuacctPath, "cpuacct.usage"), "123456789000")

	// Create systemd-style container
	systemdPath := filepath.Join(basePath, "cpu", "system.slice", "docker-789abc123def.scope")
	require.NoError(t, os.MkdirAll(systemdPath, 0755))
	createFile(t, filepath.Join(systemdPath, "cpu.stat"), `nr_periods 2000
nr_throttled 100
throttled_time 10000000000`)
}

func setupCgroupV2(t *testing.T, basePath string) {
	// Create cgroup.controllers to indicate v2
	createFile(t, filepath.Join(basePath, "cgroup.controllers"), "cpu io memory")

	// Create docker container in systemd slice
	dockerPath := filepath.Join(basePath, "system.slice", "docker-abc123def456.scope")
	require.NoError(t, os.MkdirAll(dockerPath, 0755))

	// Add cgroup.procs to indicate this is a container
	createFile(t, filepath.Join(dockerPath, "cgroup.procs"), "1234\n5678\n")

	createFile(t, filepath.Join(dockerPath, "cpu.stat"), `usage_usec 150000000
user_usec 120000000
system_usec 30000000
nr_periods 1500
nr_throttled 75
throttled_usec 7500000`)
	createFile(t, filepath.Join(dockerPath, "cpu.max"), "50000 100000")
	createFile(t, filepath.Join(dockerPath, "cpu.weight"), "100")

	// Create kubepods container
	kubePath := filepath.Join(basePath, "kubepods.slice", "kubepods-pod123.slice",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, os.MkdirAll(kubePath, 0755))

	// Add cgroup.procs to indicate this is a container
	createFile(t, filepath.Join(kubePath, "cgroup.procs"), "9012\n3456\n")

	createFile(t, filepath.Join(kubePath, "cpu.stat"), `usage_usec 200000000
nr_periods 2000
nr_throttled 0
throttled_usec 0`)
	createFile(t, filepath.Join(kubePath, "cpu.max"), "max 100000")
}

func createFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	err := os.MkdirAll(dir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
}

func findContainerByID(stats []performance.CgroupCPUStats, id string) *performance.CgroupCPUStats {
	for i := range stats {
		if stats[i].ContainerID == id {
			return &stats[i]
		}
	}
	return nil
}
