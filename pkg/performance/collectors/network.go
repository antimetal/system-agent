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

// NetworkCollector collects network interface statistics from /proc/net/dev and /sys/class/net
//
// This collector reads network interface statistics from the Linux proc and sys filesystems.
// It collects packet counts, byte counts, error counts, and interface metadata.
//
// Data sources:
// - /proc/net/dev: Interface statistics (packets, bytes, errors, drops)
// - /sys/class/net/[interface]/speed: Link speed in Mbps
// - /sys/class/net/[interface]/duplex: Duplex mode (full/half)
// - /sys/class/net/[interface]/operstate: Operational state (up/down)
// - /sys/class/net/[interface]/carrier: Link detection status
//
// Reference: https://www.kernel.org/doc/html/latest/networking/statistics.html
type NetworkCollector struct {
	performance.BaseDeltaCollector
	procNetDevPath  string
	sysClassNetPath string
}


func init() {
	performance.Register(performance.MetricTypeNetwork, performance.PartialNewContinuousPointCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewNetworkCollector(logger, config)
		},
	))
}

func NewNetworkCollector(logger logr.Logger, config performance.CollectionConfig) (*NetworkCollector, error) {
	if err := config.Validate(performance.ValidateOptions{RequireHostProcPath: true, RequireHostSysPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   false,
		RequiredCapabilities: nil,     // No special capabilities required
		MinKernelVersion:     "2.6.0", // /proc/net/dev has been around forever
	}

	return &NetworkCollector{
		BaseDeltaCollector: performance.NewBaseDeltaCollector(
			performance.MetricTypeNetwork,
			"Network Statistics Collector",
			logger,
			config,
			capabilities,
		),
		procNetDevPath:  filepath.Join(config.HostProcPath, "net", "dev"),
		sysClassNetPath: filepath.Join(config.HostSysPath, "class", "net"),
	}, nil
}

func (c *NetworkCollector) Collect(ctx context.Context) (any, error) {
	return c.collectNetworkStats()
}

// collectNetworkStats reads and parses /proc/net/dev and /sys/class/net/[interface]/*
//
// /proc/net/dev format:
//
//	Inter-|   Receive                                                |  Transmit
//	 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
//	    lo: 1234567   12345    0    0    0     0          0         0 1234567   12345    0    0    0     0       0          0
//	  eth0: 9876543   98765    0    0    0     0          0         0 9876543   98765    0    0    0     0       0          0
//
// The first two lines are headers. Each interface line contains:
// - Interface name followed by ':'
// - 8 receive statistics fields
// - 8 transmit statistics fields
//
// All counter values are cumulative since interface initialization.
//
// Error handling strategy:
// - /proc/net/dev is critical - returns error if unavailable
// - /sys/class/net/* files are optional - logs warnings but continues
// - Malformed lines in /proc/net/dev are skipped with logging
//
// Reference: https://www.kernel.org/doc/html/latest/networking/statistics.html
func (c *NetworkCollector) collectNetworkStats() ([]performance.NetworkStats, error) {
	// Read /proc/net/dev
	file, err := os.Open(c.procNetDevPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", c.procNetDevPath, err)
	}
	defer file.Close()

	var stats []performance.NetworkStats
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip the two header lines
		if lineNum <= 2 {
			continue
		}

		// Parse interface line
		// Format: interface_name: rx_bytes rx_packets ... tx_compressed
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}

		ifaceName := strings.TrimSpace(parts[0])
		fields := strings.Fields(parts[1])

		if len(fields) < 16 {
			continue // Not enough fields
		}

		// Parse all the counters
		stat := performance.NetworkStats{
			Interface: ifaceName,
		}

		// Receive statistics (columns 1-8)
		var err error
		stat.RxBytes, err = strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			c.Logger().V(2).Info("Failed to parse rx_bytes", "interface", ifaceName, "value", fields[0], "error", err)
		}
		stat.RxPackets, err = strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			c.Logger().V(2).Info("Failed to parse rx_packets", "interface", ifaceName, "value", fields[1], "error", err)
		}
		stat.RxErrors, _ = strconv.ParseUint(fields[2], 10, 64)
		stat.RxDropped, _ = strconv.ParseUint(fields[3], 10, 64)
		stat.RxFIFO, _ = strconv.ParseUint(fields[4], 10, 64)
		stat.RxFrame, _ = strconv.ParseUint(fields[5], 10, 64)
		stat.RxCompressed, _ = strconv.ParseUint(fields[6], 10, 64)
		stat.RxMulticast, _ = strconv.ParseUint(fields[7], 10, 64)

		// Transmit statistics (columns 9-16)
		stat.TxBytes, err = strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			c.Logger().V(2).Info("Failed to parse tx_bytes", "interface", ifaceName, "value", fields[8], "error", err)
		}
		stat.TxPackets, err = strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			c.Logger().V(2).Info("Failed to parse tx_packets", "interface", ifaceName, "value", fields[9], "error", err)
		}
		stat.TxErrors, _ = strconv.ParseUint(fields[10], 10, 64)
		stat.TxDropped, _ = strconv.ParseUint(fields[11], 10, 64)
		stat.TxFIFO, _ = strconv.ParseUint(fields[12], 10, 64)
		stat.TxCollisions, _ = strconv.ParseUint(fields[13], 10, 64)
		stat.TxCarrier, _ = strconv.ParseUint(fields[14], 10, 64)
		stat.TxCompressed, _ = strconv.ParseUint(fields[15], 10, 64)

		// Read interface metadata from /sys/class/net/[interface]/
		c.readInterfaceMetadata(&stat)

		stats = append(stats, stat)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading %s: %w", c.procNetDevPath, err)
	}

	c.Logger().V(1).Info("Collected network statistics", "interfaces", len(stats))
	return stats, nil
}

