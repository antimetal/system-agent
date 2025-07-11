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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

const (
	// Default number of top processes to return
	defaultTopProcessCount = 20
)

// ProcessCollector collects per-process statistics from /proc/[pid]/*
// Returns top N processes by CPU usage with memory and runtime stats.
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html#process-specific-subdirectories
type ProcessCollector struct {
	performance.BaseCollector
	procPath       string
	topProcesses   int
	lastCPUTimes   map[int32]*processCPUTime
	lastUpdateTime time.Time
	procUtils      *procUtils
}

// processCPUTime stores CPU time data for calculating CPU percentage
type processCPUTime struct {
	totalTime uint64
	timestamp time.Time
}

func NewProcessCollector(logger logr.Logger, config performance.CollectionConfig) *ProcessCollector {
	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    true,
		SupportsContinuous: true,
		RequiresRoot:       false,
		RequiresEBPF:       false,
		MinKernelVersion:   "2.6.0",
	}

	// Default to 20 top processes if not configured
	topProcesses := defaultTopProcessCount

	return &ProcessCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeProcess,
			"Process Statistics Collector",
			logger,
			config,
			capabilities,
		),
		procPath:     config.HostProcPath,
		topProcesses: topProcesses,
		lastCPUTimes: make(map[int32]*processCPUTime),
		procUtils:    newProcUtils(config.HostProcPath),
	}
}

func (c *ProcessCollector) Collect(ctx context.Context) (any, error) {
	processes, err := c.collectProcessStats(ctx)
	if err != nil {
		return nil, err
	}

	// Sort by CPU usage and return top N
	sort.Slice(processes, func(i, j int) bool {
		return processes[i].CPUPercent > processes[j].CPUPercent
	})

	if len(processes) > c.topProcesses {
		processes = processes[:c.topProcesses]
	}

	// Update last CPU times for next calculation
	c.updateLastCPUTimes(processes)

	c.Logger().V(1).Info("Collected process statistics", "processes", len(processes), "top_count", c.topProcesses)
	return processes, nil
}

func (c *ProcessCollector) collectProcessStats(ctx context.Context) ([]*performance.ProcessStats, error) {
	entries, err := os.ReadDir(c.procPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", c.procPath, err)
	}

	var processes []*performance.ProcessStats
	currentTime := time.Now()
	timeDelta := currentTime.Sub(c.lastUpdateTime).Seconds()
	if timeDelta <= 0 {
		timeDelta = 1.0 // Avoid division by zero on first run
	}

	for _, entry := range entries {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return processes, ctx.Err()
		default:
		}

		// Skip non-directories and non-PID entries
		if !entry.IsDir() {
			continue
		}

		pid, err := strconv.ParseInt(entry.Name(), 10, 32)
		if err != nil {
			continue // Not a PID directory
		}

		stats, err := c.collectProcessData(int32(pid), timeDelta)
		if err != nil {
			// Process might have disappeared, continue with others
			c.Logger().V(2).Info("Failed to collect process data", "pid", pid, "error", err)
			continue
		}

		processes = append(processes, stats)
	}

	c.lastUpdateTime = currentTime
	return processes, nil
}

