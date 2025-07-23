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

// Test data from real systems
const (
	// /sys/devices/system/node/node0/numastat
	validNumaStat = `numa_hit 1234567890
numa_miss 12345
numa_foreign 54321
interleave_hit 9876
local_node 1234567000
other_node 890`

	// /sys/devices/system/node/node0/meminfo
	validNodeMeminfo = `Node 0 MemTotal:       67108864 kB
Node 0 MemFree:        12345678 kB
Node 0 MemUsed:        54763186 kB
Node 0 Active:         40123456 kB
Node 0 Inactive:       14639730 kB
Node 0 Active(anon):   30123456 kB
Node 0 Inactive(anon):  2345678 kB
Node 0 Active(file):   10000000 kB
Node 0 Inactive(file): 12294052 kB
Node 0 Unevictable:           0 kB
Node 0 Mlocked:               0 kB
Node 0 Dirty:              1234 kB
Node 0 Writeback:             0 kB
Node 0 FilePages:      22294052 kB
Node 0 Mapped:          2345678 kB
Node 0 AnonPages:      32469134 kB
Node 0 Shmem:           1234567 kB
Node 0 KernelStack:       12345 kB
Node 0 PageTables:       123456 kB
Node 0 NFS_Unstable:          0 kB
Node 0 Bounce:                0 kB
Node 0 WritebackTmp:          0 kB
Node 0 Slab:            2345678 kB
Node 0 SReclaimable:    1234567 kB
Node 0 SUnreclaim:      1111111 kB
Node 0 AnonHugePages:   2097152 kB
Node 0 HugePages_Total:       0
Node 0 HugePages_Free:        0
Node 0 HugePages_Surp:        0`

	// /sys/devices/system/node/node0/cpulist
	validCPUList = "0-7"

	// /sys/devices/system/node/node0/distance
	validDistances = "10 21"

	// Malformed data for error cases
	malformedNumaStat = `numa_hit not_a_number
numa_miss 12345
invalid line format
numa_foreign`

	malformedNodeMeminfo = `Invalid format line
Node 0 MemTotal: not_a_number kB
Node 0 MemFree:        12345678
Node 0 MemUsed`
)

