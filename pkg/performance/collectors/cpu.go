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
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

func init() {
	performance.Register(performance.MetricTypeCPU, performance.PartialNewContinuousPointCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewCPUCollector(logger, config)
		},
	))
}

// CPUCollector collects CPU statistics from /proc/stat
//
// This collector reads CPU time statistics from the Linux proc filesystem.
// It collects both aggregate CPU stats and per-CPU core statistics.
//
// The CPU times are reported in "jiffies" (clock ticks), which can be converted
// to seconds by dividing by the system's USER_HZ value (typically 100).
//
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html#proc-stat
type CPUCollector struct {
	performance.BaseDeltaCollector
	statPath string
}

func NewCPUCollector(logger logr.Logger, config performance.CollectionConfig) (*CPUCollector, error) {
	if err := config.Validate(performance.ValidateOptions{RequireHostProcPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   false,
		RequiredCapabilities: nil,     // No special capabilities required
		MinKernelVersion:     "2.6.0", // /proc/stat has been around forever
	}

	return &CPUCollector{
		BaseDeltaCollector: performance.NewBaseDeltaCollector(
			performance.MetricTypeCPU,
			"CPU Statistics Collector",
			logger,
			config,
			capabilities,
		),
		statPath: filepath.Join(config.HostProcPath, "stat"),
	}, nil
}

// Collect performs a one-shot collection of CPU statistics
func (c *CPUCollector) Collect(ctx context.Context) (any, error) {
	currentTime := time.Now()

	// Collect current statistics
	currentStats, err := c.collectCPUStats()
	if err != nil {
		return nil, fmt.Errorf("failed to collect CPU stats: %w", err)
	}

	// Check if delta calculation is enabled for this collector
	if !c.Config.IsEnabled(performance.MetricTypeCPU) {
		return currentStats, nil
	}

	shouldCalc, reason := c.ShouldCalculateDeltas(currentTime)
	if !shouldCalc {
		c.Logger().V(2).Info("Skipping delta calculation", "reason", reason)
		if c.IsFirst {
			c.UpdateDeltaState(currentStats, currentTime)
		}
		return currentStats, nil
	}

	previousStats, ok := c.LastSnapshot.([]*performance.CPUStats)
	if !ok || previousStats == nil {
		c.UpdateDeltaState(currentStats, currentTime)
		return currentStats, nil
	}

	c.calculateCPUDeltas(currentStats, previousStats, currentTime, c.Config)
	c.UpdateDeltaState(currentStats, currentTime)

	c.Logger().V(1).Info("Collected CPU statistics with delta support", "cpus", len(currentStats))
	return currentStats, nil
}

// collectCPUStats reads and parses /proc/stat for CPU statistics
//
// CPU lines format: cpu user nice system idle iowait irq softirq [steal guest guest_nice]
// Values are in USER_HZ units. The "cpu" line is the sum of all CPUs.
//
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html#proc-stat
func (c *CPUCollector) collectCPUStats() ([]*performance.CPUStats, error) {
	// Read /proc/stat
	statData, err := os.ReadFile(c.statPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", c.statPath, err)
	}

	lines := strings.Split(string(statData), "\n")
	var cpuStats []*performance.CPUStats

	for _, line := range lines {
		// Skip empty lines
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// We only care about CPU lines
		if !strings.HasPrefix(line, "cpu") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 8 {
			// Need at least: cpu user nice system idle iowait irq softirq
			continue
		}

		cpuName := fields[0]

		// Ensure this is either "cpu" or "cpu<number>" (not "cpufreq" etc)
		if cpuName != "cpu" {
			// Must be "cpu" followed by a number
			if len(cpuName) <= 3 || cpuName[3] < '0' || cpuName[3] > '9' {
				continue
			}
		}

		// Parse CPU index
		var cpuIndex int32 = -1 // -1 for aggregate "cpu" line
		if cpuName != "cpu" {
			// Extract CPU number from "cpu0", "cpu1", etc.
			cpuNumStr := strings.TrimPrefix(cpuName, "cpu")
			num, err := strconv.ParseInt(cpuNumStr, 10, 32)
			if err != nil {
				// Skip if we can't parse the CPU number
				continue
			}
			cpuIndex = int32(num)
		}

		// Parse CPU times (all values are in USER_HZ units)
		stats := &performance.CPUStats{
			CPUIndex: cpuIndex,
		}

		// Parse each field, defaulting to 0 if parsing fails
		// Log parse errors at debug level since they might indicate format changes
		if val, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
			stats.User = val
		} else {
			c.Logger().V(2).Info("Failed to parse user time", "cpu", cpuName, "value", fields[1], "error", err)
		}
		if val, err := strconv.ParseUint(fields[2], 10, 64); err == nil {
			stats.Nice = val
		} else {
			c.Logger().V(2).Info("Failed to parse nice time", "cpu", cpuName, "value", fields[2], "error", err)
		}
		if val, err := strconv.ParseUint(fields[3], 10, 64); err == nil {
			stats.System = val
		} else {
			c.Logger().V(2).Info("Failed to parse system time", "cpu", cpuName, "value", fields[3], "error", err)
		}
		if val, err := strconv.ParseUint(fields[4], 10, 64); err == nil {
			stats.Idle = val
		} else {
			c.Logger().V(2).Info("Failed to parse idle time", "cpu", cpuName, "value", fields[4], "error", err)
		}
		if val, err := strconv.ParseUint(fields[5], 10, 64); err == nil {
			stats.IOWait = val
		} else {
			c.Logger().V(2).Info("Failed to parse iowait time", "cpu", cpuName, "value", fields[5], "error", err)
		}
		if val, err := strconv.ParseUint(fields[6], 10, 64); err == nil {
			stats.IRQ = val
		} else {
			c.Logger().V(2).Info("Failed to parse irq time", "cpu", cpuName, "value", fields[6], "error", err)
		}
		if val, err := strconv.ParseUint(fields[7], 10, 64); err == nil {
			stats.SoftIRQ = val
		} else {
			c.Logger().V(2).Info("Failed to parse softirq time", "cpu", cpuName, "value", fields[7], "error", err)
		}

		// Optional fields (may not be present in older kernels)
		if len(fields) > 8 {
			if val, err := strconv.ParseUint(fields[8], 10, 64); err == nil {
				stats.Steal = val
			}
		}
		if len(fields) > 9 {
			if val, err := strconv.ParseUint(fields[9], 10, 64); err == nil {
				stats.Guest = val
			}
		}
		if len(fields) > 10 {
			if val, err := strconv.ParseUint(fields[10], 10, 64); err == nil {
				stats.GuestNice = val
			}
		}

		cpuStats = append(cpuStats, stats)
	}

	if len(cpuStats) == 0 {
		return nil, fmt.Errorf("no CPU statistics found in %s", c.statPath)
	}

	// Validate CPU indices are sequential and detect missing CPUs
	maxCPU := int32(-1)
	cpuMap := make(map[int32]bool)

	for _, stat := range cpuStats {
		if stat.CPUIndex >= 0 {
			cpuMap[stat.CPUIndex] = true
			if stat.CPUIndex > maxCPU {
				maxCPU = stat.CPUIndex
			}
		}
	}

	// Check for missing CPUs (excluding the aggregate CPU at index -1)
	if maxCPU >= 0 {
		var missingCPUs []int32
		for i := int32(0); i <= maxCPU; i++ {
			if !cpuMap[i] {
				missingCPUs = append(missingCPUs, i)
			}
		}

		if len(missingCPUs) > 0 {
			c.Logger().Info("Missing CPU indices detected",
				"missing", missingCPUs,
				"maxCPU", maxCPU,
				"foundCPUs", len(cpuMap))
		}
	}

	c.Logger().V(1).Info("Collected CPU statistics",
		"totalEntries", len(cpuStats),
		"cpuCores", len(cpuMap),
		"maxCPUIndex", maxCPU)
	return cpuStats, nil
}