func (c *ProcessCollector) collectProcessData(pid int32, timeDelta float64) (*performance.ProcessStats, error) {
	stats := &performance.ProcessStats{
		PID: pid,
	}

	// Read /proc/[pid]/stat for CPU and memory info
	statPath := filepath.Join(c.procPath, strconv.Itoa(int(pid)), "stat")
	statData, err := os.ReadFile(statPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read stat: %w", err)
	}

	if err := c.parseStat(stats, string(statData), timeDelta); err != nil {
		return nil, fmt.Errorf("failed to parse stat: %w", err)
	}

	// Read /proc/[pid]/comm for clean command name
	commPath := filepath.Join(c.procPath, strconv.Itoa(int(pid)), "comm")
	if commData, err := os.ReadFile(commPath); err == nil {
		stats.Command = strings.TrimSpace(string(commData))
	}

	// Read /proc/[pid]/status for additional details
	statusPath := filepath.Join(c.procPath, strconv.Itoa(int(pid)), "status")
	if statusData, err := os.ReadFile(statusPath); err == nil {
		c.parseStatus(stats, string(statusData))
	}

	// Count file descriptors
	fdPath := filepath.Join(c.procPath, strconv.Itoa(int(pid)), "fd")
	if entries, err := os.ReadDir(fdPath); err == nil {
		stats.NumFds = int32(len(entries))
	}

	// Read memory stats from smaps_rollup if available
	smapsPath := filepath.Join(c.procPath, strconv.Itoa(int(pid)), "smaps_rollup")
	if smapsData, err := os.ReadFile(smapsPath); err == nil {
		c.parseSmapsRollup(stats, string(smapsData))
	}

	return stats, nil
}

// parseStat parses /proc/[pid]/stat file contents
// The command field (field 2) can contain spaces and is enclosed in parentheses.
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html#proc-pid-stat
func (c *ProcessCollector) parseStat(stats *performance.ProcessStats, statData string, timeDelta float64) error {
	// The command field might contain spaces and is enclosed in parentheses
	// Find the last ')' to properly split the fields
	lastParen := strings.LastIndex(statData, ")")
	if lastParen == -1 {
		return fmt.Errorf("invalid stat format: no closing parenthesis")
	}

	// Extract command (field 2) - between first '(' and last ')'
	firstParen := strings.Index(statData, "(")
	if firstParen != -1 && lastParen > firstParen {
		stats.Command = statData[firstParen+1 : lastParen]
	}

	// Split remaining fields after the last ')'
	fieldsStr := strings.TrimSpace(statData[lastParen+1:])
	fields := strings.Fields(fieldsStr)

	if len(fields) < 21 {
		return fmt.Errorf("invalid stat format: insufficient fields")
	}

	// Parse fields (0-indexed after command)
	// Field 0: state
	stats.State = fields[0]

	// Field 1: ppid
	if ppid, err := strconv.ParseInt(fields[1], 10, 32); err == nil {
		stats.PPID = int32(ppid)
	}

	// Field 2: pgrp
	if pgid, err := strconv.ParseInt(fields[2], 10, 32); err == nil {
		stats.PGID = int32(pgid)
	}

	// Field 3: session
	if sid, err := strconv.ParseInt(fields[3], 10, 32); err == nil {
		stats.SID = int32(sid)
	}

	// Field 7: minflt
	if minflt, err := strconv.ParseUint(fields[7], 10, 64); err == nil {
		stats.MinorFaults = minflt
	}

	// Field 9: majflt
	if majflt, err := strconv.ParseUint(fields[9], 10, 64); err == nil {
		stats.MajorFaults = majflt
	}

	// Fields 11,12: utime, stime (in clock ticks)
	var utime, stime uint64
	if u, err := strconv.ParseUint(fields[11], 10, 64); err == nil {
		utime = u
	}
	if s, err := strconv.ParseUint(fields[12], 10, 64); err == nil {
		stime = s
	}
	stats.CPUTime = utime + stime

	// Calculate CPU percentage from jiffies
	if lastCPU, exists := c.lastCPUTimes[stats.PID]; exists && timeDelta > 0 {
		cpuDelta := float64(stats.CPUTime - lastCPU.totalTime)
		// Get actual USER_HZ value from system
		userHZ, err := c.procUtils.GetUserHZ()
		if err != nil {
			userHZ = 100 // Fallback to standard value
		}
		stats.CPUPercent = (cpuDelta / float64(userHZ)) / timeDelta * 100.0
	}

	// Field 15: priority
	if prio, err := strconv.ParseInt(fields[15], 10, 32); err == nil {
		stats.Priority = int32(prio)
	}

	// Field 16: nice
	if nice, err := strconv.ParseInt(fields[16], 10, 32); err == nil {
		stats.Nice = int32(nice)
	}

	// Field 17: num_threads
	if threads, err := strconv.ParseInt(fields[17], 10, 32); err == nil {
		stats.Threads = int32(threads)
	}

	// Field 20: vsize (virtual memory size in bytes)
	if vsize, err := strconv.ParseUint(fields[20], 10, 64); err == nil {
		stats.MemoryVSZ = vsize
	}

	// Field 21: rss (resident set size in pages)
	if rss, err := strconv.ParseUint(fields[21], 10, 64); err == nil {
		// Get actual page size from system
		pageSize, err := c.procUtils.GetPageSize()
		if err != nil {
			pageSize = 4096 // Fallback to standard value
		}
		stats.MemoryRSS = rss * uint64(pageSize)
	}

	// Field 19: starttime (in clock ticks since boot)
	if len(fields) > 19 {
		if starttime, err := strconv.ParseUint(fields[19], 10, 64); err == nil {
			// Convert to actual start time using boot time
			bootTime, err := c.procUtils.GetBootTime()
			if err == nil {
				// Get actual USER_HZ value from system
				userHZ, err := c.procUtils.GetUserHZ()
				if err != nil {
					userHZ = 100 // Fallback to standard value
				}
				stats.StartTime = bootTime.Add(time.Duration(starttime) * time.Second / time.Duration(userHZ))
			}
		}
	}

	return nil
}

