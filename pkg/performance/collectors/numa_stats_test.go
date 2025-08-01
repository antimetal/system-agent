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

// Test data constants for NUMA stats collector tests
const testNode0NumaStat = `numa_hit 1234567890
numa_miss 12345
numa_foreign 54321
interleave_hit 9876
local_node 1234567000
other_node 890
`

const testNode1NumaStat = `numa_hit 987654321
numa_miss 23456
numa_foreign 65432
interleave_hit 8765
local_node 987654000
other_node 321
`

const testNode0MeminfoRuntime = `Node 0 MemTotal:       67108864 kB
Node 0 MemFree:        12345678 kB
Node 0 MemUsed:        54763186 kB
Node 0 FilePages:      8192000 kB
Node 0 AnonPages:      46571186 kB
Node 0 Shmem:          16384 kB
Node 0 KernelStack:    32768 kB
Node 0 PageTables:     131072 kB
`

const testNode1MeminfoRuntime = `Node 1 MemTotal:       67108864 kB
Node 1 MemFree:        23456789 kB
Node 1 MemUsed:        43652075 kB
Node 1 FilePages:      4096000 kB
Node 1 AnonPages:      39556075 kB
Node 1 Shmem:          8192 kB
Node 1 KernelStack:    16384 kB
Node 1 PageTables:     65536 kB
`

const testMalformedNumaStat = `numa_hit not-a-number
numa_miss 12345
invalid_field 99999
numa_foreign 54321
`

const testPartialNumaStat = `numa_hit 1000000
numa_miss 5000
# Missing other fields
`

const testEmptyNumaStat = `# All values are zero or missing
numa_hit 0
numa_miss 0
numa_foreign 0
interleave_hit 0
local_node 0
other_node 0
`

// Helper function to create a test NUMA stats collector with mock filesystem
func createTestNUMAStatsCollector(t *testing.T, nodeSetup map[string]map[string]string, numaBalancingEnabled bool) (*collectors.NUMAStatsCollector, string, string) {
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

	// Create /proc/sys/kernel/numa_balancing with current state
	balancingDir := filepath.Join(procPath, "sys", "kernel")
	require.NoError(t, os.MkdirAll(balancingDir, 0755))
	balancingValue := "0"
	if numaBalancingEnabled {
		balancingValue = "1"
	}
	require.NoError(t, os.WriteFile(filepath.Join(balancingDir, "numa_balancing"), []byte(balancingValue), 0644))

	config := performance.CollectionConfig{
		HostProcPath: procPath,
		HostSysPath:  sysPath,
	}
	collector, err := collectors.NewNUMAStatsCollector(logr.Discard(), config)
	require.NoError(t, err)

	return collector, procPath, sysPath
}

// Helper function to collect and validate NUMA stats
func collectAndValidateNUMAStats(t *testing.T, collector *collectors.NUMAStatsCollector, expectError bool, validate func(t *testing.T, result *performance.NUMAStatistics)) {
	result, err := collector.Collect(context.Background())

	if expectError {
		assert.Error(t, err)
		return
	}

	require.NoError(t, err)
	numaStats, ok := result.(*performance.NUMAStatistics)
	require.True(t, ok, "result should be *performance.NUMAStatistics")

	if validate != nil {
		validate(t, numaStats)
	}
}