// Helper function to create a test NUMA collector
func createTestNUMACollector(t *testing.T, sysFiles map[string]string, procFiles map[string]string) (*collectors.NUMACollector, string, string) {
	tmpDir := t.TempDir()
	sysPath := filepath.Join(tmpDir, "sys")
	procPath := filepath.Join(tmpDir, "proc")

	// Create sys filesystem structure
	for path, content := range sysFiles {
		fullPath := filepath.Join(sysPath, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	// Create proc filesystem structure
	for path, content := range procFiles {
		fullPath := filepath.Join(procPath, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	config := performance.CollectionConfig{
		HostSysPath:  sysPath,
		HostProcPath: procPath,
	}

	collector, err := collectors.NewNUMACollector(logr.Discard(), config)
	require.NoError(t, err)

	return collector, sysPath, procPath
}

// Helper function to collect and validate NUMA stats
func collectAndValidateNUMA(t *testing.T, collector *collectors.NUMACollector, expectError bool, validate func(t *testing.T, stats *performance.NUMAStats)) {
	result, err := collector.Collect(context.Background())

	if expectError {
		assert.Error(t, err)
		return
	}

	require.NoError(t, err)
	stats, ok := result.(*performance.NUMAStats)
	require.True(t, ok, "result should be *performance.NUMAStats")

	if validate != nil {
		validate(t, stats)
	}
}

func TestNUMACollector_Constructor(t *testing.T) {
	tests := []struct {
		name        string
		config      performance.CollectionConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid paths",
			config: performance.CollectionConfig{
				HostSysPath:  "/sys",
				HostProcPath: "/proc",
			},
			expectError: false,
		},
		{
			name: "relative sys path",
			config: performance.CollectionConfig{
				HostSysPath:  "sys",
				HostProcPath: "/proc",
			},
			expectError: true,
			errorMsg:    "HostSysPath must be an absolute path",
		},
		{
			name: "relative proc path",
			config: performance.CollectionConfig{
				HostSysPath:  "/sys",
				HostProcPath: "proc",
			},
			expectError: true,
			errorMsg:    "HostProcPath must be an absolute path",
		},
		{
			name: "empty paths",
			config: performance.CollectionConfig{
				HostSysPath:  "",
				HostProcPath: "",
			},
			expectError: true,
			errorMsg:    "must be an absolute path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := collectors.NewNUMACollector(logr.Discard(), tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
			}
		})
	}
}

func TestNUMACollector_Collect(t *testing.T) {
	tests := []struct {
		name      string
		sysFiles  map[string]string
		procFiles map[string]string
		validate  func(t *testing.T, stats *performance.NUMAStats)
	}{
		{
			name:     "non-NUMA system (no nodes)",
			sysFiles: map[string]string{},
			validate: func(t *testing.T, stats *performance.NUMAStats) {
				assert.False(t, stats.Enabled)
				assert.Equal(t, 0, stats.NodeCount)
				assert.Empty(t, stats.Nodes)
			},
		},
		{
			name: "single node system (not NUMA)",
			sysFiles: map[string]string{
				"devices/system/node/node0/numastat": validNumaStat,
			},
			validate: func(t *testing.T, stats *performance.NUMAStats) {
				assert.False(t, stats.Enabled)
				assert.Equal(t, 1, stats.NodeCount)
				assert.Empty(t, stats.Nodes)
			},
		},
		{
			name: "two node NUMA system with full data",
			sysFiles: map[string]string{
				"devices/system/node/node0/numastat": validNumaStat,
				"devices/system/node/node0/meminfo":  validNodeMeminfo,
				"devices/system/node/node0/cpulist":  validCPUList,
				"devices/system/node/node0/distance": validDistances,
				"devices/system/node/node1/numastat": `numa_hit 987654321
numa_miss 54321
numa_foreign 12345
interleave_hit 5432
local_node 987600000
other_node 54321`,
				"devices/system/node/node1/meminfo": `Node 1 MemTotal:       67108864 kB
Node 1 MemFree:        32212255 kB
Node 1 MemUsed:        34896609 kB
Node 1 FilePages:      12345678 kB
Node 1 AnonPages:      22550931 kB`,
				"devices/system/node/node1/cpulist":  "8-15",
				"devices/system/node/node1/distance": "21 10",
			},
			procFiles: map[string]string{
				"sys/kernel/numa_balancing": "1",
			},
			validate: func(t *testing.T, stats *performance.NUMAStats) {
				assert.True(t, stats.Enabled)
				assert.Equal(t, 2, stats.NodeCount)
				assert.True(t, stats.AutoBalance)
				require.Len(t, stats.Nodes, 2)

				// Validate node 0
				node0 := stats.Nodes[0]
				assert.Equal(t, 0, node0.ID)
				assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 7}, node0.CPUs)
				assert.Equal(t, uint64(67108864*1024), node0.MemTotal) // 64GB in bytes
				assert.Equal(t, uint64(12345678*1024), node0.MemFree)  // from test data
				assert.Equal(t, uint64(54763186*1024), node0.MemUsed)  // from test data
				assert.Equal(t, uint64(1234567890), node0.NumaHit)
				assert.Equal(t, uint64(12345), node0.NumaMiss)
				assert.Equal(t, uint64(54321), node0.NumaForeign)
				assert.Equal(t, []int{10, 21}, node0.Distances)

				// Validate node 1
				node1 := stats.Nodes[1]
				assert.Equal(t, 1, node1.ID)
				assert.Equal(t, []int{8, 9, 10, 11, 12, 13, 14, 15}, node1.CPUs)
				assert.Equal(t, uint64(987654321), node1.NumaHit)
				assert.Equal(t, uint64(54321), node1.NumaMiss)
			},
		},
		{
			name: "NUMA node with missing optional files",
			sysFiles: map[string]string{
				"devices/system/node/node0/numastat": validNumaStat,
				"devices/system/node/node1/numastat": validNumaStat,
				// Missing meminfo, cpulist, distance files
			},
			validate: func(t *testing.T, stats *performance.NUMAStats) {
				assert.True(t, stats.Enabled)
				assert.Equal(t, 2, stats.NodeCount)
				require.Len(t, stats.Nodes, 2)

				// Should still have critical numastat data
				node0 := stats.Nodes[0]
				assert.Equal(t, uint64(1234567890), node0.NumaHit)
				assert.Equal(t, uint64(12345), node0.NumaMiss)

				// Optional data should be zero/empty
				assert.Equal(t, uint64(0), node0.MemTotal)
				assert.Empty(t, node0.CPUs)
				assert.Empty(t, node0.Distances)
			},
		},
		{
			name: "NUMA node with completely malformed numastat",
			sysFiles: map[string]string{
				"devices/system/node/node0/numastat": "completely invalid data",
				"devices/system/node/node0/meminfo":  validNodeMeminfo,
				"devices/system/node/node0/cpulist":  "0-7",
				"devices/system/node/node1/numastat": validNumaStat,
			},
			validate: func(t *testing.T, stats *performance.NUMAStats) {
				assert.True(t, stats.Enabled)
				// Node 0 should be skipped due to critical numastat being empty
				assert.Equal(t, 1, len(stats.Nodes))
				assert.Equal(t, 1, stats.Nodes[0].ID)
			},
		},
		{
			name: "NUMA node with partially malformed data",
			sysFiles: map[string]string{
				"devices/system/node/node0/numastat": malformedNumaStat,
				"devices/system/node/node0/meminfo":  malformedNodeMeminfo,
				"devices/system/node/node0/cpulist":  "0-3,not_a_number,8-11",
				"devices/system/node/node0/distance": "10 invalid 21",
				"devices/system/node/node1/numastat": validNumaStat,
			},
			validate: func(t *testing.T, stats *performance.NUMAStats) {
				assert.True(t, stats.Enabled)
				assert.Equal(t, 2, len(stats.Nodes))

				// Node 0 should have partial data
				node0 := stats.Nodes[0]
				assert.Equal(t, 0, node0.ID)
				assert.Equal(t, uint64(12345), node0.NumaMiss)               // Only valid line in malformed data
				assert.Equal(t, uint64(0), node0.NumaHit)                    // Invalid line, should be 0
				assert.Equal(t, []int{0, 1, 2, 3, 8, 9, 10, 11}, node0.CPUs) // Partial parsing, ranges still work
			},
		},
		{
			name: "NUMA with auto-balance disabled",
			sysFiles: map[string]string{
				"devices/system/node/node0/numastat": validNumaStat,
				"devices/system/node/node1/numastat": validNumaStat,
			},
			procFiles: map[string]string{
				"sys/kernel/numa_balancing": "0",
			},
			validate: func(t *testing.T, stats *performance.NUMAStats) {
				assert.True(t, stats.Enabled)
				assert.False(t, stats.AutoBalance)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, _, _ := createTestNUMACollector(t, tt.sysFiles, tt.procFiles)
			collectAndValidateNUMA(t, collector, false, tt.validate)
		})
	}
}

