// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !integration

package collectors_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testMeminfo = `MemTotal:       16384000 kB
MemFree:         8192000 kB
MemAvailable:   12288000 kB
Buffers:          512000 kB
Cached:          2048000 kB
SwapCached:            0 kB
Active:          4096000 kB
Inactive:        2048000 kB
SwapTotal:       8192000 kB
SwapFree:        8192000 kB
`

const testNodeMeminfo = `Node 0 MemTotal:       8192000 kB
Node 0 MemFree:        4096000 kB
Node 0 MemUsed:        4096000 kB
`

func createTestMemoryInfoCollector(t *testing.T) (*collectors.MemoryInfoCollector, string) {
	tmpDir := t.TempDir()
	procPath := filepath.Join(tmpDir, "proc")
	sysPath := filepath.Join(tmpDir, "sys")

	require.NoError(t, os.MkdirAll(procPath, 0755))
	require.NoError(t, os.MkdirAll(sysPath, 0755))

	config := performance.CollectionConfig{
		HostProcPath: procPath,
		HostSysPath:  sysPath,
	}

	collector, err := collectors.NewMemoryInfoCollector(logr.Discard(), config)
	require.NoError(t, err)
	return collector, tmpDir
}

func TestMemoryInfoCollector_Constructor(t *testing.T) {
	tests := []struct {
		name    string
		config  performance.CollectionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid absolute paths",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
				HostSysPath:  "/sys",
			},
			wantErr: false,
		},
		{
			name: "invalid relative proc path",
			config: performance.CollectionConfig{
				HostProcPath: "proc",
				HostSysPath:  "/sys",
			},
			wantErr: true,
			errMsg:  "HostProcPath must be an absolute path",
		},
		{
			name: "invalid relative sys path",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
				HostSysPath:  "sys",
			},
			wantErr: true,
			errMsg:  "HostSysPath must be an absolute path",
		},
		{
			name: "empty paths",
			config: performance.CollectionConfig{
				HostProcPath: "",
				HostSysPath:  "",
			},
			wantErr: true,
			errMsg:  "HostProcPath is required but not provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := collectors.NewMemoryInfoCollector(logr.Discard(), tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
			}
		})
	}
}

