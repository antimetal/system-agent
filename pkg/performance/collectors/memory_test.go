// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// Valid scenarios
	validMeminfoContent = `MemTotal:        8192000 kB
MemFree:         1024000 kB
MemAvailable:    4096000 kB
Buffers:          256000 kB
Cached:          2048000 kB
SwapCached:       128000 kB
Active:          3072000 kB
Inactive:        2048000 kB
SwapTotal:       4096000 kB
SwapFree:        3072000 kB
Dirty:             16384 kB
Writeback:             0 kB
AnonPages:       1536000 kB
Mapped:           512000 kB
Shmem:            128000 kB
Slab:             256000 kB
SReclaimable:     128000 kB
SUnreclaim:       128000 kB
KernelStack:       16384 kB
PageTables:        32768 kB
CommitLimit:     8192000 kB
Committed_AS:    5120000 kB
VmallocTotal:   34359738367 kB
VmallocUsed:      524288 kB
HugePages_Total:       0
HugePages_Free:        0
Hugepagesize:       2048 kB
`

	partialMeminfoContent = `MemTotal:        8192000 kB
MemFree:         1024000 kB
Buffers:          256000 kB
Cached:          2048000 kB
SwapTotal:       4096000 kB
SwapFree:        3072000 kB
`

	// Edge cases
	zeroValuesMeminfoContent = `MemTotal:        0 kB
MemFree:         0 kB
MemAvailable:    0 kB
Buffers:          0 kB
Cached:          0 kB
SwapTotal:       0 kB
SwapFree:        0 kB
`

	// Boundary conditions
	highMemoryMeminfoContent = `MemTotal:        134217728 kB
MemFree:         67108864 kB
MemAvailable:    100663296 kB
Buffers:         16777216 kB
Cached:          33554432 kB
SwapTotal:       67108864 kB
SwapFree:        33554432 kB
`

	hugePagesMeminfoContent = `MemTotal:        8192000 kB
MemFree:         1024000 kB
MemAvailable:    4096000 kB
Buffers:          256000 kB
Cached:          2048000 kB
SwapTotal:       4096000 kB
SwapFree:        3072000 kB
HugePages_Total:    1024
HugePages_Free:      512
HugePages_Rsvd:      256
HugePages_Surp:        0
Hugepagesize:       2048 kB
Hugetlb:         2097152 kB
DirectMap4k:      524288 kB
DirectMap2M:     7340032 kB
DirectMap1G:           0 kB
`

	// Comprehensive meminfo with all supported fields
	comprehensiveValidMeminfoContent = `MemTotal:        16777216 kB
MemFree:          8388608 kB
MemAvailable:    12582912 kB
Buffers:           524288 kB
Cached:           4194304 kB
SwapCached:        262144 kB
Active:           6291456 kB
Inactive:         4194304 kB
SwapTotal:        8388608 kB
SwapFree:         6291456 kB
Dirty:              32768 kB
Writeback:              0 kB
AnonPages:        3145728 kB
Mapped:           1048576 kB
Shmem:             262144 kB
Slab:              524288 kB
SReclaimable:      262144 kB
SUnreclaim:        262144 kB
KernelStack:        32768 kB
PageTables:         65536 kB
CommitLimit:    16777216 kB
Committed_AS:   10485760 kB
VmallocTotal:   68719476735 kB
VmallocUsed:     1048576 kB
HugePages_Total:    2048
HugePages_Free:     1024
HugePages_Rsvd:      512
HugePages_Surp:        0
Hugepagesize:       2048 kB
Hugetlb:         4194304 kB
`

	// Error conditions
	malformedMeminfoContent = `MemTotal: invalid_value kB
MemFree:         1024000 kB
`

	invalidUnitMeminfoContent = `MemTotal:        8192000 MB
MemFree:         1024000 kB
`

	missingColonMeminfoContent = `MemTotal        8192000 kB
MemFree         1024000 kB
`

	emptyMeminfoContent = ``

	mixedValidInvalidContent = `MemTotal:        8192000 kB
MemFree:         invalid_value kB
MemAvailable:    4096000 kB
Buffers:          256000 kB
`

	// Boundary value test constants
	maxValuesMeminfoContent = `MemTotal:        18446744073709551615 kB
MemFree:         18446744073709551615 kB
MemAvailable:    18446744073709551615 kB
Buffers:         18446744073709551615 kB
Cached:          18446744073709551615 kB
SwapTotal:       18446744073709551615 kB
SwapFree:        18446744073709551615 kB
`

	overflowMeminfoContent = `MemTotal:        18446744073709551616 kB
MemFree:         1024000 kB
MemAvailable:    4096000 kB
Buffers:          256000 kB
`

	largeMemoryMeminfoContent = `MemTotal:        1099511627776 kB
MemFree:         549755813888 kB
MemAvailable:    824633720832 kB
Buffers:         68719476736 kB
Cached:          274877906944 kB
SwapTotal:       549755813888 kB
SwapFree:        274877906944 kB
`

	singleKbValueMeminfoContent = `MemTotal:        1 kB
MemFree:         1 kB
MemAvailable:    1 kB
Buffers:          1 kB
Cached:           1 kB
SwapTotal:        1 kB
SwapFree:         1 kB
`
)

