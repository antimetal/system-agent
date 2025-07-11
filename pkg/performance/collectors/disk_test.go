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
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiskCollector(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create test diskstats file
	diskstatsPath := filepath.Join(tmpDir, "diskstats")
	diskstatsContent := `   8       0 sda 1234 567 890123 4567 890 123 456789 1234 0 5678 9012
   8       1 sda1 100 50 20000 100 50 25 10000 50 0 150 200
   8       2 sda2 200 100 40000 200 100 50 20000 100 0 300 400
   8      16 sdb 2345 678 901234 5678 901 234 567890 2345 1 6789 10123
 259       0 nvme0n1 3456 789 1234567 6789 1234 567 890123 3456 2 7890 11234
 259       1 nvme0n1p1 300 150 60000 300 150 75 30000 150 0 450 600
 179       0 mmcblk0 4567 890 2345678 7890 2345 678 901234 4567 3 8901 12345
 179       1 mmcblk0p1 400 200 80000 400 200 100 40000 200 0 600 800`

	err := os.WriteFile(diskstatsPath, []byte(diskstatsContent), 0644)
	require.NoError(t, err)

	// Create collector with test config
	config := performance.CollectionConfig{
		HostProcPath: tmpDir,
		Interval:     time.Second,
	}

	logger := logr.Discard()
	collector := NewDiskCollector(logger, config)

	// Test capabilities
	caps := collector.Capabilities()
	assert.True(t, caps.SupportsOneShot)
	assert.True(t, caps.SupportsContinuous)
	assert.False(t, caps.RequiresRoot)
	assert.False(t, caps.RequiresEBPF)

	// First collection
	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats, ok := result.([]*performance.DiskStats)
	require.True(t, ok)

	// Should have 4 disks (sda, sdb, nvme0n1, mmcblk0) - partitions filtered out
	assert.Len(t, stats, 4)

	// Verify disk names
	diskNames := make(map[string]bool)
	for _, disk := range stats {
		diskNames[disk.Device] = true
	}
	assert.True(t, diskNames["sda"])
	assert.True(t, diskNames["sdb"])
	assert.True(t, diskNames["nvme0n1"])
	assert.True(t, diskNames["mmcblk0"])

	// Verify first disk stats
	var sdaStats *performance.DiskStats
	for _, disk := range stats {
		if disk.Device == "sda" {
			sdaStats = disk
			break
		}
	}
	require.NotNil(t, sdaStats)

	assert.Equal(t, uint32(8), sdaStats.Major)
	assert.Equal(t, uint32(0), sdaStats.Minor)
	assert.Equal(t, uint64(1234), sdaStats.ReadsCompleted)
	assert.Equal(t, uint64(567), sdaStats.ReadsMerged)
	assert.Equal(t, uint64(890123), sdaStats.SectorsRead)
	assert.Equal(t, uint64(4567), sdaStats.ReadTime)
	assert.Equal(t, uint64(890), sdaStats.WritesCompleted)
	assert.Equal(t, uint64(123), sdaStats.WritesMerged)
	assert.Equal(t, uint64(456789), sdaStats.SectorsWritten)
	assert.Equal(t, uint64(1234), sdaStats.WriteTime)
	assert.Equal(t, uint64(0), sdaStats.IOsInProgress)
	assert.Equal(t, uint64(5678), sdaStats.IOTime)
	assert.Equal(t, uint64(9012), sdaStats.WeightedIOTime)

	// First collection should have zero derived metrics
	assert.Equal(t, float64(0), sdaStats.IOPS)
	assert.Equal(t, float64(0), sdaStats.ReadBytesPerSec)
	assert.Equal(t, float64(0), sdaStats.WriteBytesPerSec)
	assert.Equal(t, float64(0), sdaStats.Utilization)

	// Update diskstats to simulate activity
	diskstatsContent2 := `   8       0 sda 2234 667 990123 5567 990 223 556789 2234 0 6678 10012
   8       1 sda1 200 150 40000 200 150 75 30000 150 0 450 600
   8       2 sda2 400 200 80000 400 200 100 40000 200 0 600 800
   8      16 sdb 3345 778 1001234 6678 1001 334 667890 3345 1 7789 11123
 259       0 nvme0n1 4456 889 1334567 7789 1334 667 990123 4456 2 8890 12234
 259       1 nvme0n1p1 600 300 120000 600 300 150 60000 300 0 900 1200
 179       0 mmcblk0 5567 990 2445678 8890 2445 778 1001234 5567 3 9901 13345
 179       1 mmcblk0p1 800 400 160000 800 400 200 80000 400 0 1200 1600`

	err = os.WriteFile(diskstatsPath, []byte(diskstatsContent2), 0644)
	require.NoError(t, err)

	// Wait a bit to have a time interval
	time.Sleep(100 * time.Millisecond)

	// Second collection should calculate derived metrics
	result2, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats2, ok := result2.([]*performance.DiskStats)
	require.True(t, ok)
	assert.Len(t, stats2, 4)

	// Find sda in second collection
	var sdaStats2 *performance.DiskStats
	for _, disk := range stats2 {
		if disk.Device == "sda" {
			sdaStats2 = disk
			break
		}
	}
	require.NotNil(t, sdaStats2)

	// Verify derived metrics are calculated
	assert.Greater(t, sdaStats2.IOPS, float64(0))
	assert.Greater(t, sdaStats2.ReadBytesPerSec, float64(0))
	assert.Greater(t, sdaStats2.WriteBytesPerSec, float64(0))
	assert.Greater(t, sdaStats2.Utilization, float64(0))
	assert.LessOrEqual(t, sdaStats2.Utilization, float64(100))
}

