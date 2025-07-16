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

// Test data constants for realistic /proc/diskstats content
const (
	// Valid diskstats with various device types
	validDiskstats = `   8       0 sda 1234 567 890123 4567 890 123 456789 1234 0 5678 9012
   8       1 sda1 100 50 20000 100 50 25 10000 50 0 150 200
   8       2 sda2 200 100 40000 200 100 50 20000 100 0 300 400
   8      16 sdb 2345 678 901234 5678 901 234 567890 2345 1 6789 10123
 259       0 nvme0n1 3456 789 1234567 6789 1234 567 890123 3456 2 7890 11234
 259       1 nvme0n1p1 300 150 60000 300 150 75 30000 150 0 450 600
 179       0 mmcblk0 4567 890 2345678 7890 2345 678 901234 4567 3 8901 12345
 179       1 mmcblk0p1 400 200 80000 400 200 100 40000 200 0 600 800
   7       0 loop0 10 0 100 5 0 0 0 0 0 5 5
 253       0 dm-0 1000 200 300000 1500 500 100 200000 800 0 2000 2500`

	// Minimal valid diskstats
	minimalDiskstats = `   8       0 sda 0 0 0 0 0 0 0 0 0 0 0`

	// Edge case with maximum values
	maxValuesDiskstats = `   8       0 sda 18446744073709551615 18446744073709551615 18446744073709551615 18446744073709551615 18446744073709551615 18446744073709551615 18446744073709551615 18446744073709551615 18446744073709551615 18446744073709551615 18446744073709551615`

	// Malformed data scenarios
	malformedShortLine = `   8       0 sda incomplete line`
	malformedBadMajor  = `   not_a_number 0 sda 1234 567 890123 4567 890 123 456789 1234 0 5678 9012`
	malformedBadMinor  = `   8 not_a_number sda 1234 567 890123 4567 890 123 456789 1234 0 5678 9012`
	malformedBadStats  = `   8       0 sda abc def ghi jkl mno pqr stu vwx 0 yz 123`
)

// Helper functions following TCP collector patterns
func createDiskCollector(t *testing.T, procPath string) *collectors.DiskCollector {
	config := performance.CollectionConfig{
		HostProcPath: procPath,
	}
	return collectors.NewDiskCollector(logr.Discard(), config)
}

func setupDiskstatsFile(t *testing.T, content string) string {
	tempDir := t.TempDir()
	diskstatsPath := filepath.Join(tempDir, "diskstats")
	require.NoError(t, os.WriteFile(diskstatsPath, []byte(content), 0644))
	return tempDir
}

func collectDiskStats(t *testing.T, collector *collectors.DiskCollector) []*performance.DiskStats {
	ctx := context.Background()
	result, err := collector.Collect(ctx)
	require.NoError(t, err)

	stats, ok := result.([]*performance.DiskStats)
	require.True(t, ok, "expected []*performance.DiskStats, got %T", result)
	return stats
}

func collectDiskStatsWithError(t *testing.T, collector *collectors.DiskCollector) error {
	ctx := context.Background()
	_, err := collector.Collect(ctx)
	return err
}

// Test basic functionality
func TestDiskCollector_BasicFunctionality(t *testing.T) {
	procPath := setupDiskstatsFile(t, validDiskstats)
	collector := createDiskCollector(t, procPath)

	stats := collectDiskStats(t, collector)

	// Should have 6 whole disks (partitions filtered out)
	assert.Equal(t, 6, len(stats))

	// Create map for easier validation
	diskMap := make(map[string]*performance.DiskStats)
	for _, stat := range stats {
		diskMap[stat.Device] = stat
	}

	// Verify whole disks are included
	for _, device := range []string{"sda", "sdb", "nvme0n1", "mmcblk0", "loop0", "dm-0"} {
		assert.Contains(t, diskMap, device, "device %s should be present", device)
	}

	// Verify partitions are filtered out
	for _, partition := range []string{"sda1", "sda2", "nvme0n1p1", "mmcblk0p1"} {
		assert.NotContains(t, diskMap, partition, "partition %s should be filtered", partition)
	}

	// Validate specific disk values
	sda := diskMap["sda"]
	assert.Equal(t, uint32(8), sda.Major)
	assert.Equal(t, uint32(0), sda.Minor)
	assert.Equal(t, uint64(1234), sda.ReadsCompleted)
	assert.Equal(t, uint64(567), sda.ReadsMerged)
	assert.Equal(t, uint64(890123), sda.SectorsRead)
	assert.Equal(t, uint64(4567), sda.ReadTime)
	assert.Equal(t, uint64(890), sda.WritesCompleted)
	assert.Equal(t, uint64(123), sda.WritesMerged)
	assert.Equal(t, uint64(456789), sda.SectorsWritten)
	assert.Equal(t, uint64(1234), sda.WriteTime)
	assert.Equal(t, uint64(0), sda.IOsInProgress)
	assert.Equal(t, uint64(5678), sda.IOTime)
	assert.Equal(t, uint64(9012), sda.WeightedIOTime)

	// Verify rate fields are zero (not calculated in point collector)
	assert.Equal(t, float64(0), sda.IOPS)
	assert.Equal(t, float64(0), sda.ReadBytesPerSec)
	assert.Equal(t, float64(0), sda.WriteBytesPerSec)
	assert.Equal(t, float64(0), sda.Utilization)
	assert.Equal(t, float64(0), sda.AvgQueueSize)
	assert.Equal(t, float64(0), sda.AvgReadLatency)
	assert.Equal(t, float64(0), sda.AvgWriteLatency)
}

