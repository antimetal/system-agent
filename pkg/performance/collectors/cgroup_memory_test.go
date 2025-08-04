// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCgroupMemoryCollector_Constructor(t *testing.T) {
	tests := []struct {
		name    string
		config  performance.CollectionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid absolute path",
			config: performance.CollectionConfig{
				HostCgroupPath: "/sys/fs/cgroup",
			},
			wantErr: false,
		},
		{
			name: "invalid relative path",
			config: performance.CollectionConfig{
				HostCgroupPath: "sys/fs/cgroup",
			},
			wantErr: true,
			errMsg:  "HostCgroupPath must be an absolute path",
		},
		{
			name: "empty path",
			config: performance.CollectionConfig{
				HostCgroupPath: "",
			},
			wantErr: true,
			errMsg:  "HostCgroupPath is required but not provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := collectors.NewCgroupMemoryCollector(logr.Discard(), tt.config)
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

func TestCgroupMemoryCollector_CgroupV1(t *testing.T) {
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "cgroup")

	// Create cgroup v1 structure
	setupCgroupV1Memory(t, cgroupPath)

	config := performance.CollectionConfig{
		HostCgroupPath: cgroupPath,
	}

	collector, err := collectors.NewCgroupMemoryCollector(logr.Discard(), config)
	require.NoError(t, err)

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats, ok := result.([]performance.CgroupMemoryStats)
	require.True(t, ok, "result should be []performance.CgroupMemoryStats")
	require.Len(t, stats, 2, "should find 2 containers")

	// Check first container
	dockerContainer := findMemoryContainerByID(stats, "abc123def456")
	require.NotNil(t, dockerContainer)
	assert.Equal(t, uint64(1073741824), dockerContainer.RSS)           // From memory.stat
	assert.Equal(t, uint64(536870912), dockerContainer.Cache)          // From memory.stat
	assert.Equal(t, uint64(134217728), dockerContainer.MappedFile)     // From memory.stat
	assert.Equal(t, uint64(0), dockerContainer.Swap)                   // From memory.stat
	assert.Equal(t, uint64(1610612736), dockerContainer.UsageBytes)    // From memory.usage_in_bytes
	assert.Equal(t, uint64(2147483648), dockerContainer.LimitBytes)    // From memory.limit_in_bytes
	assert.Equal(t, uint64(1879048192), dockerContainer.MaxUsageBytes) // From memory.max_usage_in_bytes
	assert.Equal(t, uint64(5), dockerContainer.FailCount)              // From memory.failcnt
	assert.Equal(t, uint64(2), dockerContainer.OOMKillCount)           // From memory.oom_control
	assert.False(t, dockerContainer.UnderOOM)                          // From memory.oom_control
	assert.InDelta(t, 75.0, dockerContainer.UsagePercent, 0.1)         // Calculated
	assert.InDelta(t, 33.3, dockerContainer.CachePercent, 0.1)         // Calculated
}

func TestCgroupMemoryCollector_CgroupV2(t *testing.T) {
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "cgroup")

	// Create cgroup v2 structure
	setupCgroupV2Memory(t, cgroupPath)

	config := performance.CollectionConfig{
		HostCgroupPath: cgroupPath,
	}

	collector, err := collectors.NewCgroupMemoryCollector(logr.Discard(), config)
	require.NoError(t, err)

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats, ok := result.([]performance.CgroupMemoryStats)
	require.True(t, ok, "result should be []performance.CgroupMemoryStats")
	require.Len(t, stats, 2, "should find 2 containers")

	// Check docker container
	dockerContainer := findMemoryContainerByID(stats, "abc123def456")
	require.NotNil(t, dockerContainer)
	assert.Equal(t, uint64(1073741824), dockerContainer.RSS)        // anon from memory.stat
	assert.Equal(t, uint64(536870912), dockerContainer.Cache)       // file from memory.stat
	assert.Equal(t, uint64(134217728), dockerContainer.MappedFile)  // file_mapped from memory.stat
	assert.Equal(t, uint64(0), dockerContainer.Swap)                // From memory.stat
	assert.Equal(t, uint64(1610612736), dockerContainer.UsageBytes) // From memory.current
	assert.Equal(t, uint64(2147483648), dockerContainer.LimitBytes) // From memory.max
	assert.Equal(t, uint64(10), dockerContainer.FailCount)          // max events from memory.events
	assert.Equal(t, uint64(3), dockerContainer.OOMKillCount)        // From memory.events
	assert.InDelta(t, 75.0, dockerContainer.UsagePercent, 0.1)      // Calculated

	// Check kubepods container with unlimited memory
	kubeContainer := findMemoryContainerByID(stats, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NotNil(t, kubeContainer)
	assert.Equal(t, uint64(math.MaxUint64), kubeContainer.LimitBytes) // "max" means unlimited
	assert.Equal(t, float64(0), kubeContainer.UsagePercent)           // No percentage for unlimited
}