func TestMemoryInfoCollector_Collect(t *testing.T) {
	tests := []struct {
		name       string
		meminfo    string
		setupSysfs func(t *testing.T, sysPath string)
		wantInfo   func(t *testing.T, info *performance.MemoryInfo)
		wantErr    bool
	}{
		{
			name:    "single NUMA node",
			meminfo: testMeminfo,
			setupSysfs: func(t *testing.T, sysPath string) {
				// Create single NUMA node
				node0Path := filepath.Join(sysPath, "devices", "system", "node", "node0")
				require.NoError(t, os.MkdirAll(node0Path, 0755))

				// Write node meminfo
				require.NoError(t, os.WriteFile(
					filepath.Join(node0Path, "meminfo"),
					[]byte(testNodeMeminfo),
					0644,
				))

				// Write CPU list
				require.NoError(t, os.WriteFile(
					filepath.Join(node0Path, "cpulist"),
					[]byte("0-7\n"),
					0644,
				))
			},
			wantInfo: func(t *testing.T, info *performance.MemoryInfo) {
				assert.Equal(t, uint64(16384000*1024), info.TotalBytes)
				assert.False(t, info.NUMAEnabled) // Single node = not NUMA
				assert.Len(t, info.NUMANodes, 1)
				assert.Equal(t, int32(0), info.NUMANodes[0].NodeID)
				assert.Equal(t, uint64(8192000*1024), info.NUMANodes[0].TotalBytes)
				assert.Equal(t, []int32{0, 1, 2, 3, 4, 5, 6, 7}, info.NUMANodes[0].CPUs)
			},
		},
		{
			name:    "dual NUMA nodes",
			meminfo: testMeminfo,
			setupSysfs: func(t *testing.T, sysPath string) {
				// Create two NUMA nodes
				for i := 0; i < 2; i++ {
					nodePath := filepath.Join(sysPath, "devices", "system", "node", fmt.Sprintf("node%d", i))
					require.NoError(t, os.MkdirAll(nodePath, 0755))

					// Write node meminfo
					require.NoError(t, os.WriteFile(
						filepath.Join(nodePath, "meminfo"),
						[]byte(fmt.Sprintf("Node %d MemTotal:       8192000 kB\n", i)),
						0644,
					))

					// Write CPU list (4 CPUs per node)
					cpuList := fmt.Sprintf("%d-%d\n", i*4, i*4+3)
					require.NoError(t, os.WriteFile(
						filepath.Join(nodePath, "cpulist"),
						[]byte(cpuList),
						0644,
					))
				}
			},
			wantInfo: func(t *testing.T, info *performance.MemoryInfo) {
				assert.Equal(t, uint64(16384000*1024), info.TotalBytes)
				assert.True(t, info.NUMAEnabled) // Multiple nodes = NUMA enabled
				assert.Len(t, info.NUMANodes, 2)

				// Node 0
				assert.Equal(t, int32(0), info.NUMANodes[0].NodeID)
				assert.Equal(t, uint64(8192000*1024), info.NUMANodes[0].TotalBytes)
				assert.Equal(t, []int32{0, 1, 2, 3}, info.NUMANodes[0].CPUs)

				// Node 1
				assert.Equal(t, int32(1), info.NUMANodes[1].NodeID)
				assert.Equal(t, uint64(8192000*1024), info.NUMANodes[1].TotalBytes)
				assert.Equal(t, []int32{4, 5, 6, 7}, info.NUMANodes[1].CPUs)
			},
		},
		{
			name:    "dual NUMA nodes with distances and balancing",
			meminfo: testMeminfo,
			setupSysfs: func(t *testing.T, sysPath string) {
				// Create NUMA balancing file
				kernelPath := filepath.Join(sysPath, "..", "proc", "sys", "kernel")
				require.NoError(t, os.MkdirAll(kernelPath, 0755))
				require.NoError(t, os.WriteFile(
					filepath.Join(kernelPath, "numa_balancing"),
					[]byte("1\n"),
					0644,
				))

				// Create two NUMA nodes with distance information
				for i := 0; i < 2; i++ {
					nodePath := filepath.Join(sysPath, "devices", "system", "node", fmt.Sprintf("node%d", i))
					require.NoError(t, os.MkdirAll(nodePath, 0755))

					// Write node meminfo
					require.NoError(t, os.WriteFile(
						filepath.Join(nodePath, "meminfo"),
						[]byte(fmt.Sprintf("Node %d MemTotal:       8192000 kB\n", i)),
						0644,
					))

					// Write CPU list (4 CPUs per node)
					cpuList := fmt.Sprintf("%d-%d\n", i*4, i*4+3)
					require.NoError(t, os.WriteFile(
						filepath.Join(nodePath, "cpulist"),
						[]byte(cpuList),
						0644,
					))

					// Write distance information
					// Node 0: "10 21" (10 to self, 21 to node1)
					// Node 1: "21 10" (21 to node0, 10 to self)
					var distances string
					if i == 0 {
						distances = "10 21\n"
					} else {
						distances = "21 10\n"
					}
					require.NoError(t, os.WriteFile(
						filepath.Join(nodePath, "distance"),
						[]byte(distances),
						0644,
					))
				}
			},
			wantInfo: func(t *testing.T, info *performance.MemoryInfo) {
				assert.Equal(t, uint64(16384000*1024), info.TotalBytes)
				assert.True(t, info.NUMAEnabled)
				assert.True(t, info.NUMABalancingAvailable)
				assert.Len(t, info.NUMANodes, 2)

				// Node 0
				assert.Equal(t, int32(0), info.NUMANodes[0].NodeID)
				assert.Equal(t, uint64(8192000*1024), info.NUMANodes[0].TotalBytes)
				assert.Equal(t, []int32{0, 1, 2, 3}, info.NUMANodes[0].CPUs)
				assert.Equal(t, []int32{10, 21}, info.NUMANodes[0].Distances)

				// Node 1
				assert.Equal(t, int32(1), info.NUMANodes[1].NodeID)
				assert.Equal(t, uint64(8192000*1024), info.NUMANodes[1].TotalBytes)
				assert.Equal(t, []int32{4, 5, 6, 7}, info.NUMANodes[1].CPUs)
				assert.Equal(t, []int32{21, 10}, info.NUMANodes[1].Distances)
			},
		},
		{
			name:    "no NUMA info",
			meminfo: testMeminfo,
			setupSysfs: func(t *testing.T, sysPath string) {
				// Create CPU directories but no NUMA nodes
				cpuPath := filepath.Join(sysPath, "devices", "system", "cpu")
				for i := 0; i < 4; i++ {
					require.NoError(t, os.MkdirAll(filepath.Join(cpuPath, fmt.Sprintf("cpu%d", i)), 0755))
				}
			},
			wantInfo: func(t *testing.T, info *performance.MemoryInfo) {
				assert.Equal(t, uint64(16384000*1024), info.TotalBytes)
				assert.False(t, info.NUMAEnabled) // No NUMA nodes = not NUMA
				assert.Len(t, info.NUMANodes, 1)  // Assumed
				assert.Equal(t, int32(0), info.NUMANodes[0].NodeID)
				assert.Equal(t, uint64(16384000*1024), info.NUMANodes[0].TotalBytes)
				assert.Equal(t, []int32{0, 1, 2, 3}, info.NUMANodes[0].CPUs)
				assert.Equal(t, []int32{10}, info.NUMANodes[0].Distances) // Default local distance
			},
		},
		{
			name:    "missing meminfo",
			meminfo: "", // Won't create the file
			wantErr: true,
		},
		{
			name:    "malformed meminfo",
			meminfo: "Invalid content\nNo MemTotal here\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, tmpDir := createTestMemoryInfoCollector(t)

			if tt.meminfo != "" || !tt.wantErr {
				meminfoPath := filepath.Join(tmpDir, "proc", "meminfo")
				require.NoError(t, os.WriteFile(meminfoPath, []byte(tt.meminfo), 0644))
			}

			if tt.setupSysfs != nil {
				tt.setupSysfs(t, filepath.Join(tmpDir, "sys"))
			}

			result, err := collector.Collect(context.Background())

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			info, ok := result.Data.(*performance.MemoryInfo)
			require.True(t, ok, "Expected *performance.MemoryInfo, got %T", result)

			if tt.wantInfo != nil {
				tt.wantInfo(t, info)
			}
		})
	}
}

