// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

func init() {
	performance.Register(performance.MetricTypeDiskInfo, performance.PartialNewOnceContinuousCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewDiskInfoCollector(logger, config)
		},
	))
}

// DiskInfoCollector collects disk hardware configuration from the Linux sysfs filesystem.
//
// Data Sources and Methodology:
//
// This collector reads block device information from /sys/block/, which is part of the Linux
// sysfs virtual filesystem. Each block device in the system has a directory under /sys/block/
// that contains various attributes and configuration files.
//
// The Linux kernel documentation for sysfs block devices can be found at:
// https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-block
//
// Key data sources:
//
// Device Information:
//   - /sys/block/[device]/device/model      - Device model name (SCSI/ATA)
//   - /sys/block/[device]/device/vendor     - Device vendor/manufacturer
//   - /sys/block/[device]/size              - Device size in 512-byte sectors
//
// Block Size Information:
//   - /sys/block/[device]/queue/logical_block_size  - Logical block size (usually 512 or 4096)
//   - /sys/block/[device]/queue/physical_block_size - Physical block size on device
//
// Performance Characteristics:
//   - /sys/block/[device]/queue/rotational  - "1" for HDD, "0" for SSD/NVMe
//   - /sys/block/[device]/queue/nr_requests - I/O queue depth
//   - /sys/block/[device]/queue/scheduler   - Active I/O scheduler (cfq, noop, bfq, etc.)
//
// Partition Information:
//   - /sys/block/[device]/[partition]/size  - Partition size in 512-byte sectors
//   - /sys/block/[device]/[partition]/start - Starting sector of partition
//
// Device Filtering:
// The collector filters out virtual and temporary devices:
//   - loop devices (loop0, loop1, etc.) - Virtual block devices backed by files
//   - ram devices (ram0, ram1, etc.) - RAM-backed block devices
//   - Partitions (sda1, nvme0n1p1, etc.) - Only collect whole disk information
//
// For more information about Linux block devices and sysfs:
// - Linux kernel sysfs documentation: https://www.kernel.org/doc/html/latest/filesystems/sysfs.html
// - Block layer documentation: https://www.kernel.org/doc/html/latest/block/index.html
// - I/O schedulers: https://www.kernel.org/doc/html/latest/block/bfq-iosched.html
type DiskInfoCollector struct {
	performance.BaseCollector
	blockPath string
}

// Compile-time interface check
var _ performance.PointCollector = (*DiskInfoCollector)(nil)

func NewDiskInfoCollector(logger logr.Logger, config performance.CollectionConfig) (*DiskInfoCollector, error) {
	// Validate paths are absolute
	if !filepath.IsAbs(config.HostSysPath) {
		return nil, fmt.Errorf("HostSysPath must be an absolute path, got: %q", config.HostSysPath)
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    true,
		SupportsContinuous: false,
		RequiresRoot:       false,
		RequiresEBPF:       false,
		MinKernelVersion:   "2.6.0",
	}

	return &DiskInfoCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeDiskInfo,
			"Disk Hardware Info Collector",
			logger,
			config,
			capabilities,
		),
		blockPath: filepath.Join(config.HostSysPath, "block"),
	}, nil
}

func (c *DiskInfoCollector) Collect(ctx context.Context) (any, error) {
	return c.collectDiskInfo()
}

// collectDiskInfo discovers and collects hardware information for all block devices.
//
// This method implements the core discovery logic:
// 1. Lists all entries in /sys/block/ (each represents a block device)
// 2. Filters out virtual/temporary devices (loop, ram) and partitions
// 3. For each real disk, collects hardware properties and partition information
//
// The filtering is necessary because /sys/block/ contains many virtual devices:
// - loop devices: Used for mounting files as block devices
// - ram devices: RAM-backed block devices
// - Partitions: We only want whole disk information, not individual partitions
//
// See: https://www.kernel.org/doc/html/latest/admin-guide/devices.html
func (c *DiskInfoCollector) collectDiskInfo() ([]performance.DiskInfo, error) {
	disks := make([]performance.DiskInfo, 0)

	// List all block devices from /sys/block/
	// Each directory represents a block device known to the kernel
	entries, err := os.ReadDir(c.blockPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read block devices: %w", err)
	}

	for _, entry := range entries {
		// In /sys/block/, entries are typically symlinks to the actual device directories
		// We need to check if the target is a directory, not just if the entry itself is
		deviceName := entry.Name()
		devicePath := filepath.Join(c.blockPath, deviceName)

		// Check if this path (following symlinks) is a directory
		info, err := os.Stat(devicePath)
		if err != nil || !info.IsDir() {
			continue
		}

		// Skip virtual block devices that aren't real hardware
		// loop devices: Virtual devices backed by files (e.g., ISO mounts)
		// ram devices: RAM-backed block devices
		if strings.HasPrefix(deviceName, "loop") || strings.HasPrefix(deviceName, "ram") {
			continue
		}

		// Skip partition entries (e.g., sda1, sda2, nvme0n1p1)
		// We only want whole disk information, partitions are collected separately
		if c.isPartition(deviceName) {
			c.Logger().V(1).Info("Skipping partition", "device", deviceName)
			continue
		}

		disk := performance.DiskInfo{
			Device:     deviceName,
			Partitions: make([]performance.PartitionInfo, 0),
		}

		// Collect disk information
		c.parseDiskProperties(&disk, devicePath)
		c.parsePartitions(&disk, devicePath)

		c.Logger().V(1).Info("Found disk device", "device", deviceName, "size", disk.SizeBytes)
		disks = append(disks, disk)
	}

	return disks, nil
}

