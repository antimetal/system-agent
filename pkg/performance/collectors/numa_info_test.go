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

// Test data constants for NUMA info collector tests
const testNode0CPUList = "0-11,24-35"
const testNode1CPUList = "12-23,36-47"

const testNode0Distance = "10 21"
const testNode1Distance = "21 10"

const testNode0Meminfo = `Node 0 MemTotal:       67108864 kB
Node 0 MemFree:        12345678 kB
Node 0 MemUsed:        54763186 kB
Node 0 FilePages:      8192000 kB
Node 0 AnonPages:      46571186 kB
Node 0 Shmem:          16384 kB
Node 0 KernelStack:    32768 kB
Node 0 PageTables:     131072 kB
`

const testNode1Meminfo = `Node 1 MemTotal:       67108864 kB
Node 1 MemFree:        23456789 kB
Node 1 MemUsed:        43652075 kB
Node 1 FilePages:      4096000 kB
Node 1 AnonPages:      39556075 kB
Node 1 Shmem:          8192 kB
Node 1 KernelStack:    16384 kB
Node 1 PageTables:     65536 kB
`

const testSingleNodeMeminfo = `Node 0 MemTotal:       16777216 kB
Node 0 MemFree:        8388608 kB
Node 0 MemUsed:        8388608 kB
Node 0 FilePages:      4194304 kB
Node 0 AnonPages:      4194304 kB
`

// Helper function to create a test NUMA info collector with mock filesystem
func createTestNUMAInfoCollector(t *testing.T, nodeSetup map[string]map[string]string, numaBalancing bool) (*collectors.NUMAInfoCollector, string, string) {
	tmpDir := t.TempDir()
	procPath := filepath.Join(tmpDir, "proc")
	sysPath := filepath.Join(tmpDir, "sys")

	// Create directory structure
	require.NoError(t, os.MkdirAll(procPath, 0755))
	require.NoError(t, os.MkdirAll(sysPath, 0755))

	nodesPath := filepath.Join(sysPath, "devices", "system", "node")
	require.NoError(t, os.MkdirAll(nodesPath, 0755))

	// Create node directories and files
	for nodeID, files := range nodeSetup {
		nodePath := filepath.Join(nodesPath, nodeID)
		require.NoError(t, os.MkdirAll(nodePath, 0755))

		for fileName, content := range files {
			filePath := filepath.Join(nodePath, fileName)
			require.NoError(t, os.WriteFile(filePath, []byte(content), 0644))
		}
	}

	// Create /proc/sys/kernel/numa_balancing if needed
	if numaBalancing {
		balancingDir := filepath.Join(procPath, "sys", "kernel")
		require.NoError(t, os.MkdirAll(balancingDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(balancingDir, "numa_balancing"), []byte("1"), 0644))
	}

	config := performance.CollectionConfig{
		HostProcPath: procPath,
		HostSysPath:  sysPath,
	}
	collector, err := collectors.NewNUMAInfoCollector(logr.Discard(), config)
	require.NoError(t, err)

	return collector, procPath, sysPath
}

// Helper function to collect and validate NUMA info
func collectAndValidateNUMAInfo(t *testing.T, collector *collectors.NUMAInfoCollector, expectError bool, validate func(t *testing.T, result *performance.NUMAInfo)) {
	result, err := collector.Collect(context.Background())

	if expectError {
		assert.Error(t, err)
		return
	}

	require.NoError(t, err)
	numaInfo, ok := result.(*performance.NUMAInfo)
	require.True(t, ok, "result should be *performance.NUMAInfo")

	if validate != nil {
		validate(t, numaInfo)
	}
}

func TestNUMAInfoCollector_Constructor(t *testing.T) {
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
			name: "missing proc path",
			config: performance.CollectionConfig{
				HostSysPath: "/sys",
			},
			wantErr: true,
			errMsg:  "HostProcPath is required",
		},
		{
			name: "missing sys path",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
			},
			wantErr: true,
			errMsg:  "HostSysPath is required",
		},
		{
			name: "relative proc path",
			config: performance.CollectionConfig{
				HostProcPath: "proc",
				HostSysPath:  "/sys",
			},
			wantErr: true,
			errMsg:  "must be an absolute path",
		},
		{
			name: "relative sys path",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
				HostSysPath:  "sys",
			},
			wantErr: true,
			errMsg:  "must be an absolute path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := collectors.NewNUMAInfoCollector(logr.Discard(), tt.config)

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