func (c *CPUCollector) calculateCPUDeltas(
	current, previous []*performance.CPUStats,
	currentTime time.Time,
	config performance.DeltaConfig,
) {
	interval := currentTime.Sub(c.LastTime)

	// Create a map of previous stats by CPU index for efficient lookup
	prevStatsMap := make(map[int32]*performance.CPUStats)
	for _, prevStat := range previous {
		prevStatsMap[prevStat.CPUIndex] = prevStat
	}

	// Calculate deltas for each current CPU
	for _, currentStat := range current {
		prevStat, exists := prevStatsMap[currentStat.CPUIndex]
		if !exists {
			// New CPU - skip delta calculation
			c.Logger().V(2).Info("New CPU detected, skipping delta calculation",
				"cpuIndex", currentStat.CPUIndex)
			continue
		}

		c.calculateCPUCoreDeltas(currentStat, prevStat, interval, config)
	}
}

func (c *CPUCollector) calculateCPUCoreDeltas(
	current, previous *performance.CPUStats,
	interval time.Duration,
	config performance.DeltaConfig,
) {
	var resetDetected bool

	// Create delta data structure
	delta := &performance.CPUDeltaData{}

	calculateField := func(currentVal, previousVal uint64) uint64 {
		deltaVal, reset := c.CalculateUint64Delta(currentVal, previousVal, interval)
		resetDetected = resetDetected || reset
		return deltaVal
	}

	// Calculate time deltas
	delta.User = calculateField(current.User, previous.User)
	delta.Nice = calculateField(current.Nice, previous.Nice)
	delta.System = calculateField(current.System, previous.System)
	delta.Idle = calculateField(current.Idle, previous.Idle)
	delta.IOWait = calculateField(current.IOWait, previous.IOWait)
	delta.IRQ = calculateField(current.IRQ, previous.IRQ)
	delta.SoftIRQ = calculateField(current.SoftIRQ, previous.SoftIRQ)
	delta.Steal = calculateField(current.Steal, previous.Steal)
	delta.Guest = calculateField(current.Guest, previous.Guest)
	delta.GuestNice = calculateField(current.GuestNice, previous.GuestNice)

	// Calculate utilization percentages if no reset detected
	if !resetDetected {
		c.calculateCPUPercentages(delta)
	}

	// Use composition helper to set metadata
	c.PopulateMetadata(delta, time.Now(), resetDetected)
	current.Delta = delta

	if resetDetected {
		c.Logger().V(1).Info("Counter reset detected for CPU", "cpuIndex", current.CPUIndex)
	}
}