func createMemoryTestCollector(t *testing.T, meminfoContent string) *MemoryCollector {
	tmpDir := t.TempDir()

	meminfoPath := filepath.Join(tmpDir, "meminfo")
	err := os.WriteFile(meminfoPath, []byte(meminfoContent), 0644)
	require.NoError(t, err)

	config := performance.CollectionConfig{
		HostProcPath: tmpDir,
		HostSysPath:  tmpDir,
	}
	collector, err := NewMemoryCollector(logr.Discard(), config)
	require.NoError(t, err)
	return collector
}

func createMemoryTestCollectorWithoutFile(t *testing.T) *MemoryCollector {
	tmpDir := t.TempDir()

	config := performance.CollectionConfig{
		HostProcPath: tmpDir,
		HostSysPath:  tmpDir,
	}
	collector, err := NewMemoryCollector(logr.Discard(), config)
	require.NoError(t, err)
	return collector
}

func validateMemoryCollectorInterface(t *testing.T, collector interface{}) {
	_, ok := collector.(performance.Collector)
	require.True(t, ok, "collector must implement Collector interface")
}

func validateMemoryStats(t *testing.T, stats *performance.MemoryStats, expected *performance.MemoryStats) {
	assert.Equal(t, expected.MemTotal, stats.MemTotal)
	assert.Equal(t, expected.MemFree, stats.MemFree)
	assert.Equal(t, expected.MemAvailable, stats.MemAvailable)
	assert.Equal(t, expected.Buffers, stats.Buffers)
	assert.Equal(t, expected.Cached, stats.Cached)
	assert.Equal(t, expected.SwapCached, stats.SwapCached)
	assert.Equal(t, expected.Active, stats.Active)
	assert.Equal(t, expected.Inactive, stats.Inactive)
	assert.Equal(t, expected.SwapTotal, stats.SwapTotal)
	assert.Equal(t, expected.SwapFree, stats.SwapFree)
	assert.Equal(t, expected.Dirty, stats.Dirty)
	assert.Equal(t, expected.Writeback, stats.Writeback)
	assert.Equal(t, expected.AnonPages, stats.AnonPages)
	assert.Equal(t, expected.Mapped, stats.Mapped)
	assert.Equal(t, expected.Shmem, stats.Shmem)
	assert.Equal(t, expected.Slab, stats.Slab)
	assert.Equal(t, expected.SReclaimable, stats.SReclaimable)
	assert.Equal(t, expected.SUnreclaim, stats.SUnreclaim)
	assert.Equal(t, expected.KernelStack, stats.KernelStack)
	assert.Equal(t, expected.PageTables, stats.PageTables)
	assert.Equal(t, expected.CommitLimit, stats.CommitLimit)
	assert.Equal(t, expected.CommittedAS, stats.CommittedAS)
	assert.Equal(t, expected.VmallocTotal, stats.VmallocTotal)
	assert.Equal(t, expected.VmallocUsed, stats.VmallocUsed)
	assert.Equal(t, expected.HugePages_Total, stats.HugePages_Total)
	assert.Equal(t, expected.HugePages_Free, stats.HugePages_Free)
	assert.Equal(t, expected.HugePages_Rsvd, stats.HugePages_Rsvd)
	assert.Equal(t, expected.HugePages_Surp, stats.HugePages_Surp)
	assert.Equal(t, expected.HugePagesize, stats.HugePagesize)
	assert.Equal(t, expected.Hugetlb, stats.Hugetlb)
}