// readInterfaceMetadata reads interface properties from /sys/class/net/[interface]/
//
// Files read from sysfs:
// - speed: Link speed in Mbps (e.g., 1000 for gigabit)
// - duplex: Duplex mode ("full" or "half")
// - operstate: Operational state (e.g., "up", "down", "unknown")
// - carrier: Physical link detection (1 = link detected, 0 = no link)
//
// Note: Some of these files may not exist for virtual interfaces (lo, docker0, etc.)
// or may return errors if the interface is down. We gracefully handle these cases.
func (c *NetworkCollector) readInterfaceMetadata(stat *performance.NetworkStats) {
	ifacePath := filepath.Join(c.sysClassNetPath, stat.Interface)

	// Read speed (link speed in Mbps)
	speedPath := filepath.Join(ifacePath, "speed")
	if speedData, err := os.ReadFile(speedPath); err == nil {
		if speed, err := strconv.ParseUint(strings.TrimSpace(string(speedData)), 10, 64); err == nil {
			stat.Speed = speed
		}
	}

	// Read duplex mode
	duplexPath := filepath.Join(ifacePath, "duplex")
	if duplexData, err := os.ReadFile(duplexPath); err == nil {
		stat.Duplex = strings.TrimSpace(string(duplexData))
	}

	// Read operational state
	operstatePath := filepath.Join(ifacePath, "operstate")
	if operstateData, err := os.ReadFile(operstatePath); err == nil {
		stat.OperState = strings.TrimSpace(string(operstateData))
	}

	// Read carrier (link detection)
	carrierPath := filepath.Join(ifacePath, "carrier")
	if carrierData, err := os.ReadFile(carrierPath); err == nil {
		carrier := strings.TrimSpace(string(carrierData))
		stat.LinkDetected = carrier == "1"
	}
}