func TestNUMACollector_CPUListParsing(t *testing.T) {
	tests := []struct {
		name     string
		cpuList  string
		expected []int
	}{
		{
			name:     "single CPU",
			cpuList:  "0",
			expected: []int{0},
		},
		{
			name:     "range of CPUs",
			cpuList:  "0-7",
			expected: []int{0, 1, 2, 3, 4, 5, 6, 7},
		},
		{
			name:     "comma-separated CPUs",
			cpuList:  "0,2,4,6",
			expected: []int{0, 2, 4, 6},
		},
		{
			name:     "mixed ranges and singles",
			cpuList:  "0-3,8,10-11,15",
			expected: []int{0, 1, 2, 3, 8, 10, 11, 15},
		},
		{
			name:     "empty cpulist",
			cpuList:  "",
			expected: []int{},
		},
		{
			name:     "cpulist with spaces",
			cpuList:  " 0-3 , 8 , 10-11 ",
			expected: []int{0, 1, 2, 3, 8, 10, 11},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sysFiles := map[string]string{
				"devices/system/node/node0/numastat": validNumaStat,
				"devices/system/node/node0/cpulist":  tt.cpuList,
				"devices/system/node/node1/numastat": validNumaStat,
			}

			collector, _, _ := createTestNUMACollector(t, sysFiles, nil)
			result, err := collector.Collect(context.Background())
			require.NoError(t, err)

			stats := result.(*performance.NUMAStats)
			require.Len(t, stats.Nodes, 2)
			assert.Equal(t, tt.expected, stats.Nodes[0].CPUs)
		})
	}
}

func TestNUMACollector_MissingCriticalFiles(t *testing.T) {
	// Test when critical numastat file is missing
	sysFiles := map[string]string{
		"devices/system/node/node0/meminfo": validNodeMeminfo,
		"devices/system/node/node0/cpulist": validCPUList,
		// Missing numastat - critical file
		"devices/system/node/node1/numastat": validNumaStat,
	}

	collector, _, _ := createTestNUMACollector(t, sysFiles, nil)
	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats := result.(*performance.NUMAStats)
	// Node 0 should be skipped due to missing critical file
	assert.Equal(t, 1, len(stats.Nodes))
	assert.Equal(t, 1, stats.Nodes[0].ID)
}

func TestNUMACollector_WhitespaceTolerance(t *testing.T) {
	// Test handling of files with extra whitespace
	sysFiles := map[string]string{
		"devices/system/node/node0/numastat": `  numa_hit   1234567890  
  numa_miss   12345  
  
  numa_foreign   54321  `,
		"devices/system/node/node0/cpulist":  "  0-7  \n",
		"devices/system/node/node0/distance": "  10   21  \n",
		"devices/system/node/node1/numastat": validNumaStat,
	}

	collector, _, _ := createTestNUMACollector(t, sysFiles, nil)
	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats := result.(*performance.NUMAStats)
	require.Len(t, stats.Nodes, 2)

	node0 := stats.Nodes[0]
	assert.Equal(t, uint64(1234567890), node0.NumaHit)
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 7}, node0.CPUs)
	assert.Equal(t, []int{10, 21}, node0.Distances)
}