// parseStatus parses /proc/[pid]/status file contents
//
// The status file provides human-readable information about the process.
// Each line has the format: "Field:\tvalue"
//
// Key fields we parse:
// - Threads: Number of threads in process
// - voluntary_ctxt_switches: Number of voluntary context switches
// - nonvoluntary_ctxt_switches: Number of involuntary context switches
//
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html#proc-pid-status
func (c *ProcessCollector) parseStatus(stats *performance.ProcessStats, statusData string) {
	scanner := bufio.NewScanner(strings.NewReader(statusData))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		switch parts[0] {
		case "Threads:":
			if threads, err := strconv.ParseInt(parts[1], 10, 32); err == nil {
				stats.NumThreads = int32(threads)
			}
		case "voluntary_ctxt_switches:":
			if vctx, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
				stats.VoluntaryCtxt = vctx
			}
		case "nonvoluntary_ctxt_switches:":
			if nvctx, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
				stats.InvoluntaryCtxt = nvctx
			}
		}
	}
}

// parseSmapsRollup parses /proc/[pid]/smaps_rollup file contents
//
// The smaps_rollup file provides a summary of memory mappings for the process.
// It's more efficient than reading the full /proc/[pid]/smaps file.
//
// Key fields we parse:
// - Pss: Proportional Set Size - memory shared with other processes, divided by
//        the number of processes sharing it. More accurate than RSS for multi-process apps.
// - Private_Clean + Private_Dirty: Unique Set Size (USS) - memory unique to this process
//
// All values are in kB and need to be converted to bytes.
//
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html#proc-pid-smaps-rollup
func (c *ProcessCollector) parseSmapsRollup(stats *performance.ProcessStats, smapsData string) {
	scanner := bufio.NewScanner(strings.NewReader(smapsData))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}

		switch parts[0] {
		case "Pss:":
			if pss, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
				stats.MemoryPSS = pss * 1024 // Convert from kB to bytes
			}
		case "Private_Clean:", "Private_Dirty:":
			// USS is the sum of private clean and dirty pages
			if uss, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
				stats.MemoryUSS += uss * 1024 // Convert from kB to bytes
			}
		}
	}
}


func (c *ProcessCollector) updateLastCPUTimes(processes []*performance.ProcessStats) {
	// Clean up old entries
	newTimes := make(map[int32]*processCPUTime)

	for _, proc := range processes {
		newTimes[proc.PID] = &processCPUTime{
			totalTime: proc.CPUTime,
			timestamp: time.Now(),
		}
	}

	c.lastCPUTimes = newTimes
}
