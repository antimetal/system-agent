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

// LoadCollector collects system load statistics from /proc/loadavg and /proc/uptime
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html#proc-loadavg
type LoadCollector struct {
	performance.BaseCollector
	loadavgPath string
	uptimePath  string
}

func NewLoadCollector(logger logr.Logger, config performance.CollectionConfig) *LoadCollector {
	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    true,
		SupportsContinuous: false,
		RequiresRoot:       false,
		RequiresEBPF:       false,
		MinKernelVersion:   "2.6.0", // /proc/loadavg has been around forever
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
	}
}

func (c *LoadCollector) Collect(ctx context.Context) (any, error) {
	return c.collectLoadStats()
}

// collectLoadStats reads and parses /proc/loadavg and /proc/uptime
//
// /proc/loadavg: load1 load5 load15 nr_running/nr_threads last_pid
// /proc/uptime: uptime_seconds idle_seconds
//
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html
func (c *LoadCollector) collectLoadStats() (*performance.LoadStats, error) {
	stats := &performance.LoadStats{}

	// Read /proc/loadavg
	loadavgData, err := os.ReadFile(c.loadavgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", c.loadavgPath, err)
	}

	fields := strings.Fields(string(loadavgData))
	if len(fields) < 5 {
		return nil, fmt.Errorf("unexpected format in %s: got %d fields, expected 5 (load1 load5 load15 running/total lastpid): %q", 
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

	// Read /proc/uptime for system uptime
	// This file provides uptime in seconds since boot
	uptimeData, err := os.ReadFile(c.uptimePath)
	if err != nil {
		// Log but don't fail - uptime is optional (may not be available in some containers)
		c.Logger().V(2).Info("Failed to read uptime file", "path", c.uptimePath, "error", err)
	} else {
		uptimeFields := strings.Fields(string(uptimeData))
		if len(uptimeFields) < 1 {
			c.Logger().V(2).Info("Unexpected uptime format", "path", c.uptimePath, 
				"content", strings.TrimSpace(string(uptimeData)))
		} else {
			uptimeSeconds, err := strconv.ParseFloat(uptimeFields[0], 64)
			if err != nil {
				c.Logger().V(2).Info("Failed to parse uptime", "value", uptimeFields[0], "error", err)
			} else {
				stats.Uptime = time.Duration(uptimeSeconds * float64(time.Second))
			}
		}
	}

	return stats, nil
}