func TestMemoryInfoCollector_ComplexCPUList(t *testing.T) {
	collector, tmpDir := createTestMemoryInfoCollector(t)

	// Create meminfo
	meminfoPath := filepath.Join(tmpDir, "proc", "meminfo")
	require.NoError(t, os.WriteFile(meminfoPath, []byte(testMeminfo), 0644))

	// Create NUMA node with complex CPU list
	nodePath := filepath.Join(tmpDir, "sys", "devices", "system", "node", "node0")
	require.NoError(t, os.MkdirAll(nodePath, 0755))

	// Complex CPU list with ranges and individual CPUs
	require.NoError(t, os.WriteFile(
		filepath.Join(nodePath, "cpulist"),
		[]byte("0-3,8,10-11,15\n"),
		0644,
	))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.Data.(*performance.MemoryInfo)
	require.True(t, ok)

	assert.Len(t, info.NUMANodes, 1)
	assert.Equal(t, []int32{0, 1, 2, 3, 8, 10, 11, 15}, info.NUMANodes[0].CPUs)
}

// NUMA node with empty CPU list
//
// NOTE: This is an extremely unusual scenario in practice. NUMA nodes typically
// always have associated CPUs. An empty CPU list could theoretically occur in:
// - Memory-only nodes (very rare, exotic hardware configurations)
// - Transient states during CPU hotplug operations
// - Hardware configuration errors or firmware bugs
// - Corrupted sysfs state
// This test ensures graceful handling of such edge cases rather than representing
// common real-world scenarios.
func TestMemoryInfoCollector_EmptyCPUList(t *testing.T) {
	collector, tmpDir := createTestMemoryInfoCollector(t)

	meminfoPath := filepath.Join(tmpDir, "proc", "meminfo")
	require.NoError(t, os.WriteFile(meminfoPath, []byte(testMeminfo), 0644))

	nodePath := filepath.Join(tmpDir, "sys", "devices", "system", "node", "node0")
	require.NoError(t, os.MkdirAll(nodePath, 0755))

	require.NoError(t, os.WriteFile(
		filepath.Join(nodePath, "cpulist"),
		[]byte("\n"),
		0644,
	))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.Data.(*performance.MemoryInfo)
	require.True(t, ok)

	assert.Len(t, info.NUMANodes, 1)
	assert.Empty(t, info.NUMANodes[0].CPUs)
}