func collectAndValidateMemory(t *testing.T, collector *MemoryCollector, wantErr bool) *performance.MemoryStats {
	result, err := collector.Collect(context.Background())

	if wantErr {
		assert.Error(t, err)
		return nil
	}

	require.NoError(t, err)
	stats, ok := result.(*performance.MemoryStats)
	require.True(t, ok)
	return stats
}

func TestMemoryCollector_Constructor(t *testing.T) {
	tests := []struct {
		name                string
		hostProcPath        string
		expectedMeminfoPath string
		wantErr             bool
	}{
		{"default proc path", "/proc", "/proc/meminfo", false},
		{"custom proc path", "/custom/proc", "/custom/proc/meminfo", false},
		{"relative proc path", "proc", "", true},
		{"empty proc path", "", "meminfo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := performance.CollectionConfig{
				HostProcPath: tt.hostProcPath,
				HostSysPath:  "/sys",
			}
			collector, err := NewMemoryCollector(logr.Discard(), config)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, collector)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, collector)
			validateMemoryCollectorInterface(t, collector)
			assert.Equal(t, tt.expectedMeminfoPath, collector.meminfoPath)
		})
	}
}

func TestMemoryCollector_AllFields(t *testing.T) {
	collector := createMemoryTestCollector(t, comprehensiveValidMeminfoContent)
	stats := collectAndValidateMemory(t, collector, false)

	// Test all 26 supported fields with proper kB to bytes conversion
	expectedFields := map[string]struct {
		got      uint64
		expected uint64
		name     string
	}{
		"MemTotal":     {stats.MemTotal, 16777216 * 1024, "MemTotal"},
		"MemFree":      {stats.MemFree, 8388608 * 1024, "MemFree"},
		"MemAvailable": {stats.MemAvailable, 12582912 * 1024, "MemAvailable"},
		"Buffers":      {stats.Buffers, 524288 * 1024, "Buffers"},
		"Cached":       {stats.Cached, 4194304 * 1024, "Cached"},
		"SwapCached":   {stats.SwapCached, 262144 * 1024, "SwapCached"},
		"Active":       {stats.Active, 6291456 * 1024, "Active"},
		"Inactive":     {stats.Inactive, 4194304 * 1024, "Inactive"},
		"SwapTotal":    {stats.SwapTotal, 8388608 * 1024, "SwapTotal"},
		"SwapFree":     {stats.SwapFree, 6291456 * 1024, "SwapFree"},
		"Dirty":        {stats.Dirty, 32768 * 1024, "Dirty"},
		"Writeback":    {stats.Writeback, 0, "Writeback"},
		"AnonPages":    {stats.AnonPages, 3145728 * 1024, "AnonPages"},
		"Mapped":       {stats.Mapped, 1048576 * 1024, "Mapped"},
		"Shmem":        {stats.Shmem, 262144 * 1024, "Shmem"},
		"Slab":         {stats.Slab, 524288 * 1024, "Slab"},
		"SReclaimable": {stats.SReclaimable, 262144 * 1024, "SReclaimable"},
		"SUnreclaim":   {stats.SUnreclaim, 262144 * 1024, "SUnreclaim"},
		"KernelStack":  {stats.KernelStack, 32768 * 1024, "KernelStack"},
		"PageTables":   {stats.PageTables, 65536 * 1024, "PageTables"},
		"CommitLimit":  {stats.CommitLimit, 16777216 * 1024, "CommitLimit"},
		"CommittedAS":  {stats.CommittedAS, 10485760 * 1024, "CommittedAS"},
		"VmallocTotal": {stats.VmallocTotal, 68719476735 * 1024, "VmallocTotal"},
		"VmallocUsed":  {stats.VmallocUsed, 1048576 * 1024, "VmallocUsed"},
		// HugePages fields are converted from counts to bytes using HugePagesize
		"HugePages_Total": {stats.HugePages_Total, 2048 * (2048 * 1024), "HugePages_Total"}, // 2048 pages * 2048 kB * 1024 bytes/kB
		"HugePages_Free":  {stats.HugePages_Free, 1024 * (2048 * 1024), "HugePages_Free"},   // 1024 pages * 2048 kB * 1024 bytes/kB
		"HugePages_Rsvd":  {stats.HugePages_Rsvd, 512 * (2048 * 1024), "HugePages_Rsvd"},    // 512 pages * 2048 kB * 1024 bytes/kB
		"HugePages_Surp":  {stats.HugePages_Surp, 0 * (2048 * 1024), "HugePages_Surp"},      // 0 pages * 2048 kB * 1024 bytes/kB
		"HugePagesize":    {stats.HugePagesize, 2048 * 1024, "HugePagesize"},                // 2048 kB * 1024 bytes/kB
		"Hugetlb":         {stats.Hugetlb, 4194304 * 1024, "Hugetlb"},                       // 4194304 kB * 1024 bytes/kB
	}

	// Validate each field
	for _, expected := range expectedFields {
		if expected.got != expected.expected {
			t.Errorf("%s: got %d bytes, expected %d bytes (conversion from kB failed)",
				expected.name, expected.got, expected.expected)
		}
	}

	// Verify we're testing all 26+ supported fields
	totalFields := len(expectedFields)
	if totalFields < 26 {
		t.Errorf("Expected to test at least 26 memory fields, but only tested %d", totalFields)
	}

	t.Logf("Successfully validated %d memory fields with proper kB to bytes conversion", totalFields)
}

