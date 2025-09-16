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
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

func init() {
	performance.Register(
		performance.MetricTypeDisk, performance.PartialNewContinuousPointCollector(
			func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
				return NewDiskCollector(logger, config)
			},
		),
	)
}

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
	performance.BaseDeltaCollector
	diskstatsPath string
}

func NewDiskCollector(logger logr.Logger, config performance.CollectionConfig) (*DiskCollector, error) {
	if err := config.Validate(performance.ValidateOptions{RequireHostProcPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   false,
		RequiredCapabilities: nil,     // No special capabilities required
		MinKernelVersion:     "2.6.0", // /proc/diskstats has been around since 2.6
	}

	return &DiskCollector{
		BaseDeltaCollector: performance.NewBaseDeltaCollector(
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
func (c *DiskCollector) Collect(ctx context.Context) (performance.Event, error) {
	stats, err := c.collectDiskStats()
	if err != nil {
		return performance.Event{}, fmt.Errorf("failed to collect disk stats: %w", err)
	}

	currentTime := time.Now()

	// Calculate deltas if we have previous state
	if c.HasDeltaState() {
		if should, reason := c.ShouldCalculateDeltas(currentTime); should {
			previous := c.LastSnapshot.([]*performance.DiskStats)
			c.calculateDiskDeltas(stats, previous, currentTime, c.Config)
		} else {
			c.Logger().V(2).Info("Skipping delta calculation", "reason", reason)
		}
	}
	c.UpdateDeltaState(stats, currentTime)

	c.Logger().V(1).Info("Collected disk statistics", "devices", len(stats))
	return performance.Event{Metric: performance.MetricTypeDisk, Data: stats}, nil
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

func (c *DiskCollector) calculateDiskDeltas(
	current, previous []*performance.DiskStats,
	currentTime time.Time,
	config performance.DeltaConfig,
) {
	interval := currentTime.Sub(c.LastTime)

	// Create a map of previous stats by device name for efficient lookup
	prevStatsMap := make(map[string]*performance.DiskStats)
	for _, prevStat := range previous {
		prevStatsMap[prevStat.Device] = prevStat
	}

	// Calculate deltas for each current device
	for _, currentStat := range current {
		prevStat, exists := prevStatsMap[currentStat.Device]
		if !exists {
			// New device - skip delta calculation
			c.Logger().V(2).Info("New disk device detected, skipping delta calculation",
				"device", currentStat.Device)
			continue
		}

		c.calculateDeviceDeltas(currentStat, prevStat, interval, config)
	}
}

func (c *DiskCollector) calculateDeviceDeltas(
	current, previous *performance.DiskStats,
	interval time.Duration,
	config performance.DeltaConfig,
) {
	var resetDetected bool

	delta := &performance.DiskDeltaData{}

	calculateField := func(currentVal, previousVal uint64) uint64 {
		deltaVal, reset := c.CalculateUint64Delta(currentVal, previousVal, interval)
		resetDetected = resetDetected || reset
		return deltaVal
	}

	delta.ReadsCompleted = calculateField(current.ReadsCompleted, previous.ReadsCompleted)
	delta.WritesCompleted = calculateField(current.WritesCompleted, previous.WritesCompleted)
	delta.ReadsMerged = calculateField(current.ReadsMerged, previous.ReadsMerged)
	delta.WritesMerged = calculateField(current.WritesMerged, previous.WritesMerged)
	delta.SectorsRead = calculateField(current.SectorsRead, previous.SectorsRead)
	delta.SectorsWritten = calculateField(current.SectorsWritten, previous.SectorsWritten)
	delta.ReadTime = calculateField(current.ReadTime, previous.ReadTime)
	delta.WriteTime = calculateField(current.WriteTime, previous.WriteTime)
	delta.IOTime = calculateField(current.IOTime, previous.IOTime)
	delta.WeightedIOTime = calculateField(current.WeightedIOTime, previous.WeightedIOTime)

	if !resetDetected {
		intervalSecs := interval.Seconds()
		if intervalSecs > 0 {
			// Basic rate calculations
			delta.ReadsPerSec = uint64(float64(delta.ReadsCompleted) / intervalSecs)
			delta.WritesPerSec = uint64(float64(delta.WritesCompleted) / intervalSecs)
			delta.SectorsReadPerSec = uint64(float64(delta.SectorsRead) / intervalSecs)
			delta.SectorsWrittenPerSec = uint64(float64(delta.SectorsWritten) / intervalSecs)

			// Calculate iostat-compatible performance metrics
			c.calculatePerformanceMetrics(delta, intervalSecs)
		}
	}

	// Use composition helper to set metadata
	c.PopulateMetadata(delta, time.Now(), resetDetected)
	current.Delta = delta

	if resetDetected {
		c.Logger().V(1).Info("Counter reset detected for disk device", "device", current.Device)
	}
}

// calculatePerformanceMetrics computes iostat-compatible performance metrics
func (c *DiskCollector) calculatePerformanceMetrics(delta *performance.DiskDeltaData, intervalSecs float64) {
	// IOPS: Total I/O operations per second (iostat: r/s + w/s)
	delta.IOPS = delta.ReadsPerSec + delta.WritesPerSec

	// Throughput: Convert sectors to bytes (iostat: rKB/s and wKB/s but in bytes)
	// Linux sectors are always 512 bytes
	delta.ReadBytesPerSec = delta.SectorsReadPerSec * 512
	delta.WriteBytesPerSec = delta.SectorsWrittenPerSec * 512

	// Utilization: Percentage of time device was busy (iostat: %util)
	// IOTime is in milliseconds, convert to percentage of interval
	delta.Utilization = (float64(delta.IOTime) / (intervalSecs * 1000.0)) * 100.0
	if delta.Utilization > 100.0 {
		delta.Utilization = 100.0 // Cap at 100% for devices that can exceed 100% utilization
	}

	// Average queue size: WeightedIOTime represents queue depth over time (iostat: avgqu-sz)
	// WeightedIOTime is in milliseconds, convert to average queue depth
	delta.AvgQueueSize = float64(delta.WeightedIOTime) / (intervalSecs * 1000.0)

	// Average latencies: Time per operation in milliseconds (iostat: r_await, w_await)
	// Calculate only if there were operations to avoid division by zero
	if delta.ReadsCompleted > 0 {
		delta.AvgReadLatency = uint64(float64(delta.ReadTime) / float64(delta.ReadsCompleted))
	}
	if delta.WritesCompleted > 0 {
		delta.AvgWriteLatency = uint64(float64(delta.WriteTime) / float64(delta.WritesCompleted))
	}
}
