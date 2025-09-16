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
	performance.Register(performance.MetricTypeSystem, performance.PartialNewContinuousPointCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewSystemStatsCollector(logger, config)
		},
	))
}

// Compile-time interface check
var _ performance.PointCollector = (*SystemStatsCollector)(nil)

// SystemStatsCollector collects system-wide activity statistics from /proc/stat
//
// Purpose: System activity monitoring for interrupt and context switch rates
// This collector provides cumulative counters for system activity that can be
// used to calculate vmstat-compatible rates (interrupts/sec, context switches/sec).
//
// This collector reads system activity counters from /proc/stat, including:
// - Total interrupt count (intr line, first value)
// - Total context switches (ctxt line)
//
// These are cumulative counters since boot that can be used by higher-level
// code to compute rates between collection intervals.
//
// Performance Investigation Context:
//
// Context Switches (cs in vmstat):
// - Measures kernel switches between processes/threads since boot
// - High rates (>10,000/sec) indicate CPU scheduling pressure:
//   - Too many competing processes for CPU resources
//   - Processes frequently blocking on I/O or synchronization
//   - Poor thread pool sizing in applications
//
// - Diagnostic patterns:
//   - High CS + High CPU = CPU-bound workload with many competing processes
//   - High CS + Low CPU = Processes blocking on I/O, locks, or synchronization
//   - High CS + High Load = System overloaded with too many runnable processes
//
// Interrupts (in in vmstat):
// - Measures total hardware/software interrupts processed since boot
// - Shows system activity level for:
//   - Network packets (NIC interrupts)
//   - Disk I/O completions (storage controller interrupts)
//   - Timer interrupts and inter-processor interrupts (IPI)
//
// - Diagnostic patterns:
//   - High interrupts + High I/O = Normal for busy systems
//   - High interrupts + Low I/O = May indicate interrupt storms or driver issues
//   - Sudden spikes = Hardware issues or inefficient drivers
//
// Integration with Brendan Gregg's 60-Second Methodology:
// These counters enable calculation of vmstat's 'cs' and 'in' fields, which are
// essential for rapid performance triage and identifying CPU scheduling vs I/O
// bottlenecks in system performance investigations.
//
// Key Data Sources:
// - /proc/stat: intr and ctxt lines for system activity counters
//
// Error handling strategy:
// - /proc/stat is critical - returns error if unavailable
// - Individual field parsing errors are logged but don't fail collection
// - Missing fields are left as zero (graceful degradation)
//
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html#miscellaneous-kernel-statistics-in-proc-stat
type SystemStatsCollector struct {
	performance.BaseDeltaCollector
	statPath string
}

func NewSystemStatsCollector(logger logr.Logger, config performance.CollectionConfig) (*SystemStatsCollector, error) {
	if err := config.Validate(performance.ValidateOptions{RequireHostProcPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   false,
		RequiredCapabilities: nil,     // No special capabilities required
		MinKernelVersion:     "2.6.0", // /proc/stat has been around forever
	}

	return &SystemStatsCollector{
		BaseDeltaCollector: performance.NewBaseDeltaCollector(
			performance.MetricTypeSystem,
			"System Activity Collector",
			logger,
			config,
			capabilities,
		),
		statPath: filepath.Join(config.HostProcPath, "stat"),
	}, nil
}

func (c *SystemStatsCollector) Collect(ctx context.Context) (performance.Event, error) {
	currentTime := time.Now()

	// Collect current statistics
	currentStats, err := c.collectSystemStats()
	if err != nil {
		return performance.Event{}, fmt.Errorf("failed to collect system stats: %w", err)
	}

	shouldCalc, reason := c.ShouldCalculateDeltas(currentTime)
	if !shouldCalc {
		c.Logger().V(2).Info("Skipping delta calculation", "reason", reason)
		if c.IsFirst {
			c.UpdateDeltaState(currentStats, currentTime)
		}
		c.Logger().V(1).Info("Collected system statistics")
		return performance.Event{Metric: performance.MetricTypeSystem, Data: currentStats}, nil
	}

	previousStats, ok := c.LastSnapshot.(*performance.SystemStats)
	if !ok || previousStats == nil {
		c.UpdateDeltaState(currentStats, currentTime)
		c.Logger().V(1).Info("Collected system statistics")
		return performance.Event{Metric: performance.MetricTypeSystem, Data: currentStats}, nil
	}

	c.calculateSystemDeltas(currentStats, previousStats, currentTime, c.Config)
	c.UpdateDeltaState(currentStats, currentTime)

	c.Logger().V(1).Info("Collected system statistics with delta support")
	return performance.Event{Metric: performance.MetricTypeSystem, Data: currentStats}, nil
}

// collectSystemStats reads and parses system activity statistics from /proc/stat
//
// This method collects cumulative counters for system activity that are used
// to calculate vmstat-compatible rates (interrupts/sec, context switches/sec).
//
// /proc/stat format (relevant lines):
//
//	intr total_interrupts individual_interrupt_counts...
//	ctxt total_context_switches
//
// The intr line contains the total interrupt count as the first value after "intr".
// The ctxt line contains the total context switch count.
//
// Error handling strategy:
// - File read errors return an error (critical failure)
// - Individual field parsing errors are logged but don't fail collection
// - Missing fields are left as zero (graceful degradation)
func (c *SystemStatsCollector) collectSystemStats() (*performance.SystemStats, error) {
	file, err := os.Open(c.statPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", c.statPath, err)
	}
	defer file.Close()

	stats := &performance.SystemStats{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		switch parts[0] {
		case "intr":
			// intr line format: "intr total_interrupts individual_counts..."
			// We want the total (first value after "intr")
			if interrupts, err := strconv.ParseUint(parts[1], 10, 64); err != nil {
				c.Logger().V(2).Info("Failed to parse interrupt count",
					"value", parts[1], "error", err)
			} else {
				stats.Interrupts = interrupts
			}

		case "ctxt":
			// ctxt line format: "ctxt total_context_switches"
			if contextSwitches, err := strconv.ParseUint(parts[1], 10, 64); err != nil {
				c.Logger().V(2).Info("Failed to parse context switch count",
					"value", parts[1], "error", err)
			} else {
				stats.ContextSwitches = contextSwitches
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading %s: %w", c.statPath, err)
	}

	return stats, nil
}

func (c *SystemStatsCollector) calculateSystemDeltas(
	current, previous *performance.SystemStats,
	currentTime time.Time,
	config performance.DeltaConfig,
) {
	interval := currentTime.Sub(c.LastTime)
	var resetDetected bool

	// Create nested delta data structure
	delta := &performance.SystemDeltaData{}

	calculateField := func(currentVal, previousVal uint64) uint64 {
		deltaVal, reset := c.CalculateUint64Delta(currentVal, previousVal, interval)
		resetDetected = resetDetected || reset
		return deltaVal
	}

	// Calculate delta values
	delta.Interrupts = calculateField(current.Interrupts, previous.Interrupts)
	delta.ContextSwitches = calculateField(current.ContextSwitches, previous.ContextSwitches)

	// Calculate rates if no reset detected
	if !resetDetected {
		intervalSecs := interval.Seconds()
		if intervalSecs > 0 {
			delta.InterruptsPerSec = uint64(float64(delta.Interrupts) / intervalSecs)
			delta.ContextSwitchesPerSec = uint64(float64(delta.ContextSwitches) / intervalSecs)
		}
	}

	// Use composition helper to set metadata
	c.PopulateMetadata(delta, currentTime, resetDetected)
	current.Delta = delta

	if resetDetected {
		c.Logger().V(1).Info("Counter reset detected in system statistics")
	}
}