func TestMemoryCollector_Comprehensive(t *testing.T) {
	tests := []struct {
		name           string
		meminfoContent string
		createFile     bool
		wantErr        bool
		expectedErr    string
		expected       *performance.MemoryStats
	}{
		// Valid parsing cases
		{
			name:           "valid complete meminfo",
			meminfoContent: validMeminfoContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemTotal:        8192000 * 1024,
				MemFree:         1024000 * 1024,
				MemAvailable:    4096000 * 1024,
				Buffers:         256000 * 1024,
				Cached:          2048000 * 1024,
				SwapCached:      128000 * 1024,
				Active:          3072000 * 1024,
				Inactive:        2048000 * 1024,
				SwapTotal:       4096000 * 1024,
				SwapFree:        3072000 * 1024,
				Dirty:           16384 * 1024,
				Writeback:       0,
				AnonPages:       1536000 * 1024,
				Mapped:          512000 * 1024,
				Shmem:           128000 * 1024,
				Slab:            256000 * 1024,
				SReclaimable:    128000 * 1024,
				SUnreclaim:      128000 * 1024,
				KernelStack:     16384 * 1024,
				PageTables:      32768 * 1024,
				CommitLimit:     8192000 * 1024,
				CommittedAS:     5120000 * 1024,
				VmallocTotal:    34359738367 * 1024,
				VmallocUsed:     524288 * 1024,
				HugePages_Total: 0,
				HugePages_Free:  0,
				HugePagesize:    2048 * 1024,
			},
		},
		{
			name:           "huge pages data",
			meminfoContent: hugePagesMeminfoContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemTotal:        8192000 * 1024,
				MemFree:         1024000 * 1024,
				MemAvailable:    4096000 * 1024,
				Buffers:         256000 * 1024,
				Cached:          2048000 * 1024,
				SwapTotal:       4096000 * 1024,
				SwapFree:        3072000 * 1024,
				HugePages_Total: 1024 * (2048 * 1024), // 1024 pages * 2048 kB per page * 1024 bytes per kB
				HugePages_Free:  512 * (2048 * 1024),  // 512 pages * 2048 kB per page * 1024 bytes per kB
				HugePages_Rsvd:  256 * (2048 * 1024),  // 256 pages * 2048 kB per page * 1024 bytes per kB
				HugePages_Surp:  0 * (2048 * 1024),    // 0 pages * 2048 kB per page * 1024 bytes per kB
				HugePagesize:    2048 * 1024,          // 2048 kB * 1024 bytes per kB
				Hugetlb:         2097152 * 1024,       // 2097152 kB * 1024 bytes per kB
			},
		},
		{
			name:           "partial data (graceful degradation)",
			meminfoContent: partialMeminfoContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemTotal:  8192000 * 1024,
				MemFree:   1024000 * 1024,
				Buffers:   256000 * 1024,
				Cached:    2048000 * 1024,
				SwapTotal: 4096000 * 1024,
				SwapFree:  3072000 * 1024,
			},
		},
		// Boundary value cases
		{
			name:           "maximum uint64 values",
			meminfoContent: maxValuesMeminfoContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemTotal:     ^uint64(0) & ^uint64(1023), // Max value rounded down to nearest KB boundary in bytes
				MemFree:      ^uint64(0) & ^uint64(1023),
				MemAvailable: ^uint64(0) & ^uint64(1023),
				Buffers:      ^uint64(0) & ^uint64(1023),
				Cached:       ^uint64(0) & ^uint64(1023),
				SwapTotal:    ^uint64(0) & ^uint64(1023),
				SwapFree:     ^uint64(0) & ^uint64(1023),
			},
		},
		{
			name:           "single kB values",
			meminfoContent: singleKbValueMeminfoContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemTotal:     1024,
				MemFree:      1024,
				MemAvailable: 1024,
				Buffers:      1024,
				Cached:       1024,
				SwapTotal:    1024,
				SwapFree:     1024,
			},
		},
		{
			name:           "large memory system (1TB total)",
			meminfoContent: largeMemoryMeminfoContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemTotal:     1099511627776 * 1024, // 1TB in bytes
				MemFree:      549755813888 * 1024,  // 512GB in bytes
				MemAvailable: 824633720832 * 1024,  // 768GB in bytes
				Buffers:      68719476736 * 1024,   // 64GB in bytes
				Cached:       274877906944 * 1024,  // 256GB in bytes
				SwapTotal:    549755813888 * 1024,  // 512GB in bytes
				SwapFree:     274877906944 * 1024,  // 256GB in bytes
			},
		},
		{
			name:           "zero values (minimal system)",
			meminfoContent: zeroValuesMeminfoContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemTotal:     0,
				MemFree:      0,
				MemAvailable: 0,
				Buffers:      0,
				Cached:       0,
				SwapTotal:    0,
				SwapFree:     0,
			},
		},
		{
			name:           "high memory values",
			meminfoContent: highMemoryMeminfoContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemTotal:     134217728 * 1024,
				MemFree:      67108864 * 1024,
				MemAvailable: 100663296 * 1024,
				Buffers:      16777216 * 1024,
				Cached:       33554432 * 1024,
				SwapTotal:    67108864 * 1024,
				SwapFree:     33554432 * 1024,
			},
		},
		// Graceful degradation cases (collector continues on parse errors)
		{
			name:           "malformed numeric values (graceful degradation)",
			meminfoContent: malformedMeminfoContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemFree: 1024000 * 1024, // Only valid line is parsed
			},
		},
		{
			name:           "invalid units (graceful degradation)",
			meminfoContent: invalidUnitMeminfoContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemTotal: 8192000 * 1024, // Parses number, ignores invalid unit
				MemFree:  1024000 * 1024, // Valid line is parsed
			},
		},
		{
			name:           "missing colon separator (graceful degradation)",
			meminfoContent: missingColonMeminfoContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemTotal: 8192000 * 1024, // Parses "MemTotal 8192000 kB" without colon
				MemFree:  1024000 * 1024, // Parses "MemFree 1024000 kB" without colon
			},
		},
		{
			name:           "mixed valid and invalid lines (graceful degradation)",
			meminfoContent: mixedValidInvalidContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemTotal:     8192000 * 1024, // Valid lines are parsed
				MemAvailable: 4096000 * 1024,
				Buffers:      256000 * 1024,
			},
		},
		{
			name:           "overflow value handling (graceful degradation)",
			meminfoContent: overflowMeminfoContent,
			createFile:     true,
			expected: &performance.MemoryStats{
				MemFree:      1024000 * 1024,
				MemAvailable: 4096000 * 1024,
				Buffers:      256000 * 1024,
			},
		},
		{
			name:           "empty file",
			meminfoContent: emptyMeminfoContent,
			createFile:     true,
			expected:       &performance.MemoryStats{},
		},
		// Error cases
		{
			name:        "missing file",
			createFile:  false,
			wantErr:     true,
			expectedErr: "failed to open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var collector *MemoryCollector
			if tt.createFile {
				collector = createMemoryTestCollector(t, tt.meminfoContent)
			} else {
				collector = createMemoryTestCollectorWithoutFile(t)
			}

			validateMemoryCollectorInterface(t, collector)
			stats := collectAndValidateMemory(t, collector, tt.wantErr)

			if tt.wantErr {
				return
			}

			if tt.expected != nil {
				validateMemoryStats(t, stats, tt.expected)
			}
		})
	}
}