func TestCgroupMemoryCollector_MissingCgroup(t *testing.T) {
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "nonexistent")

	config := performance.CollectionConfig{
		HostCgroupPath: cgroupPath,
	}

	collector, err := collectors.NewCgroupMemoryCollector(logr.Discard(), config)
	require.NoError(t, err)

	_, err = collector.Collect(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to detect cgroup version")
}

func TestCgroupMemoryCollector_PartialData(t *testing.T) {
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "cgroup")

	// Create cgroup v1 structure with partial data
	memPath := filepath.Join(cgroupPath, "memory", "docker", "abc123def456789")
	require.NoError(t, os.MkdirAll(memPath, 0755))

	// Only create memory.stat, missing other files
	createMemoryFile(t, filepath.Join(memPath, "memory.stat"), `rss 1073741824
cache 536870912
mapped_file 134217728
swap 0`)

	// Create memory controller marker
	require.NoError(t, os.MkdirAll(filepath.Join(cgroupPath, "memory"), 0755))

	config := performance.CollectionConfig{
		HostCgroupPath: cgroupPath,
	}

	collector, err := collectors.NewCgroupMemoryCollector(logr.Discard(), config)
	require.NoError(t, err)

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats, ok := result.([]performance.CgroupMemoryStats)
	require.True(t, ok)
	require.Len(t, stats, 1)

	// Should have memory.stat data but missing usage/limit info
	assert.Equal(t, uint64(1073741824), stats[0].RSS)
	assert.Equal(t, uint64(536870912), stats[0].Cache)
	assert.Equal(t, uint64(0), stats[0].UsageBytes) // Missing file
	assert.Equal(t, uint64(0), stats[0].LimitBytes) // Missing file
}

func TestCgroupMemoryCollector_OOMCondition(t *testing.T) {
	tmpDir := t.TempDir()
	cgroupPath := filepath.Join(tmpDir, "cgroup")

	// Create container under OOM
	memPath := filepath.Join(cgroupPath, "memory", "docker", "fedcba987654321")
	require.NoError(t, os.MkdirAll(memPath, 0755))

	createMemoryFile(t, filepath.Join(memPath, "memory.stat"), `rss 2147483648
cache 0`)
	createMemoryFile(t, filepath.Join(memPath, "memory.usage_in_bytes"), "2147483648")
	createMemoryFile(t, filepath.Join(memPath, "memory.limit_in_bytes"), "2147483648")
	createMemoryFile(t, filepath.Join(memPath, "memory.oom_control"), `oom_kill_disable 0
under_oom 1
oom_kill 10`)

	// Create memory controller marker
	require.NoError(t, os.MkdirAll(filepath.Join(cgroupPath, "memory"), 0755))

	config := performance.CollectionConfig{
		HostCgroupPath: cgroupPath,
	}

	collector, err := collectors.NewCgroupMemoryCollector(logr.Discard(), config)
	require.NoError(t, err)

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats, ok := result.([]performance.CgroupMemoryStats)
	require.True(t, ok)
	require.Len(t, stats, 1)

	// Check OOM condition
	assert.True(t, stats[0].UnderOOM)
	assert.Equal(t, uint64(10), stats[0].OOMKillCount)
	assert.Equal(t, float64(100), stats[0].UsagePercent) // At limit
}