func TestNUMAStatsCollector_Constructor(t *testing.T) {
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
			collector, err := collectors.NewNUMAStatsCollector(logr.Discard(), tt.config)

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

func TestNUMAStatsCollector_Collect(t *testing.T) {
	tests := []struct {
		name          string
		nodeSetup     map[string]map[string]string
		numaBalancing bool
		validate      func(t *testing.T, stats *performance.NUMAStatistics)
		expectError   bool
	}{
		{
			name: "dual node NUMA system with full stats",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"numastat": testNode0NumaStat,
					"meminfo":  testNode0MeminfoRuntime,
				},
				"node1": {
					"numastat": testNode1NumaStat,
					"meminfo":  testNode1MeminfoRuntime,
				},
			},
			numaBalancing: true,
			validate: func(t *testing.T, stats *performance.NUMAStatistics) {
				assert.True(t, stats.Enabled, "NUMA should be enabled for multi-node system")
				assert.Equal(t, 2, stats.NodeCount)
				assert.True(t, stats.AutoBalanceEnabled)
				assert.Len(t, stats.Nodes, 2)

				// Validate node 0 stats
				node0 := stats.Nodes[0]
				assert.Equal(t, 0, node0.ID)
				assert.Equal(t, uint64(1234567890), node0.NumaHit)
				assert.Equal(t, uint64(12345), node0.NumaMiss)
				assert.Equal(t, uint64(54321), node0.NumaForeign)
				assert.Equal(t, uint64(9876), node0.InterleaveHit)
				assert.Equal(t, uint64(1234567000), node0.LocalNode)
				assert.Equal(t, uint64(890), node0.OtherNode)
				assert.Equal(t, uint64(12345678*1024), node0.MemFree)
				assert.Equal(t, uint64(54763186*1024), node0.MemUsed)
				assert.Equal(t, uint64(8192000*1024), node0.FilePages)
				assert.Equal(t, uint64(46571186*1024), node0.AnonPages)

				// Validate node 1 stats
				node1 := stats.Nodes[1]
				assert.Equal(t, 1, node1.ID)
				assert.Equal(t, uint64(987654321), node1.NumaHit)
				assert.Equal(t, uint64(23456), node1.NumaMiss)
				assert.Equal(t, uint64(65432), node1.NumaForeign)
				assert.Equal(t, uint64(8765), node1.InterleaveHit)
				assert.Equal(t, uint64(987654000), node1.LocalNode)
				assert.Equal(t, uint64(321), node1.OtherNode)
				assert.Equal(t, uint64(23456789*1024), node1.MemFree)
				assert.Equal(t, uint64(43652075*1024), node1.MemUsed)
				assert.Equal(t, uint64(4096000*1024), node1.FilePages)
				assert.Equal(t, uint64(39556075*1024), node1.AnonPages)
			},
		},
		{
			name: "single node system (NUMA disabled)",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"numastat": testNode0NumaStat,
					"meminfo":  testNode0MeminfoRuntime,
				},
			},
			numaBalancing: false,
			validate: func(t *testing.T, stats *performance.NUMAStatistics) {
				assert.False(t, stats.Enabled, "NUMA should be disabled for single-node system")
				assert.Equal(t, 1, stats.NodeCount)
				assert.False(t, stats.AutoBalanceEnabled)
				assert.Empty(t, stats.Nodes, "No node stats should be collected for non-NUMA system")
			},
		},
		{
			name: "NUMA system with missing meminfo (graceful degradation)",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"numastat": testNode0NumaStat,
					// Missing meminfo file
				},
				"node1": {
					"numastat": testNode1NumaStat,
					"meminfo":  testNode1MeminfoRuntime,
				},
			},
			numaBalancing: false,
			validate: func(t *testing.T, stats *performance.NUMAStatistics) {
				assert.True(t, stats.Enabled)
				assert.Equal(t, 2, stats.NodeCount)
				assert.False(t, stats.AutoBalanceEnabled)
				assert.Len(t, stats.Nodes, 2)

				// Node 0 should have NUMA stats but no memory info
				node0 := stats.Nodes[0]
				assert.Equal(t, uint64(1234567890), node0.NumaHit)
				assert.Equal(t, uint64(0), node0.MemFree) // Default value
				assert.Equal(t, uint64(0), node0.MemUsed) // Default value

				// Node 1 should have complete data
				node1 := stats.Nodes[1]
				assert.Equal(t, uint64(987654321), node1.NumaHit)
				assert.Equal(t, uint64(23456789*1024), node1.MemFree)
				assert.Equal(t, uint64(43652075*1024), node1.MemUsed)
			},
		},
		{
			name: "malformed numastat handling",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"numastat": testMalformedNumaStat,
					"meminfo":  testNode0MeminfoRuntime,
				},
				"node1": {
					"numastat": testNode1NumaStat,
					"meminfo":  testNode1MeminfoRuntime,
				},
			},
			validate: func(t *testing.T, stats *performance.NUMAStatistics) {
				assert.True(t, stats.Enabled)
				assert.Len(t, stats.Nodes, 2) // Both nodes collected, node 0 with partial data

				// Node 0 should have partial data (only numa_miss and numa_foreign parsed)
				node0 := stats.Nodes[0]
				assert.Equal(t, 0, node0.ID)
				assert.Equal(t, uint64(0), node0.NumaHit) // Failed to parse "not-a-number"
				assert.Equal(t, uint64(12345), node0.NumaMiss) // Valid
				assert.Equal(t, uint64(54321), node0.NumaForeign) // Valid
				
				// Node 1 should have complete data
				node1 := stats.Nodes[1]
				assert.Equal(t, 1, node1.ID)
				assert.Equal(t, uint64(987654321), node1.NumaHit)
			},
		},
		{
			name: "partial numastat data",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"numastat": testPartialNumaStat,
					"meminfo":  testNode0MeminfoRuntime,
				},
				"node1": {
					"numastat": testNode1NumaStat,
					"meminfo":  testNode1MeminfoRuntime,
				},
			},
			validate: func(t *testing.T, stats *performance.NUMAStatistics) {
				assert.True(t, stats.Enabled)
				assert.Len(t, stats.Nodes, 2)

				// Node 0 should have partial data (missing fields default to 0)
				node0 := stats.Nodes[0]
				assert.Equal(t, uint64(1000000), node0.NumaHit)
				assert.Equal(t, uint64(5000), node0.NumaMiss)
				assert.Equal(t, uint64(0), node0.NumaForeign)   // Missing field
				assert.Equal(t, uint64(0), node0.InterleaveHit) // Missing field
			},
		},
		{
			name: "empty numastat fails validation",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"numastat": testEmptyNumaStat,
					"meminfo":  testNode0MeminfoRuntime,
				},
				"node1": {
					"numastat": testNode1NumaStat,
					"meminfo":  testNode1MeminfoRuntime,
				},
			},
			validate: func(t *testing.T, stats *performance.NUMAStatistics) {
				assert.True(t, stats.Enabled)
				assert.Len(t, stats.Nodes, 1) // Node 0 should fail validation, only node 1 collected

				// Only node 1 should be present
				node1 := stats.Nodes[0]
				assert.Equal(t, 1, node1.ID)
				assert.Equal(t, uint64(987654321), node1.NumaHit)
			},
		},
		{
			name: "missing critical numastat file",
			nodeSetup: map[string]map[string]string{
				"node0": {
					// Missing numastat file - this should cause collection to fail for this node
					"meminfo": testNode0MeminfoRuntime,
				},
				"node1": {
					"numastat": testNode1NumaStat,
					"meminfo":  testNode1MeminfoRuntime,
				},
			},
			validate: func(t *testing.T, stats *performance.NUMAStatistics) {
				assert.True(t, stats.Enabled)
				assert.Len(t, stats.Nodes, 1) // Only node 1 should be collected

				// Only node 1 should be present
				node1 := stats.Nodes[0]
				assert.Equal(t, 1, node1.ID)
				assert.Equal(t, uint64(987654321), node1.NumaHit)
			},
		},
		{
			name: "balanced vs unbalanced states",
			nodeSetup: map[string]map[string]string{
				"node0": {
					"numastat": testNode0NumaStat,
					"meminfo":  testNode0MeminfoRuntime,
				},
				"node1": {
					"numastat": testNode1NumaStat,
					"meminfo":  testNode1MeminfoRuntime,
				},
			},
			numaBalancing: false,
			validate: func(t *testing.T, stats *performance.NUMAStatistics) {
				assert.True(t, stats.Enabled)
				assert.False(t, stats.AutoBalanceEnabled, "Auto-balance should be disabled")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, _, _ := createTestNUMAStatsCollector(t, tt.nodeSetup, tt.numaBalancing)
			collectAndValidateNUMAStats(t, collector, tt.expectError, tt.validate)
		})
	}
}