func TestNUMAInfoCollector_Collect(t *testing.T) {
	tests := []struct {
		name          string
		nodeSetup     map[string]map[string]string
		numaBalancing bool
		validate      func(t *testing.T, info *performance.NUMAInfo)
		expectError   bool
	}{
		{
			name: "dual node NUMA system with balancing",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"cpulist":  testNode0CPUList,
					"distance": testNode0Distance,
					"meminfo":  testNode0Meminfo,
				},
				"node1": {
					"cpulist":  testNode1CPUList,
					"distance": testNode1Distance,
					"meminfo":  testNode1Meminfo,
				},
			},
			numaBalancing: true,
			validate: func(t *testing.T, info *performance.NUMAInfo) {
				assert.True(t, info.Enabled, "NUMA should be enabled for multi-node system")
				assert.Equal(t, 2, info.NodeCount)
				assert.True(t, info.BalancingAvailable)
				assert.Len(t, info.Nodes, 2)

				// Validate node 0
				node0 := info.Nodes[0]
				assert.Equal(t, 0, node0.ID)
				expectedCPUs0 := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35}
				assert.Equal(t, expectedCPUs0, node0.CPUs)
				assert.Equal(t, uint64(67108864*1024), node0.TotalMemory) // 64 GB in bytes
				assert.Equal(t, []int{10, 21}, node0.Distances)

				// Validate node 1
				node1 := info.Nodes[1]
				assert.Equal(t, 1, node1.ID)
				expectedCPUs1 := []int{12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47}
				assert.Equal(t, expectedCPUs1, node1.CPUs)
				assert.Equal(t, uint64(67108864*1024), node1.TotalMemory) // 64 GB in bytes
				assert.Equal(t, []int{21, 10}, node1.Distances)
			},
		},
		{
			name: "single node system (NUMA disabled)",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"cpulist":  "0-7",
					"distance": "10",
					"meminfo":  testSingleNodeMeminfo,
				},
			},
			numaBalancing: false,
			validate: func(t *testing.T, info *performance.NUMAInfo) {
				assert.False(t, info.Enabled, "NUMA should be disabled for single-node system")
				assert.Equal(t, 1, info.NodeCount)
				assert.False(t, info.BalancingAvailable)
				assert.Empty(t, info.Nodes, "No node details should be collected for non-NUMA system")
			},
		},
		{
			name: "NUMA system with partial data",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"cpulist": testNode0CPUList,
					"meminfo": testNode0Meminfo,
					// Missing distance file
				},
				"node1": {
					"distance": testNode1Distance,
					"meminfo":  testNode1Meminfo,
					// Missing cpulist file
				},
			},
			numaBalancing: false,
			validate: func(t *testing.T, info *performance.NUMAInfo) {
				assert.True(t, info.Enabled)
				assert.Equal(t, 2, info.NodeCount)
				assert.False(t, info.BalancingAvailable)
				assert.Len(t, info.Nodes, 2)

				// Node 0 should have CPUs and memory but no distances
				node0 := info.Nodes[0]
				assert.Equal(t, 0, node0.ID)
				assert.Len(t, node0.CPUs, 24) // Should have parsed CPUs
				assert.Equal(t, uint64(67108864*1024), node0.TotalMemory)
				assert.Empty(t, node0.Distances) // Missing distance file

				// Node 1 should have distances and memory but no CPUs
				node1 := info.Nodes[1]
				assert.Equal(t, 1, node1.ID)
				assert.Empty(t, node1.CPUs) // Missing cpulist file
				assert.Equal(t, uint64(67108864*1024), node1.TotalMemory)
				assert.Equal(t, []int{21, 10}, node1.Distances)
			},
		},
		{
			name: "complex CPU list parsing",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"cpulist": "0,2,4,6-8,10-15",
					"meminfo": testSingleNodeMeminfo,
				},
				"node1": {
					"cpulist": "1,3,5,9,16-31",
					"meminfo": testSingleNodeMeminfo,
				},
			},
			validate: func(t *testing.T, info *performance.NUMAInfo) {
				assert.True(t, info.Enabled)
				assert.Len(t, info.Nodes, 2)

				// Validate complex CPU list parsing
				node0 := info.Nodes[0]
				expectedCPUs0 := []int{0, 2, 4, 6, 7, 8, 10, 11, 12, 13, 14, 15}
				assert.Equal(t, expectedCPUs0, node0.CPUs)

				node1 := info.Nodes[1]
				expectedCPUs1 := []int{1, 3, 5, 9, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
				assert.Equal(t, expectedCPUs1, node1.CPUs)
			},
		},
		{
			name: "malformed data handling",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"cpulist":  "0-3,invalid-range,8,bad",
					"distance": "10 invalid 30",
					"meminfo":  "Node 0 MemTotal: not-a-number kB\nNode 0 MemFree: 1024 kB",
				},
				"node1": {
					"cpulist":  "4-7",
					"distance": "",
					"meminfo":  testSingleNodeMeminfo,
				},
			},
			validate: func(t *testing.T, info *performance.NUMAInfo) {
				assert.True(t, info.Enabled)
				assert.Len(t, info.Nodes, 2)

				// Node 0: Should have parsed valid CPUs, failed on distances and memory
				node0 := info.Nodes[0]
				expectedCPUs0 := []int{0, 1, 2, 3, 8} // Should skip invalid ranges
				assert.Equal(t, expectedCPUs0, node0.CPUs)
				assert.Empty(t, node0.Distances)              // Should fail on invalid distance
				assert.Equal(t, uint64(0), node0.TotalMemory) // Should fail on invalid MemTotal

				// Node 1: Should have valid data
				node1 := info.Nodes[1]
				expectedCPUs1 := []int{4, 5, 6, 7}
				assert.Equal(t, expectedCPUs1, node1.CPUs)
				assert.Empty(t, node1.Distances) // Empty distance file
				assert.Equal(t, uint64(16777216*1024), node1.TotalMemory)
			},
		},
		{
			name: "empty CPU list",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"cpulist": "",
					"meminfo": testSingleNodeMeminfo,
				},
				"node1": {
					"cpulist": "0-3",
					"meminfo": testSingleNodeMeminfo,
				},
			},
			validate: func(t *testing.T, info *performance.NUMAInfo) {
				assert.True(t, info.Enabled)
				assert.Len(t, info.Nodes, 2)

				// Node 0 should have empty CPU list
				node0 := info.Nodes[0]
				assert.Empty(t, node0.CPUs)

				// Node 1 should have valid CPUs
				node1 := info.Nodes[1]
				assert.Equal(t, []int{0, 1, 2, 3}, node1.CPUs)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, _, _ := createTestNUMAInfoCollector(t, tt.nodeSetup, tt.numaBalancing)
			collectAndValidateNUMAInfo(t, collector, tt.expectError, tt.validate)
		})
	}
}

