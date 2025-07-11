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

// LoadCollector collects system load statistics from /proc/loadavg
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
func (c *LoadCollector) collectLoadStats() (*performance.LoadStats, error) {
	stats := &performance.LoadStats{}

	// Read /proc/loadavg
	// Format: 0.00 0.01 0.05 1/234 5678
	// Where: 1min 5min 15min running/total lastpid
	loadavgData, err := os.ReadFile(c.loadavgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", c.loadavgPath, err)
	}

	fields := strings.Fields(string(loadavgData))
	if len(fields) < 5 {
		return nil, fmt.Errorf("unexpected format in %s: %s", c.loadavgPath, string(loadavgData))
	}

	// Parse load averages
	if stats.Load1Min, err = strconv.ParseFloat(fields[0], 64); err != nil {
		return nil, fmt.Errorf("failed to parse 1min load: %w", err)
	}

	if stats.Load5Min, err = strconv.ParseFloat(fields[1], 64); err != nil {
		return nil, fmt.Errorf("failed to parse 5min load: %w", err)
	}

	if stats.Load15Min, err = strconv.ParseFloat(fields[2], 64); err != nil {
		return nil, fmt.Errorf("failed to parse 15min load: %w", err)
	}

	// Parse running/total processes
	procParts := strings.Split(fields[3], "/")
	if len(procParts) != 2 {
		return nil, fmt.Errorf("unexpected process format: %s", fields[3])
	}

	running, err := strconv.ParseInt(procParts[0], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to parse running processes: %w", err)
	}
	stats.RunningProcs = int32(running)

	total, err := strconv.ParseInt(procParts[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to parse total processes: %w", err)
	}
	stats.TotalProcs = int32(total)

	// Parse last PID
	lastPID, err := strconv.ParseInt(fields[4], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to parse last PID: %w", err)
	}
	stats.LastPID = int32(lastPID)

	// Read /proc/uptime for system uptime
	// Format: uptime_seconds idle_seconds
	uptimeData, err := os.ReadFile(c.uptimePath)
	if err == nil {
		uptimeFields := strings.Fields(string(uptimeData))
		if len(uptimeFields) >= 1 {
			uptimeSeconds, err := strconv.ParseFloat(uptimeFields[0], 64)
			if err == nil {
				stats.Uptime = time.Duration(uptimeSeconds * float64(time.Second))
			}
		}
	}
	// If we can't read uptime, just continue without it

	return stats, nil
}