// Helper functions

func setupCgroupV1Memory(t *testing.T, basePath string) {
	// Create docker container
	dockerPath := filepath.Join(basePath, "memory", "docker", "abc123def456")
	require.NoError(t, os.MkdirAll(dockerPath, 0755))

	createMemoryFile(t, filepath.Join(dockerPath, "memory.stat"), `rss 1073741824
cache 536870912
mapped_file 134217728
swap 0
active_anon 805306368
inactive_anon 268435456
active_file 402653184
inactive_file 134217728`)
	createMemoryFile(t, filepath.Join(dockerPath, "memory.usage_in_bytes"), "1610612736")
	createMemoryFile(t, filepath.Join(dockerPath, "memory.limit_in_bytes"), "2147483648")
	createMemoryFile(t, filepath.Join(dockerPath, "memory.max_usage_in_bytes"), "1879048192")
	createMemoryFile(t, filepath.Join(dockerPath, "memory.failcnt"), "5")
	createMemoryFile(t, filepath.Join(dockerPath, "memory.oom_control"), `oom_kill_disable 0
under_oom 0
oom_kill 2`)

	// Create systemd-style container
	systemdPath := filepath.Join(basePath, "memory", "system.slice", "docker-789abc123def.scope")
	require.NoError(t, os.MkdirAll(systemdPath, 0755))
	createMemoryFile(t, filepath.Join(systemdPath, "memory.stat"), `rss 536870912
cache 268435456`)
	createMemoryFile(t, filepath.Join(systemdPath, "memory.usage_in_bytes"), "805306368")
	createMemoryFile(t, filepath.Join(systemdPath, "memory.limit_in_bytes"), "1073741824")
}

func setupCgroupV2Memory(t *testing.T, basePath string) {
	// Create cgroup.controllers to indicate v2
	createMemoryFile(t, filepath.Join(basePath, "cgroup.controllers"), "cpu io memory")

	// Create docker container in systemd slice
	dockerPath := filepath.Join(basePath, "system.slice", "docker-abc123def456.scope")
	require.NoError(t, os.MkdirAll(dockerPath, 0755))

	createMemoryFile(t, filepath.Join(dockerPath, "memory.stat"), `anon 1073741824
file 536870912
file_mapped 134217728
swap 0
active_anon 805306368
inactive_anon 268435456
active_file 402653184
inactive_file 134217728`)
	createMemoryFile(t, filepath.Join(dockerPath, "memory.current"), "1610612736")
	createMemoryFile(t, filepath.Join(dockerPath, "memory.max"), "2147483648")
	createMemoryFile(t, filepath.Join(dockerPath, "memory.events"), `low 0
high 0
max 10
oom 0
oom_kill 3`)

	// Create kubepods container with unlimited memory
	kubePath := filepath.Join(basePath, "kubepods.slice", "kubepods-pod123.slice",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, os.MkdirAll(kubePath, 0755))

	createMemoryFile(t, filepath.Join(kubePath, "memory.stat"), `anon 2147483648
file 1073741824`)
	createMemoryFile(t, filepath.Join(kubePath, "memory.current"), "3221225472")
	createMemoryFile(t, filepath.Join(kubePath, "memory.max"), "max")
	createMemoryFile(t, filepath.Join(kubePath, "memory.events"), `max 0
oom_kill 0`)
}

func createMemoryFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	err := os.MkdirAll(dir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
}

func findMemoryContainerByID(stats []performance.CgroupMemoryStats, id string) *performance.CgroupMemoryStats {
	for i := range stats {
		if stats[i].ContainerID == id {
			return &stats[i]
		}
	}
	return nil
}