func TestNUMAInfoCollector_NoNUMASupport(t *testing.T) {
	// Test system without NUMA support (no /sys/devices/system/node directory)
	tmpDir := t.TempDir()
	procPath := filepath.Join(tmpDir, "proc")
	sysPath := filepath.Join(tmpDir, "sys")

	// Create directories but NOT the node directory
	require.NoError(t, os.MkdirAll(procPath, 0755))
	require.NoError(t, os.MkdirAll(sysPath, 0755))

	config := performance.CollectionConfig{
		HostProcPath: procPath,
		HostSysPath:  sysPath,
	}
	collector, err := collectors.NewNUMAInfoCollector(logr.Discard(), config)
	require.NoError(t, err)

	collectAndValidateNUMAInfo(t, collector, false, func(t *testing.T, info *performance.NUMAInfo) {
		assert.False(t, info.Enabled, "NUMA should be disabled when no node directory exists")
		assert.Equal(t, 0, info.NodeCount)
		assert.False(t, info.BalancingAvailable)
		assert.Empty(t, info.Nodes)
	})
}

func TestNUMAInfoCollector_InvalidDirectoryAccess(t *testing.T) {
	// Test handling of permission denied on node directory
	tmpDir := t.TempDir()
	procPath := filepath.Join(tmpDir, "proc")
	sysPath := filepath.Join(tmpDir, "sys")

	// Create node directory but make it inaccessible
	nodesPath := filepath.Join(sysPath, "devices", "system", "node")
	require.NoError(t, os.MkdirAll(nodesPath, 0755))
	require.NoError(t, os.Chmod(nodesPath, 0000)) // No permissions

	// Restore permissions for cleanup
	defer func() {
		os.Chmod(nodesPath, 0755)
	}()

	config := performance.CollectionConfig{
		HostProcPath: procPath,
		HostSysPath:  sysPath,
	}
	collector, err := collectors.NewNUMAInfoCollector(logr.Discard(), config)
	require.NoError(t, err)

	collectAndValidateNUMAInfo(t, collector, true, nil)
}

func TestNUMAInfoCollector_WhitespaceHandling(t *testing.T) {
	// Test handling of files with whitespace
	nodeSetup := map[string]map[string]string{
		"node0": {
			"cpulist":  "  0-3  \n",
			"distance": "\t10 21  \n",
			"meminfo":  testNode0Meminfo,
		},
	}

	collector, _, _ := createTestNUMAInfoCollector(t, nodeSetup, false)
	collectAndValidateNUMAInfo(t, collector, false, func(t *testing.T, info *performance.NUMAInfo) {
		assert.False(t, info.Enabled) // Single node = not NUMA
		assert.Equal(t, 1, info.NodeCount)
	})
}

func TestNUMAInfoCollector_EdgeCaseDistances(t *testing.T) {
	// Test various distance configurations
	tests := []struct {
		name      string
		distances string
		expected  []int
		shouldErr bool
	}{
		{
			name:      "single node distance",
			distances: "10",
			expected:  []int{10},
		},
		{
			name:      "multi-node distances",
			distances: "10 21 31 42",
			expected:  []int{10, 21, 31, 42},
		},
		{
			name:      "zero distance (invalid but handled)",
			distances: "0 10",
			expected:  []int{0, 10},
		},
		{
			name:      "large distances",
			distances: "10 255",
			expected:  []int{10, 255},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeSetup := map[string]map[string]string{
				"node0": {
					"distance": tt.distances,
					"meminfo":  testSingleNodeMeminfo,
				},
			}

			collector, _, _ := createTestNUMAInfoCollector(t, nodeSetup, false)
			collectAndValidateNUMAInfo(t, collector, false, func(t *testing.T, info *performance.NUMAInfo) {
				if len(info.Nodes) > 0 {
					assert.Equal(t, tt.expected, info.Nodes[0].Distances)
				}
			})
		})
	}
}