func (c *NetworkCollector) CollectWithDelta(ctx context.Context, config performance.DeltaConfig) (any, error) {
	stats, err := c.collectNetworkStats()
	if err != nil {
		return nil, fmt.Errorf("failed to collect network stats: %w", err)
	}

	currentTime := time.Now()

	if c.HasDeltaState() {
		if should, reason := c.ShouldCalculateDeltas(currentTime); should {
			previous := c.LastSnapshot.([]performance.NetworkStats)
			c.calculateNetworkDeltas(stats, previous, currentTime, config)
		} else {
			c.Logger().V(2).Info("Skipping delta calculation", "reason", reason)
		}
	}

	c.UpdateDeltaState(stats, currentTime)
	c.Logger().V(1).Info("Collected network statistics with delta support", "interfaces", len(stats))
	return stats, nil
}

func (c *NetworkCollector) calculateNetworkDeltas(
	current, previous []performance.NetworkStats,
	currentTime time.Time,
	config performance.DeltaConfig,
) {
	interval := currentTime.Sub(c.LastTime)

	// Create a map of previous stats by interface name for efficient lookup
	prevStatsMap := make(map[string]*performance.NetworkStats)
	for i := range previous {
		prevStatsMap[previous[i].Interface] = &previous[i]
	}

	// Calculate deltas for each current interface
	for i := range current {
		prevStat, exists := prevStatsMap[current[i].Interface]
		if !exists {
			// New interface - skip delta calculation for this interface
			c.Logger().V(2).Info("New interface detected, skipping delta calculation",
				"interface", current[i].Interface)
			continue
		}

		c.calculateInterfaceDeltas(&current[i], prevStat, interval, config)
	}
}

func (c *NetworkCollector) calculateInterfaceDeltas(
	current, previous *performance.NetworkStats,
	interval time.Duration,
	config performance.DeltaConfig,
) {
	var resetDetected bool

	delta := &performance.NetworkDeltaData{}

	calculateField := func(currentVal, previousVal uint64) uint64 {
		deltaVal, reset := c.CalculateUint64Delta(currentVal, previousVal, interval)
		resetDetected = resetDetected || reset
		return deltaVal
	}

	delta.RxBytes = calculateField(current.RxBytes, previous.RxBytes)
	delta.TxBytes = calculateField(current.TxBytes, previous.TxBytes)
	delta.RxPackets = calculateField(current.RxPackets, previous.RxPackets)
	delta.TxPackets = calculateField(current.TxPackets, previous.TxPackets)
	delta.RxErrors = calculateField(current.RxErrors, previous.RxErrors)
	delta.TxErrors = calculateField(current.TxErrors, previous.TxErrors)
	delta.RxDropped = calculateField(current.RxDropped, previous.RxDropped)
	delta.TxDropped = calculateField(current.TxDropped, previous.TxDropped)

	if !resetDetected {
		intervalSecs := interval.Seconds()
		if intervalSecs > 0 {
			delta.RxBytesPerSec = uint64(float64(delta.RxBytes) / intervalSecs)
			delta.TxBytesPerSec = uint64(float64(delta.TxBytes) / intervalSecs)
			delta.RxPacketsPerSec = uint64(float64(delta.RxPackets) / intervalSecs)
			delta.TxPacketsPerSec = uint64(float64(delta.TxPackets) / intervalSecs)
			delta.RxErrorsPerSec = uint64(float64(delta.RxErrors) / intervalSecs)
			delta.TxErrorsPerSec = uint64(float64(delta.TxErrors) / intervalSecs)
			delta.RxDroppedPerSec = uint64(float64(delta.RxDropped) / intervalSecs)
			delta.TxDroppedPerSec = uint64(float64(delta.TxDropped) / intervalSecs)
		}
	}

	// Use composition helper to set metadata
	c.PopulateMetadata(delta, time.Now(), resetDetected)
	current.Delta = delta

	if resetDetected {
		c.Logger().V(1).Info("Counter reset detected for network interface", "interface", current.Interface)
	}
}