func TestNUMAStatsCollector_NoNUMASupport(t *testing.T) {
	// Test system without NUMA support (no /sys/devices/system/node directory)
	tmpDir := t.TempDir()
	procPath := filepath.Join(tmpDir, "proc")
	sysPath := filepath.Join(tmpDir, "sys")

	// Create directories but NOT the node directory
	require.NoError(t, os.MkdirAll(procPath, 0755))
	require.NoError(t, os.MkdirAll(sysPath, 0755))

	// Create numa_balancing file
	balancingDir := filepath.Join(procPath, "sys", "kernel")
	require.NoError(t, os.MkdirAll(balancingDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(balancingDir, "numa_balancing"), []byte("0"), 0644))

	config := performance.CollectionConfig{
		HostProcPath: procPath,
		HostSysPath:  sysPath,
	}
	collector, err := collectors.NewNUMAStatsCollector(logr.Discard(), config)
	require.NoError(t, err)

	collectAndValidateNUMAStats(t, collector, false, func(t *testing.T, stats *performance.NUMAStatistics) {
		assert.False(t, stats.Enabled, "NUMA should be disabled when no node directory exists")
		assert.Equal(t, 0, stats.NodeCount)
		assert.False(t, stats.AutoBalanceEnabled)
		assert.Empty(t, stats.Nodes)
	})
}