func (c *CPUCollector) calculateCPUPercentages(delta *performance.CPUDeltaData) {
	// Calculate total time delta
	totalTime := float64(delta.User + delta.Nice + delta.System + delta.Idle +
		delta.IOWait + delta.IRQ + delta.SoftIRQ + delta.Steal +
		delta.Guest + delta.GuestNice)

	if totalTime > 0 {
		// Calculate percentages
		delta.UserPercent = (float64(delta.User) / totalTime) * 100.0
		delta.NicePercent = (float64(delta.Nice) / totalTime) * 100.0
		delta.SystemPercent = (float64(delta.System) / totalTime) * 100.0
		delta.IdlePercent = (float64(delta.Idle) / totalTime) * 100.0
		delta.IOWaitPercent = (float64(delta.IOWait) / totalTime) * 100.0
		delta.IRQPercent = (float64(delta.IRQ) / totalTime) * 100.0
		delta.SoftIRQPercent = (float64(delta.SoftIRQ) / totalTime) * 100.0
		delta.StealPercent = (float64(delta.Steal) / totalTime) * 100.0
		delta.GuestPercent = (float64(delta.Guest) / totalTime) * 100.0
		delta.GuestNicePercent = (float64(delta.GuestNice) / totalTime) * 100.0
	}
}
