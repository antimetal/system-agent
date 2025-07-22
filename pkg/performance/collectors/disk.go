// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// Compile-time interface check
var _ performance.PointCollector = (*DiskCollector)(nil)

const (
	// diskstatsFieldCount is the expected number of fields in /proc/diskstats
	diskstatsFieldCount = 14
)

// DiskCollector collects disk I/O statistics from /proc/diskstats
//
// This collector reads the kernel's disk statistics interface to provide raw counter values:
// - Read/write operations completed
// - Sectors read/written
// - Time spent on I/O operations
// - Queue statistics
//
// Only whole disk devices are reported; partitions are filtered out.
// All values are cumulative counters since system boot.
type DiskCollector struct {
	performance.BaseCollector
	diskstatsPath string
}

func NewDiskCollector(logger logr.Logger, config performance.CollectionConfig) (*DiskCollector, error) {
	// Validate paths are absolute
	if !filepath.IsAbs(config.HostProcPath) {
		return nil, fmt.Errorf("HostProcPath must be an absolute path, got: %q", config.HostProcPath)
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    true,
		SupportsContinuous: false,
		RequiresRoot:       false,
		RequiresEBPF:       false,
		MinKernelVersion:   "2.6.0", // /proc/diskstats has been around since 2.6
	}

	return &DiskCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeDisk,
			"Disk I/O Statistics Collector",
			logger,
			config,
			capabilities,
		),
		diskstatsPath: filepath.Join(config.HostProcPath, "diskstats"),
	}, nil
}

// Collect performs a one-shot collection of disk statistics
func (c *DiskCollector) Collect(ctx context.Context) (any, error) {
	stats, err := c.collectDiskStats()
	if err != nil {
		return nil, fmt.Errorf("failed to collect disk stats: %w", err)
	}

	c.Logger().V(1).Info("Collected disk statistics", "devices", len(stats))
	return stats, nil
}

// collectDiskStats reads and parses /proc/diskstats
//
// Format: major minor device reads... writes... ios_in_progress io_time weighted_io_time
// Fields 4-14 are I/O statistics. Sectors are 512 bytes, times in milliseconds.
//
// Reference: https://www.kernel.org/doc/Documentation/iostats.txt
func (c *DiskCollector) collectDiskStats() ([]*performance.DiskStats, error) {
	file, err := os.Open(c.diskstatsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", c.diskstatsPath, err)
	}
	defer file.Close()

	var diskStats []*performance.DiskStats
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)

		// /proc/diskstats has 14 fields (3 identification + 11 metrics)
		if len(fields) < diskstatsFieldCount {
			continue
		}

		// Parse fields
		major, err := strconv.ParseUint(fields[0], 10, 32)
		if err != nil {
			c.Logger().V(1).Info("Failed to parse disk major number",
				"line", line, "error", err)
			continue
		}

		minor, err := strconv.ParseUint(fields[1], 10, 32)
		if err != nil {
			c.Logger().V(1).Info("Failed to parse disk minor number",
				"line", line, "error", err)
			continue
		}

		device := fields[2]

		// Skip partitions - only include whole disks
		if IsPartition(device) {
			continue
		}

		stats := &performance.DiskStats{
			Device: device,
			Major:  uint32(major),
			Minor:  uint32(minor),
		}

		// Parse read statistics (fields 4-7)
		parseErrors := false

		if stats.ReadsCompleted, err = strconv.ParseUint(fields[3], 10, 64); err != nil {
			parseErrors = true
		}
		if stats.ReadsMerged, err = strconv.ParseUint(fields[4], 10, 64); err != nil {
			parseErrors = true
		}
		if stats.SectorsRead, err = strconv.ParseUint(fields[5], 10, 64); err != nil {
			parseErrors = true
		}
		if stats.ReadTime, err = strconv.ParseUint(fields[6], 10, 64); err != nil {
			parseErrors = true
		}

		// Parse write statistics (fields 8-11)
		if stats.WritesCompleted, err = strconv.ParseUint(fields[7], 10, 64); err != nil {
			parseErrors = true
		}
		if stats.WritesMerged, err = strconv.ParseUint(fields[8], 10, 64); err != nil {
			parseErrors = true
		}
		if stats.SectorsWritten, err = strconv.ParseUint(fields[9], 10, 64); err != nil {
			parseErrors = true
		}
		if stats.WriteTime, err = strconv.ParseUint(fields[10], 10, 64); err != nil {
			parseErrors = true
		}

		// Parse I/O queue statistics (fields 12-14)
		if stats.IOsInProgress, err = strconv.ParseUint(fields[11], 10, 64); err != nil {
			parseErrors = true
		}
		if stats.IOTime, err = strconv.ParseUint(fields[12], 10, 64); err != nil {
			parseErrors = true
		}
		if stats.WeightedIOTime, err = strconv.ParseUint(fields[13], 10, 64); err != nil {
			parseErrors = true
		}

		if parseErrors {
			c.Logger().V(2).Info("Parse errors in disk statistics",
				"device", device, "line", line)
		}

		diskStats = append(diskStats, stats)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading %s: %w", c.diskstatsPath, err)
	}

	c.Logger().V(1).Info("Collected disk statistics", "devices", len(diskStats))
	return diskStats, nil
}

// IsPartition checks if a device name represents a partition
//
// Partitions are identified by:
// - Standard devices: end with a digit (e.g., sda1, vdb2)
// - NVMe devices: contain 'pN' suffix (e.g., nvme0n1p1)
// - MMC devices: contain 'pN' suffix (e.g., mmcblk0p1)
//
// Special cases:
// - loop devices (loop0, loop1) are whole devices, not partitions
// - device mapper devices (dm-0, dm-1) are whole devices, not partitions
func IsPartition(device string) bool {
	if device == "" {
		return false
	}

	// Special whole devices that end with digits
	if strings.HasPrefix(device, "loop") || strings.HasPrefix(device, "dm-") {
		return false
	}

	// NVMe and MMC devices use 'p' before partition number
	if strings.Contains(device, "nvme") || strings.Contains(device, "mmcblk") {
		// Look for 'p' followed by digits at the end
		idx := strings.LastIndex(device, "p")
		if idx > 0 && idx < len(device)-1 {
			// Check if everything after 'p' is digits
			partNum := device[idx+1:]
			for _, ch := range partNum {
				if ch < '0' || ch > '9' {
					return false
				}
			}
			return true
		}
		return false
	}

	// Standard devices: partition if ends with digit
	lastChar := device[len(device)-1]
	return lastChar >= '0' && lastChar <= '9'
}