func TestIsPartition(t *testing.T) {
	tests := []struct {
		device      string
		isPartition bool
	}{
		// Whole disks
		{"sda", false},
		{"sdb", false},
		{"vda", false},
		{"hda", false},
		{"nvme0n1", false},
		{"nvme1n1", false},
		{"mmcblk0", false},
		{"mmcblk1", false},

		// Partitions
		{"sda1", true},
		{"sda2", true},
		{"sdb10", true},
		{"vda1", true},
		{"hda3", true},
		{"nvme0n1p1", true},
		{"nvme0n1p2", true},
		{"nvme1n1p10", true},
		{"mmcblk0p1", true},
		{"mmcblk0p2", true},
		{"mmcblk1p5", true},

		// Edge cases
		{"", false},
		{"sd", false},
		{"loop0", false},
		{"loop1", false},
		{"dm-0", false},
		{"dm-1", false},
	}

	for _, tt := range tests {
		t.Run(tt.device, func(t *testing.T) {
			result := isPartition(tt.device)
			assert.Equal(t, tt.isPartition, result, "device: %s", tt.device)
		})
	}
}

func TestDiskCollectorWithMissingFile(t *testing.T) {
	// Create collector with non-existent path
	config := performance.CollectionConfig{
		HostProcPath: "/non/existent/path",
		Interval:     time.Second,
	}

	logger := logr.Discard()
	collector := NewDiskCollector(logger, config)

	// Collection should fail
	_, err := collector.Collect(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open")
}

func TestDiskCollectorWithMalformedData(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create test diskstats file with malformed data
	diskstatsPath := filepath.Join(tmpDir, "diskstats")
	diskstatsContent := `   8       0 sda incomplete line
   8       1 sda1 100 50 20000 100 50 25 10000 50 0 150 200
   not_a_number 2 sda2 200 100 40000 200 100 50 20000 100 0 300 400
   8      16 sdb 2345 678 901234 5678 901 234 567890 2345 1 6789 10123`

	err := os.WriteFile(diskstatsPath, []byte(diskstatsContent), 0644)
	require.NoError(t, err)

	// Create collector with test config
	config := performance.CollectionConfig{
		HostProcPath: tmpDir,
		Interval:     time.Second,
	}

	logger := logr.Discard()
	collector := NewDiskCollector(logger, config)

	// Collection should succeed but skip malformed lines
	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats, ok := result.([]*performance.DiskStats)
	require.True(t, ok)

	// Should only have sdb (sda1 is a partition, others are malformed)
	assert.Len(t, stats, 1)
	assert.Equal(t, "sdb", stats[0].Device)
}