// Test error handling
func TestDiskCollector_ErrorHandling(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func() string
		errorMsg  string
	}{
		{
			name: "missing_diskstats_file",
			setupFunc: func() string {
				return t.TempDir() // Empty directory
			},
			errorMsg: "failed to open",
		},
		{
			name: "unreadable_directory",
			setupFunc: func() string {
				return "/non/existent/path"
			},
			errorMsg: "failed to open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procPath := tt.setupFunc()
			collector := createDiskCollector(t, procPath)
			err := collectDiskStatsWithError(t, collector)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMsg)
		})
	}
}

// Test graceful degradation with malformed data
func TestDiskCollector_GracefulDegradation(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		validateResult func(*testing.T, []*performance.DiskStats)
	}{
		{
			name:    "empty_file",
			content: "",
			validateResult: func(t *testing.T, stats []*performance.DiskStats) {
				assert.Empty(t, stats)
			},
		},
		{
			name:    "only_partitions",
			content: "   8       1 sda1 100 50 20000 100 50 25 10000 50 0 150 200\n   8       2 sda2 200 100 40000 200 100 50 20000 100 0 300 400",
			validateResult: func(t *testing.T, stats []*performance.DiskStats) {
				assert.Empty(t, stats, "all partitions should be filtered")
			},
		},
		{
			name:    "mixed_valid_and_invalid",
			content: validDiskstats + "\n" + malformedShortLine + "\n" + malformedBadMajor,
			validateResult: func(t *testing.T, stats []*performance.DiskStats) {
				// Should still parse the valid lines
				assert.Equal(t, 6, len(stats))
			},
		},
		{
			name:    "short_lines_skipped",
			content: malformedShortLine + "\n" + minimalDiskstats,
			validateResult: func(t *testing.T, stats []*performance.DiskStats) {
				// Should skip short line but parse minimal valid line
				assert.Equal(t, 1, len(stats))
				assert.Equal(t, "sda", stats[0].Device)
			},
		},
		{
			name:    "bad_numeric_fields",
			content: malformedBadStats + "\n" + "   253       0 dm-0 1000 200 300000 1500 500 100 200000 800 0 2000 2500",
			validateResult: func(t *testing.T, stats []*performance.DiskStats) {
				// Should include device with parse errors (zero values) and valid device
				assert.Equal(t, 2, len(stats))

				// Find sda with bad stats
				var sda *performance.DiskStats
				for _, s := range stats {
					if s.Device == "sda" {
						sda = s
						break
					}
				}
				require.NotNil(t, sda)
				// All numeric fields should be zero due to parse errors
				assert.Equal(t, uint64(0), sda.ReadsCompleted)
				assert.Equal(t, uint64(0), sda.SectorsRead)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procPath := setupDiskstatsFile(t, tt.content)
			collector := createDiskCollector(t, procPath)
			stats := collectDiskStats(t, collector)
			tt.validateResult(t, stats)
		})
	}
}

// Test edge cases
func TestDiskCollector_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		content string
		check   func(*testing.T, []*performance.DiskStats)
	}{
		{
			name:    "extreme_values",
			content: maxValuesDiskstats,
			check: func(t *testing.T, stats []*performance.DiskStats) {
				assert.Equal(t, 1, len(stats))
				sda := stats[0]
				// Verify max uint64 values are parsed correctly
				assert.Equal(t, uint64(18446744073709551615), sda.ReadsCompleted)
				assert.Equal(t, uint64(18446744073709551615), sda.SectorsRead)
				assert.Equal(t, uint64(18446744073709551615), sda.WritesCompleted)
			},
		},
		{
			name:    "zero_values",
			content: minimalDiskstats,
			check: func(t *testing.T, stats []*performance.DiskStats) {
				assert.Equal(t, 1, len(stats))
				sda := stats[0]
				assert.Equal(t, uint64(0), sda.ReadsCompleted)
				assert.Equal(t, uint64(0), sda.SectorsRead)
				assert.Equal(t, uint64(0), sda.WritesCompleted)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procPath := setupDiskstatsFile(t, tt.content)
			collector := createDiskCollector(t, procPath)
			stats := collectDiskStats(t, collector)
			tt.check(t, stats)
		})
	}
}

