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
	performance.Register(performance.MetricTypeLoad, performance.PartialNewContinuousPointCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewLoadCollector(logger, config)
		},
	))
}

// Compile-time interface check
var _ performance.PointCollector = (*LoadCollector)(nil)

// LoadCollector collects system load statistics from /proc/loadavg and /proc/uptime
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html#proc-loadavg
type LoadCollector struct {
	performance.BaseCollector
	loadavgPath string
	uptimePath  string
	statPath    string
}

func NewLoadCollector(logger logr.Logger, config performance.CollectionConfig) (*LoadCollector, error) {
	if err := config.Validate(performance.ValidateOptions{RequireHostProcPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   false,
		RequiredCapabilities: nil,     // No special capabilities required
		MinKernelVersion:     "2.6.0", // /proc/loadavg has been around forever
	}

	return &LoadCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeLoad,
			"System Load Collector",
			logger,
			config,
			capabilities,
		),
		loadavgPath: filepath.Join(config.HostProcPath, "loadavg"),
		uptimePath:  filepath.Join(config.HostProcPath, "uptime"),
		statPath:    filepath.Join(config.HostProcPath, "stat"),
	}, nil
}

func (c *LoadCollector) Collect(ctx context.Context) (performance.Event, error) {
	stats, err := c.collectLoadStats()
	if err != nil {
		return performance.Event{}, err
	}
	return performance.Event{Metric: performance.MetricTypeLoad, Data: stats}, nil
}

// collectLoadStats reads and parses /proc/loadavg and /proc/uptime
//
// Error Handling Strategy:
// - /proc/loadavg: Any parsing error returns an error (critical data)
// - /proc/uptime: Parsing errors are logged but don't fail collection (optional data)
//
// This design ensures the collector works in containerized environments where
// uptime may not be available while still providing essential load metrics.
//
// File formats:
// - /proc/loadavg: load1 load5 load15 nr_running/nr_threads last_pid
// - /proc/uptime: uptime_seconds idle_seconds
//
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html
func (c *LoadCollector) collectLoadStats() (*performance.LoadStats, error) {
	stats := &performance.LoadStats{}

	// Read /proc/loadavg - critical data, any error fails the collection
	loadavgData, err := os.ReadFile(c.loadavgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", c.loadavgPath, err)
	}

	fields := strings.Fields(string(loadavgData))
	if len(fields) < 5 {
		return nil, fmt.Errorf("unexpected format in %s: got %d fields, expected 5: %q",
			c.loadavgPath, len(fields), strings.TrimSpace(string(loadavgData)))
	}

	// Parse load averages
	if stats.Load1Min, err = strconv.ParseFloat(fields[0], 64); err != nil {
		return nil, fmt.Errorf("failed to parse 1min load average from %q: %w", fields[0], err)
	}

	if stats.Load5Min, err = strconv.ParseFloat(fields[1], 64); err != nil {
		return nil, fmt.Errorf("failed to parse 5min load average from %q: %w", fields[1], err)
	}

	if stats.Load15Min, err = strconv.ParseFloat(fields[2], 64); err != nil {
		return nil, fmt.Errorf("failed to parse 15min load average from %q: %w", fields[2], err)
	}

	// Parse running/total processes
	procParts := strings.Split(fields[3], "/")
	if len(procParts) != 2 {
		return nil, fmt.Errorf("unexpected process count format: expected 'running/total', got %q", fields[3])
	}

	running, err := strconv.ParseInt(procParts[0], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to parse running process count from %q: %w", procParts[0], err)
	}
	stats.RunningProcs = int32(running)

	total, err := strconv.ParseInt(procParts[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to parse total process count from %q: %w", procParts[1], err)
	}
	stats.TotalProcs = int32(total)

	// Parse last PID
	lastPID, err := strconv.ParseInt(fields[4], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to parse last PID from %q: %w", fields[4], err)
	}
	stats.LastPID = int32(lastPID)

	// Read /proc/uptime for system uptime - optional data, errors are logged but don't fail collection
	// Format: "uptime_seconds idle_seconds" - kernel always provides exactly 2 fields
	// Reference: https://github.com/torvalds/linux/blob/master/fs/proc/uptime.c
	//
	// Graceful degradation rationale:
	// - Uptime is supplementary information, not critical for load monitoring
	// - Some containerized environments may not provide /proc/uptime
	// - Load averages and process counts are the essential metrics
	uptimeData, err := os.ReadFile(c.uptimePath)
	if err != nil {
		c.Logger().V(1).Info("Failed to read uptime file (continuing without uptime)", "path", c.uptimePath, "error", err)
	} else {
		uptimeFields := strings.Fields(string(uptimeData))
		if len(uptimeFields) != 2 {
			c.Logger().V(1).Info("Unexpected uptime format - expected 2 fields (continuing with zero uptime)", "path", c.uptimePath,
				"fields", len(uptimeFields), "content", strings.TrimSpace(string(uptimeData)))
		} else {
			uptimeSeconds, err := strconv.ParseFloat(uptimeFields[0], 64)
			if err != nil {
				c.Logger().V(1).Info("Failed to parse uptime (continuing with zero uptime)", "value", uptimeFields[0], "error", err)
			} else {
				stats.Uptime = time.Duration(uptimeSeconds * float64(time.Second))
			}
		}
	}

	// Read blocked processes from /proc/stat - optional data, errors are logged but don't fail collection
	c.collectBlockedProcs(stats)

	return stats, nil
}

// collectBlockedProcs reads blocked process count from /proc/stat
//
// This method reads the procs_blocked field from /proc/stat to provide
// the blocked process count needed for vmstat-compatible metrics.
//
// /proc/stat format (partial):
//
//	procs_blocked value
//
// Error handling strategy:
// - /proc/stat is optional - logs warnings but continues if unavailable
// - Parse errors are logged but don't fail collection
// - Missing field is left as zero (graceful degradation)
func (c *LoadCollector) collectBlockedProcs(stats *performance.LoadStats) {
	file, err := os.Open(c.statPath)
	if err != nil {
		c.Logger().V(2).Info("Optional file not available", "path", c.statPath, "error", err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "procs_blocked ") {
			parts := strings.Fields(line)
			if len(parts) != 2 {
				c.Logger().V(2).Info("Unexpected procs_blocked format",
					"line", line, "fields", len(parts))
				return
			}

			blocked, err := strconv.ParseInt(parts[1], 10, 32)
			if err != nil {
				c.Logger().V(2).Info("Failed to parse procs_blocked",
					"value", parts[1], "error", err)
				return
			}

			stats.BlockedProcs = int32(blocked)
			return
		}
	}

	if err := scanner.Err(); err != nil {
		c.Logger().V(2).Info("Error reading stat file", "path", c.statPath, "error", err)
	}
}