func TestNUMAStatsCollector_InvalidDirectoryAccess(t *testing.T) {
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
	collector, err := collectors.NewNUMAStatsCollector(logr.Discard(), config)
	require.NoError(t, err)

	collectAndValidateNUMAStats(t, collector, true, nil)
}

func TestNUMAStatsCollector_MissingBalancingFile(t *testing.T) {
	// Test system without numa_balancing file (older kernels)
	nodeSetup := map[string]map[string]string{
		"node0": {
			"numastat": testNode0NumaStat,
			"meminfo":  testNode0MeminfoRuntime,
		},
		"node1": {
			"numastat": testNode1NumaStat,
			"meminfo":  testNode1MeminfoRuntime,
		},
	}

	tmpDir := t.TempDir()
	procPath := filepath.Join(tmpDir, "proc")
	sysPath := filepath.Join(tmpDir, "sys")

	// Create directory structure but don't create numa_balancing file
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

	config := performance.CollectionConfig{
		HostProcPath: procPath,
		HostSysPath:  sysPath,
	}
	collector, err := collectors.NewNUMAStatsCollector(logr.Discard(), config)
	require.NoError(t, err)

	collectAndValidateNUMAStats(t, collector, false, func(t *testing.T, stats *performance.NUMAStatistics) {
		assert.True(t, stats.Enabled)
		assert.False(t, stats.AutoBalanceEnabled, "Should default to false when file missing")
		assert.Len(t, stats.Nodes, 2)
	})
}

func TestNUMAStatsCollector_WhitespaceHandling(t *testing.T) {
	// Test handling of files with whitespace and line variations
	numastatWithWhitespace := `  numa_hit  1234567890  
numa_miss	12345
	numa_foreign 54321	
interleave_hit 9876
local_node 1234567000
other_node 890  
`

	nodeSetup := map[string]map[string]string{
		"node0": {
			"numastat": numastatWithWhitespace,
			"meminfo":  testNode0MeminfoRuntime,
		},
	}

	collector, _, _ := createTestNUMAStatsCollector(t, nodeSetup, false)
	collectAndValidateNUMAStats(t, collector, false, func(t *testing.T, stats *performance.NUMAStatistics) {
		assert.False(t, stats.Enabled) // Single node = not NUMA
		assert.Equal(t, 1, stats.NodeCount)
	})
}

func TestNUMAStatsCollector_LargeCounterValues(t *testing.T) {
	// Test handling of very large counter values (near uint64 max)
	largeCountersNumaStat := `numa_hit 18446744073709551615
numa_miss 18446744073709551614
numa_foreign 18446744073709551613
interleave_hit 18446744073709551612
local_node 18446744073709551611
other_node 18446744073709551610
`

	nodeSetup := map[string]map[string]string{
		"node0": {
			"numastat": largeCountersNumaStat,
			"meminfo":  testNode0MeminfoRuntime,
		},
		"node1": {
			"numastat": testNode1NumaStat,
			"meminfo":  testNode1MeminfoRuntime,
		},
	}

	collector, _, _ := createTestNUMAStatsCollector(t, nodeSetup, false)
	collectAndValidateNUMAStats(t, collector, false, func(t *testing.T, stats *performance.NUMAStatistics) {
		assert.True(t, stats.Enabled)
		assert.Len(t, stats.Nodes, 2)

		// Validate large counter values
		node0 := stats.Nodes[0]
		assert.Equal(t, uint64(18446744073709551615), node0.NumaHit)
		assert.Equal(t, uint64(18446744073709551614), node0.NumaMiss)
		assert.Equal(t, uint64(18446744073709551613), node0.NumaForeign)
	})
}
