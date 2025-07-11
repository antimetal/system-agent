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
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

const (
	// sectorSize is the standard sector size in bytes for disk I/O calculations
	sectorSize = 512

	// msPerSecond is milliseconds per second for time conversions
	msPerSecond = 1000

	// percentMultiplier converts fractions to percentages
	percentMultiplier = 100

	// diskstatsFieldCount is the expected number of fields in /proc/diskstats
	diskstatsFieldCount = 14
)

// DiskCollector collects disk I/O statistics from /proc/diskstats
//
// This collector reads the kernel's disk statistics interface to provide:
// - I/O operations per second (IOPS)
// - Read/write throughput (bytes/sec)
// - Disk utilization percentage
// - Average I/O latencies
// - Queue size metrics
//
// The collector maintains previous readings to calculate rate-based metrics.
// Only whole disk devices are reported; partitions are filtered out.
type DiskCollector struct {
	performance.BaseCollector
	diskstatsPath string
	prevStats     map[string]*performance.DiskStats // Previous readings for delta calculations
	prevTime      time.Time                         // Time of previous collection
	mu            sync.Mutex                        // Protects prevStats and prevTime
}

func NewDiskCollector(logger logr.Logger, config performance.CollectionConfig) *DiskCollector {
	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    true,
		SupportsContinuous: true,
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
		prevStats:     make(map[string]*performance.DiskStats),
	}
}

func (c *DiskCollector) Collect(ctx context.Context) (any, error) {
	return c.collectDiskStats(ctx)
}

// collectDiskStats reads and parses /proc/diskstats
//
// Format: major minor device reads... writes... ios_in_progress io_time weighted_io_time
// Fields 4-14 are I/O statistics. Sectors are 512 bytes, times in milliseconds.
//
// Reference: https://www.kernel.org/doc/Documentation/iostats.txt
func (c *DiskCollector) collectDiskStats(ctx context.Context) ([]*performance.DiskStats, error) {
	file, err := os.Open(c.diskstatsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", c.diskstatsPath, err)
	}
	defer file.Close()

	currentTime := time.Now()

	// Read file contents first, before acquiring lock
	var diskStats []*performance.DiskStats
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := scanner.Text()
		fields := strings.Fields(line)

		// /proc/diskstats has 14 fields (3 identification + 11 metrics)
		if len(fields) < diskstatsFieldCount {
			continue
		}

		// Parse fields
		major, err := strconv.ParseUint(fields[0], 10, 32)
		if err != nil {
			c.Logger().V(2).Info("Failed to parse disk major number",
				"line", line, "error", err)
			continue
		}

		minor, err := strconv.ParseUint(fields[1], 10, 32)
		if err != nil {
			c.Logger().V(2).Info("Failed to parse disk minor number",
				"line", line, "error", err)
			continue
		}

		device := fields[2]

		// Skip partitions - only include whole disks
		if isPartition(device) {
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

	// Only lock when accessing shared state
	c.mu.Lock()
	defer c.mu.Unlock()

	var interval float64
	if !c.prevTime.IsZero() {
		interval = currentTime.Sub(c.prevTime).Seconds()
	}

	// Create set of current devices
	currentDevices := make(map[string]bool, len(diskStats))

	// Calculate derived metrics and update prevStats
	for _, stats := range diskStats {
		currentDevices[stats.Device] = true

		if interval > 0 {
			if prevStats, ok := c.prevStats[stats.Device]; ok {
				c.calculateDerivedMetrics(stats, prevStats, interval)
			}
		}
		// Store current stats for next calculation
		c.prevStats[stats.Device] = stats
	}

	// Clean up devices that no longer exist
	for device := range c.prevStats {
		if !currentDevices[device] {
			delete(c.prevStats, device)
			c.Logger().V(2).Info("Removed stale device from tracking", "device", device)
		}
	}

	c.prevTime = currentTime
	c.Logger().V(1).Info("Collected disk statistics", "devices", len(diskStats))
	return diskStats, nil
}

// calculateDerivedMetrics calculates IOPS, throughput, utilization, and latency
//
// This function handles counter wraparound using the standard approach:
// if current < previous, we assume wraparound occurred and skip the calculation
// for that metric to avoid incorrect spikes in the data.
func (c *DiskCollector) calculateDerivedMetrics(current, prev *performance.DiskStats, interval float64) {
	// Validate interval
	if interval <= 0 {
		c.Logger().V(2).Info("Invalid interval for derived metrics calculation",
			"device", current.Device, "interval", interval)
		return
	}

	// Calculate IOPS (handle wraparound)
	if current.ReadsCompleted >= prev.ReadsCompleted && current.WritesCompleted >= prev.WritesCompleted {
		readOps := float64(current.ReadsCompleted - prev.ReadsCompleted)
		writeOps := float64(current.WritesCompleted - prev.WritesCompleted)
		current.IOPS = (readOps + writeOps) / interval
	}

	// Calculate throughput (sectors are 512 bytes)
	if current.SectorsRead >= prev.SectorsRead {
		readSectors := float64(current.SectorsRead - prev.SectorsRead)
		current.ReadBytesPerSec = (readSectors * sectorSize) / interval
	}

	if current.SectorsWritten >= prev.SectorsWritten {
		writeSectors := float64(current.SectorsWritten - prev.SectorsWritten)
		current.WriteBytesPerSec = (writeSectors * sectorSize) / interval
	}

	// Calculate utilization (io_time is in milliseconds)
	if current.IOTime >= prev.IOTime {
		ioTimeDelta := float64(current.IOTime - prev.IOTime)
		current.Utilization = (ioTimeDelta / (interval * msPerSecond)) * percentMultiplier
		if current.Utilization > percentMultiplier {
			current.Utilization = percentMultiplier
		}
	}

	// Calculate average queue size
	if current.WeightedIOTime >= prev.WeightedIOTime {
		weightedTimeDelta := float64(current.WeightedIOTime - prev.WeightedIOTime)
		current.AvgQueueSize = weightedTimeDelta / (interval * msPerSecond)
	}

	// Calculate average latencies
	if current.ReadsCompleted > prev.ReadsCompleted && current.ReadTime >= prev.ReadTime {
		readOps := float64(current.ReadsCompleted - prev.ReadsCompleted)
		readTimeDelta := float64(current.ReadTime - prev.ReadTime)
		current.AvgReadLatency = readTimeDelta / readOps
	}

	if current.WritesCompleted > prev.WritesCompleted && current.WriteTime >= prev.WriteTime {
		writeOps := float64(current.WritesCompleted - prev.WritesCompleted)
		writeTimeDelta := float64(current.WriteTime - prev.WriteTime)
		current.AvgWriteLatency = writeTimeDelta / writeOps
	}
}

// isPartition checks if a device name represents a partition
//
// Partitions are identified by:
// - Standard devices: end with a digit (e.g., sda1, vdb2)
// - NVMe devices: contain 'pN' suffix (e.g., nvme0n1p1)
// - MMC devices: contain 'pN' suffix (e.g., mmcblk0p1)
//
// Special cases:
// - loop devices (loop0, loop1) are whole devices, not partitions
// - device mapper devices (dm-0, dm-1) are whole devices, not partitions
func isPartition(device string) bool {
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
