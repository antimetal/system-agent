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

func createTestDiskInfoCollector(t *testing.T) (*collectors.DiskInfoCollector, string) {
	tmpDir := t.TempDir()
	sysPath := filepath.Join(tmpDir, "sys")

	require.NoError(t, os.MkdirAll(sysPath, 0755))

	config := performance.CollectionConfig{
		HostSysPath: sysPath,
	}

	return collectors.NewDiskInfoCollector(logr.Discard(), config), tmpDir
}

func setupDiskDevice(t *testing.T, blockPath, device string, props map[string]string) {
	devicePath := filepath.Join(blockPath, device)
	require.NoError(t, os.MkdirAll(devicePath, 0755))

	// Create device and queue subdirectories
	require.NoError(t, os.MkdirAll(filepath.Join(devicePath, "device"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(devicePath, "queue"), 0755))

	// Write properties
	for key, value := range props {
		var filePath string
		switch key {
		case "model", "vendor":
			filePath = filepath.Join(devicePath, "device", key)
		case "size", "start":
			filePath = filepath.Join(devicePath, key)
		default:
			filePath = filepath.Join(devicePath, "queue", key)
		}

		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err == nil {
			require.NoError(t, os.WriteFile(filePath, []byte(value), 0644))
		}
	}
}

func TestDiskInfoCollector_Collect(t *testing.T) {
	tests := []struct {
		name     string
		setupSys func(t *testing.T, sysPath string)
		wantInfo func(t *testing.T, disks []performance.DiskInfo)
		wantErr  bool
	}{
		{
			name: "single SSD",
			setupSys: func(t *testing.T, sysPath string) {
				blockPath := filepath.Join(sysPath, "block")
				require.NoError(t, os.MkdirAll(blockPath, 0755))

				setupDiskDevice(t, blockPath, "sda", map[string]string{
					"model":               "Samsung SSD 860",
					"vendor":              "ATA",
					"size":                "1953525168", // ~1TB in 512-byte sectors
					"logical_block_size":  "512",
					"physical_block_size": "4096",
					"rotational":          "0",
					"nr_requests":         "256",
					"scheduler":           "noop deadline [cfq]",
				})
			},
			wantInfo: func(t *testing.T, disks []performance.DiskInfo) {
				assert.Len(t, disks, 1)
				assert.Equal(t, "sda", disks[0].Device)
				assert.Equal(t, "Samsung SSD 860", disks[0].Model)
				assert.Equal(t, "ATA", disks[0].Vendor)
				assert.Equal(t, uint64(1953525168*512), disks[0].SizeBytes)
				assert.Equal(t, uint32(512), disks[0].BlockSize)
				assert.Equal(t, uint32(4096), disks[0].PhysicalBlockSize)
				assert.False(t, disks[0].Rotational)
				assert.Equal(t, uint32(256), disks[0].QueueDepth)
				assert.Equal(t, "cfq", disks[0].Scheduler)
			},
		},
		{
			name: "HDD with partitions",
			setupSys: func(t *testing.T, sysPath string) {
				blockPath := filepath.Join(sysPath, "block")
				require.NoError(t, os.MkdirAll(blockPath, 0755))

				// Main disk
				setupDiskDevice(t, blockPath, "sdb", map[string]string{
					"model":      "WDC WD10EZEX",
					"vendor":     "ATA",
					"size":       "1953525168",
					"rotational": "1",
				})

				// Partitions
				setupDiskDevice(t, blockPath, "sdb/sdb1", map[string]string{
					"size":  "204800", // 100MB
					"start": "2048",
				})
				setupDiskDevice(t, blockPath, "sdb/sdb2", map[string]string{
					"size":  "1953318120", // Rest of disk
					"start": "206848",
				})
			},
			wantInfo: func(t *testing.T, disks []performance.DiskInfo) {
				assert.Len(t, disks, 1)
				assert.Equal(t, "sdb", disks[0].Device)
				assert.Equal(t, "WDC WD10EZEX", disks[0].Model)
				assert.True(t, disks[0].Rotational)

				assert.Len(t, disks[0].Partitions, 2)
				assert.Equal(t, "sdb1", disks[0].Partitions[0].Name)
				assert.Equal(t, uint64(204800*512), disks[0].Partitions[0].SizeBytes)
				assert.Equal(t, uint64(2048), disks[0].Partitions[0].StartSector)

				assert.Equal(t, "sdb2", disks[0].Partitions[1].Name)
				assert.Equal(t, uint64(1953318120*512), disks[0].Partitions[1].SizeBytes)
				assert.Equal(t, uint64(206848), disks[0].Partitions[1].StartSector)
			},
		},
		{
			name: "NVMe device",
			setupSys: func(t *testing.T, sysPath string) {
				blockPath := filepath.Join(sysPath, "block")
				require.NoError(t, os.MkdirAll(blockPath, 0755))

				setupDiskDevice(t, blockPath, "nvme0n1", map[string]string{
					"model":      "Samsung SSD 970 EVO Plus 1TB",
					"size":       "1953525168",
					"rotational": "0",
					"scheduler":  "none",
				})

				// NVMe partition naming
				setupDiskDevice(t, blockPath, "nvme0n1/nvme0n1p1", map[string]string{
					"size":  "1048576",
					"start": "2048",
				})
			},
			wantInfo: func(t *testing.T, disks []performance.DiskInfo) {
				assert.Len(t, disks, 1)
				assert.Equal(t, "nvme0n1", disks[0].Device)
				assert.Contains(t, disks[0].Model, "Samsung SSD 970")
				assert.False(t, disks[0].Rotational)
				assert.Equal(t, "none", disks[0].Scheduler)

				assert.Len(t, disks[0].Partitions, 1)
				assert.Equal(t, "nvme0n1p1", disks[0].Partitions[0].Name)
			},
		},
		{
			name: "multiple disks with loop devices filtered",
			setupSys: func(t *testing.T, sysPath string) {
				blockPath := filepath.Join(sysPath, "block")
				require.NoError(t, os.MkdirAll(blockPath, 0755))

				// Real disks
				setupDiskDevice(t, blockPath, "sda", map[string]string{
					"model": "Disk 1",
					"size":  "1000000",
				})
				setupDiskDevice(t, blockPath, "sdb", map[string]string{
					"model": "Disk 2",
					"size":  "2000000",
				})

				// Loop devices (should be filtered)
				setupDiskDevice(t, blockPath, "loop0", map[string]string{
					"size": "100000",
				})
				setupDiskDevice(t, blockPath, "loop1", map[string]string{
					"size": "200000",
				})

				// Ram disk (should be filtered)
				setupDiskDevice(t, blockPath, "ram0", map[string]string{
					"size": "50000",
				})
			},
			wantInfo: func(t *testing.T, disks []performance.DiskInfo) {
				assert.Len(t, disks, 2)

				// Find disks by device name
				var sda, sdb *performance.DiskInfo
				for i := range disks {
					switch disks[i].Device {
					case "sda":
						sda = &disks[i]
					case "sdb":
						sdb = &disks[i]
					}
				}

				require.NotNil(t, sda)
				require.NotNil(t, sdb)

				assert.Equal(t, "Disk 1", sda.Model)
				assert.Equal(t, uint64(1000000*512), sda.SizeBytes)

				assert.Equal(t, "Disk 2", sdb.Model)
				assert.Equal(t, uint64(2000000*512), sdb.SizeBytes)
			},
		},
		{
			name: "missing block directory",
			setupSys: func(t *testing.T, sysPath string) {
				// Don't create block directory
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, tmpDir := createTestDiskInfoCollector(t)

			if tt.setupSys != nil {
				tt.setupSys(t, filepath.Join(tmpDir, "sys"))
			}

			result, err := collector.Collect(context.Background())

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			disks, ok := result.([]performance.DiskInfo)
			require.True(t, ok, "Expected []performance.DiskInfo, got %T", result)

			if tt.wantInfo != nil {
				tt.wantInfo(t, disks)
			}
		})
	}
}

func TestDiskInfoCollector_PartitionDetectionWithSoftwareRAID(t *testing.T) {
	collector, tmpDir := createTestDiskInfoCollector(t)
	blockPath := filepath.Join(tmpDir, "sys", "block")
	require.NoError(t, os.MkdirAll(blockPath, 0755))

	// Create main disk and its partition (partition should be filtered out)
	setupDiskDevice(t, blockPath, "sda", map[string]string{
		"size": "1000000",
	})
	setupDiskDevice(t, blockPath, "sda1", map[string]string{
		"size": "500000",
	})

	// Create software RAID device (mdadm) that ends with a number but is NOT a partition
	// md0 is a whole block device created by Linux software RAID (mdadm)
	// Despite ending with "0", it should be treated as a whole disk, not filtered as partition
	setupDiskDevice(t, blockPath, "md0", map[string]string{
		"size": "2000000",
	})

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	disks, ok := result.([]performance.DiskInfo)
	require.True(t, ok)

	// Should include: sda (whole disk) and md0 (software RAID device)
	// Should exclude: sda1 (partition of sda)
	assert.Len(t, disks, 2)

	deviceNames := make([]string, len(disks))
	for i, disk := range disks {
		deviceNames[i] = disk.Device
	}

	assert.Contains(t, deviceNames, "sda")     // Physical disk should be included
	assert.Contains(t, deviceNames, "md0")     // Software RAID device should be included
	assert.NotContains(t, deviceNames, "sda1") // Partition should be filtered out
}