// isPartition determines if a device name represents a partition rather than a whole disk.
//
// Linux partition naming conventions:
// - SCSI/SATA disks: sda, sdb → sda1, sda2, sdb1, sdb2
// - NVMe disks: nvme0n1, nvme1n1 → nvme0n1p1, nvme0n1p2, nvme1n1p1
// - IDE disks: hda, hdb → hda1, hda2, hdb1, hdb2
// - Software RAID: md0, md1 → md0p1, md0p2 (but md0 itself is a whole device)
//
// Algorithm:
// 1. Check if device name ends with a digit
// 2. Strip trailing digits to get potential parent device name
// 3. Check if parent device exists in /sys/block/
// 4. If parent exists, this is a partition; otherwise it's a whole device
//
// This handles edge cases like:
// - md0: Software RAID device (whole disk, not partition)
// - sda1: Partition of sda (filtered out)
// - nvme0n1: NVMe device (whole disk)
// - nvme0n1p1: Partition of nvme0n1 (filtered out)
//
// Note: The md0 case is specifically tested in TestDiskInfoCollector_PartitionDetectionWithSoftwareRAID
//
// See: https://www.kernel.org/doc/html/latest/admin-guide/devices.html
func (c *DiskInfoCollector) isPartition(name string) bool {
	if len(name) == 0 {
		return false
	}

	// Check if the name ends with a number (potential partition indicator)
	lastChar := name[len(name)-1]
	if lastChar >= '0' && lastChar <= '9' {
		// Strip trailing digits to get parent device name
		// e.g., "sda1" → "sda", "nvme0n1p1" → "nvme0n1p", "md0" → "md"
		parentName := name
		for i := len(name) - 1; i >= 0 && name[i] >= '0' && name[i] <= '9'; i-- {
			parentName = name[:i]
		}

		// For NVMe devices, also strip the 'p' partition indicator
		// e.g., "nvme0n1p1" → "nvme0n1p" → "nvme0n1"
		parentName = strings.TrimSuffix(parentName, "p")

		// If we found a potential parent name, check if it exists
		if parentName != name && parentName != "" {
			parentPath := filepath.Join(c.blockPath, parentName)
			if _, err := os.Stat(parentPath); err == nil {
				return true // Parent exists, this is a partition
			}
		}
	}
	return false // No parent found or doesn't end with digit
}