// Test device type detection
func TestDiskCollector_DeviceTypes(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		expectedDevices []string
		filteredDevices []string
	}{
		{
			name: "standard_scsi_devices",
			content: `   8       0 sda 1 2 3 4 5 6 7 8 9 10 11
   8       1 sda1 1 2 3 4 5 6 7 8 9 10 11
   8      16 sdb 1 2 3 4 5 6 7 8 9 10 11
   8      17 sdb1 1 2 3 4 5 6 7 8 9 10 11`,
			expectedDevices: []string{"sda", "sdb"},
			filteredDevices: []string{"sda1", "sdb1"},
		},
		{
			name: "nvme_devices",
			content: ` 259       0 nvme0n1 1 2 3 4 5 6 7 8 9 10 11
 259       1 nvme0n1p1 1 2 3 4 5 6 7 8 9 10 11
 259       2 nvme0n1p2 1 2 3 4 5 6 7 8 9 10 11`,
			expectedDevices: []string{"nvme0n1"},
			filteredDevices: []string{"nvme0n1p1", "nvme0n1p2"},
		},
		{
			name: "mmc_devices",
			content: ` 179       0 mmcblk0 1 2 3 4 5 6 7 8 9 10 11
 179       1 mmcblk0p1 1 2 3 4 5 6 7 8 9 10 11
 179       2 mmcblk0p2 1 2 3 4 5 6 7 8 9 10 11`,
			expectedDevices: []string{"mmcblk0"},
			filteredDevices: []string{"mmcblk0p1", "mmcblk0p2"},
		},
		{
			name: "special_devices",
			content: `   7       0 loop0 1 2 3 4 5 6 7 8 9 10 11
   7       1 loop1 1 2 3 4 5 6 7 8 9 10 11
   7      10 loop10 1 2 3 4 5 6 7 8 9 10 11
 253       0 dm-0 1 2 3 4 5 6 7 8 9 10 11
 253       1 dm-1 1 2 3 4 5 6 7 8 9 10 11`,
			expectedDevices: []string{"loop0", "loop1", "loop10", "dm-0", "dm-1"},
			filteredDevices: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procPath := setupDiskstatsFile(t, tt.content)
			collector := createDiskCollector(t, procPath)
			stats := collectDiskStats(t, collector)

			// Verify expected devices are present
			deviceMap := make(map[string]bool)
			for _, stat := range stats {
				deviceMap[stat.Device] = true
			}

			for _, expected := range tt.expectedDevices {
				assert.True(t, deviceMap[expected], "expected device %s not found", expected)
			}

			// Verify filtered devices are not present
			for _, filtered := range tt.filteredDevices {
				assert.False(t, deviceMap[filtered], "partition %s should be filtered", filtered)
			}

			assert.Equal(t, len(tt.expectedDevices), len(stats))
		})
	}
}

// Test isPartition function directly
func TestIsPartition(t *testing.T) {
	tests := []struct {
		device      string
		isPartition bool
	}{
		// Whole disks - standard
		{"sda", false},
		{"sdb", false},
		{"vda", false},
		{"hda", false},
		{"xvda", false},

		// Whole disks - NVMe
		{"nvme0n1", false},
		{"nvme1n1", false},
		{"nvme10n1", false},

		// Whole disks - MMC
		{"mmcblk0", false},
		{"mmcblk1", false},
		{"mmcblk10", false},

		// Whole disks - special
		{"loop0", false},
		{"loop1", false},
		{"loop10", false},
		{"dm-0", false},
		{"dm-1", false},
		{"dm-10", false},

		// Partitions - standard
		{"sda1", true},
		{"sda2", true},
		{"sdb10", true},
		{"vda1", true},
		{"hda3", true},
		{"xvda1", true},

		// Partitions - NVMe
		{"nvme0n1p1", true},
		{"nvme0n1p2", true},
		{"nvme1n1p10", true},

		// Partitions - MMC
		{"mmcblk0p1", true},
		{"mmcblk0p2", true},
		{"mmcblk1p5", true},

		// Edge cases
		{"", false},
		{"sd", false},
		{"nvme", false},
		{"mmcblk", false},
		{"loopback", false},
		{"dm-", false},
	}

	for _, tt := range tests {
		t.Run(tt.device, func(t *testing.T) {
			result := collectors.IsPartition(tt.device)
			assert.Equal(t, tt.isPartition, result, "device: %s", tt.device)
		})
	}
}