// parseDiskProperties reads hardware properties for a disk from sysfs files.
//
// This method reads various sysfs attributes to determine disk characteristics:
//
// Device Information (SCSI/ATA devices):
// - model: Device model string from SCSI/ATA INQUIRY/IDENTIFY
// - vendor: Vendor/manufacturer string
//
// Size Information:
// - size: Total device size in 512-byte sectors (always 512, regardless of actual sector size)
// - logical_block_size: Logical sector size (what the filesystem sees)
// - physical_block_size: Physical sector size (what the hardware uses)
//
// Performance Characteristics:
// - rotational: "1" for traditional spinning disks, "0" for SSDs/NVMe
// - nr_requests: Maximum number of requests in the I/O queue
// - scheduler: Active I/O scheduler algorithm
//
// Notes:
// - Size is always reported in 512-byte sectors for historical reasons
// - Modern drives often have 4096-byte physical sectors but present 512-byte logical sectors
// - NVMe drives may not have device/model files (different sysfs layout)
// - All reads are gracefully handled - missing files result in default/empty values
//
// References:
// - https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-block
// - https://www.kernel.org/doc/html/latest/block/queue-sysfs.html
func (c *DiskInfoCollector) parseDiskProperties(disk *performance.DiskInfo, devicePath string) {
	// Note: May not exist for NVMe devices (different sysfs layout)
	modelPath := filepath.Join(devicePath, "device", "model")
	if data, err := os.ReadFile(modelPath); err == nil {
		disk.Model = strings.TrimSpace(string(data))
	}

	vendorPath := filepath.Join(devicePath, "device", "vendor")
	if data, err := os.ReadFile(vendorPath); err == nil {
		disk.Vendor = strings.TrimSpace(string(data))
	}

	// Note: Always in 512-byte units regardless of physical sector size
	sizePath := filepath.Join(devicePath, "size")
	if data, err := os.ReadFile(sizePath); err == nil {
		if sectors, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
			disk.SizeBytes = sectors * 512
		}
	}

	// Typically 512 or 4096 bytes
	blockSizePath := filepath.Join(devicePath, "queue", "logical_block_size")
	if data, err := os.ReadFile(blockSizePath); err == nil {
		if size, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32); err == nil {
			disk.BlockSize = uint32(size)
		}
	}

	// Modern drives often use 4096-byte physical sectors
	physBlockSizePath := filepath.Join(devicePath, "queue", "physical_block_size")
	if data, err := os.ReadFile(physBlockSizePath); err == nil {
		if size, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32); err == nil {
			disk.PhysicalBlockSize = uint32(size)
		}
	}

	// "1" = traditional spinning disk, "0" = solid state device
	rotationalPath := filepath.Join(devicePath, "queue", "rotational")
	if data, err := os.ReadFile(rotationalPath); err == nil {
		disk.Rotational = strings.TrimSpace(string(data)) == "1"
	}

	// Higher values allow more concurrent I/O operations
	queueDepthPath := filepath.Join(devicePath, "queue", "nr_requests")
	if data, err := os.ReadFile(queueDepthPath); err == nil {
		if depth, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32); err == nil {
			disk.QueueDepth = uint32(depth)
		}
	}

	// Format: "noop deadline [cfq]" where active scheduler is in brackets
	// Common schedulers: noop, deadline, cfq, bfq, mq-deadline, none (for NVMe)
	schedulerPath := filepath.Join(devicePath, "queue", "scheduler")
	if data, err := os.ReadFile(schedulerPath); err == nil {
		schedulerStr := strings.TrimSpace(string(data))
		schedulers := strings.Fields(schedulerStr)
		for _, sched := range schedulers {
			if strings.HasPrefix(sched, "[") && strings.HasSuffix(sched, "]") {
				disk.Scheduler = strings.Trim(sched, "[]")
				break
			}
		}
		// If no bracketed scheduler found, use the whole string
		if disk.Scheduler == "" {
			disk.Scheduler = schedulerStr
		}
	}
}

// parsePartitions discovers and collects information about disk partitions.
//
// Partitions are represented as subdirectories within the parent disk's sysfs directory.
// For example, /sys/block/sda/sda1/ contains information about the first partition of sda.
//
// Partition naming follows these conventions:
// - SCSI/SATA: sda1, sda2, sdb1, etc.
// - NVMe: nvme0n1p1, nvme0n1p2, etc.
// - IDE: hda1, hda2, etc.
// - Software RAID: md0p1, md0p2, etc.
//
// Information collected for each partition:
// - size: Partition size in 512-byte sectors
// - start: Starting sector offset from beginning of disk
//
// This information is useful for:
// - Understanding disk layout and utilization
// - Calculating partition boundaries and free space
// - Identifying partition alignment (important for SSD performance)
//
// Note: The kernel always reports sizes in 512-byte sectors regardless of the
// actual physical sector size of the device.
//
// References:
// - https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-block
// - https://en.wikipedia.org/wiki/Disk_partitioning
func (c *DiskInfoCollector) parsePartitions(disk *performance.DiskInfo, devicePath string) {
	// Look for partition subdirectories within the disk's sysfs directory
	// e.g., /sys/block/sda/sda1/, /sys/block/sda/sda2/
	entries, err := os.ReadDir(devicePath)
	if err != nil {
		return // Gracefully handle missing or inaccessible directory
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue // Skip non-directory entries
		}

		name := entry.Name()
		if strings.HasPrefix(name, disk.Device) && name != disk.Device {
			partition := performance.PartitionInfo{
				Name: name,
			}

			partPath := filepath.Join(devicePath, name)

			// Read partition size in 512-byte sectors
			sizePath := filepath.Join(partPath, "size")
			if data, err := os.ReadFile(sizePath); err == nil {
				if sectors, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
					partition.SizeBytes = sectors * 512
				}
			}

			// Read partition start sector (offset from beginning of disk)
			// Important for understanding partition layout and alignment
			startPath := filepath.Join(partPath, "start")
			if data, err := os.ReadFile(startPath); err == nil {
				if start, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
					partition.StartSector = start
				}
			}

			disk.Partitions = append(disk.Partitions, partition)
		}
	}
}
