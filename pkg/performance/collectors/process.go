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
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/procutils"
	"github.com/go-logr/logr"
)

// Compile-time interface check
var _ performance.ContinuousCollector = (*ProcessCollector)(nil)

const (
	defaultTopProcessCount = 20
)

// ProcessCollector collects per-process statistics from /proc/[pid]/*
// Emits, for each iteration, the top N processes by CPU usage with memory and runtime stats.
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html#process-specific-subdirectories
type ProcessCollector struct {
	performance.BaseContinuousCollector
	procPath     string
	topProcesses int
	interval     time.Duration
	procUtils    *procutils.ProcUtils

	// State tracking for CPU percentage calculations
	mu             sync.RWMutex
	lastCPUTimes   map[int32]*processCPUTime
	lastUpdateTime time.Time
}

type processCPUTime struct {
	totalTime uint64
	timestamp time.Time
}

func NewProcessCollector(logger logr.Logger, config performance.CollectionConfig) (*ProcessCollector, error) {
	// Validate paths are absolute
	if !filepath.IsAbs(config.HostProcPath) {
		return nil, fmt.Errorf("HostProcPath must be an absolute path, got: %q", config.HostProcPath)
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    false,
		SupportsContinuous: true,
		RequiresRoot:       false,
		RequiresEBPF:       false,
		MinKernelVersion:   "2.6.0",
	}

	// Use configured value or default
	topProcesses := config.TopProcessCount
	if topProcesses <= 0 {
		topProcesses = defaultTopProcessCount
	}

	interval := config.Interval
	if interval <= 0 {
		interval = 1 * time.Second
	}

	return &ProcessCollector{
		BaseContinuousCollector: performance.NewBaseContinuousCollector(
			performance.MetricTypeProcess,
			"Process Statistics Collector",
			logger,
			config,
			capabilities,
		),
		procPath:     config.HostProcPath,
		topProcesses: topProcesses,
		interval:     interval,
		lastCPUTimes: make(map[int32]*processCPUTime),
		procUtils:    procutils.New(config.HostProcPath),
	}, nil
}

func (c *ProcessCollector) Start(ctx context.Context) (<-chan any, error) {
	c.SetStatus(performance.CollectorStatusActive)

	// Take initial snapshot of minimal stats to establish baseline
	initial, err := c.collectMinimalStats(ctx)
	if err != nil {
		c.SetStatus(performance.CollectorStatusFailed)
		return nil, fmt.Errorf("failed to collect initial process stats: %w", err)
	}

	c.mu.Lock()
	c.updateLastCPUTimesFromMinimal(initial)
	c.lastUpdateTime = time.Now()
	c.mu.Unlock()

	dataChan := make(chan any)
	go c.runCollection(ctx, dataChan)
	return dataChan, nil
}

func (c *ProcessCollector) Stop() error {
	c.SetStatus(performance.CollectorStatusDisabled)
	return nil
}

func (c *ProcessCollector) runCollection(ctx context.Context, dataChan chan<- any) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	defer close(dataChan)

	for {
		select {
		case <-ctx.Done():
			c.SetStatus(performance.CollectorStatusDisabled)
			return
		case <-ticker.C:
			processes, err := c.collectWithDeltas(ctx)
			if err != nil {
				c.Logger().Error(err, "Failed to collect process stats")
				c.SetError(err)
				continue
			}

			select {
			case dataChan <- processes:
			case <-ctx.Done():
				c.SetStatus(performance.CollectorStatusDisabled)
				return
			}
		}
	}
}

// minimalProcessStats holds just enough data to calculate CPU% and sort
type minimalProcessStats struct {
	pid        int32
	cpuTime    uint64
	cpuPercent float64
}

func (c *ProcessCollector) collectWithDeltas(ctx context.Context) ([]*performance.ProcessStats, error) {
	// Phase 1: Collect minimal data for all processes (just enough for CPU% calculation)
	minimalStats, err := c.collectMinimalStats(ctx)
	if err != nil {
		return nil, err
	}

	// Update CPU times for ALL processes before sorting
	c.mu.Lock()
	c.updateLastCPUTimesFromMinimal(minimalStats)
	c.mu.Unlock()

	// Sort by CPU usage
	sort.Slice(minimalStats, func(i, j int) bool {
		return minimalStats[i].cpuPercent > minimalStats[j].cpuPercent
	})

	// Take top N
	topN := minimalStats
	if len(topN) > c.topProcesses {
		topN = topN[:c.topProcesses]
	}

	// Phase 2: Collect full details only for top N processes
	processes := make([]*performance.ProcessStats, 0, len(topN))
	for _, minimal := range topN {
		full, err := c.collectFullProcessData(minimal.pid, minimal)
		if err != nil {
			// Process might have disappeared between phases
			c.Logger().V(2).Info("Failed to collect full data for top process", "pid", minimal.pid, "error", err)
			continue
		}
		processes = append(processes, full)
	}

	c.Logger().V(1).Info("Collected process statistics",
		"total_processes", len(minimalStats),
		"returned_processes", len(processes),
		"top_count", c.topProcesses)
	return processes, nil
}

// collectMinimalStats reads only /proc/[pid]/stat for CPU time calculation
//
// Error handling strategy:
// - /proc directory listing is critical - returns error if unavailable
// - Individual /proc/[pid]/stat files are optional - skips processes that disappear
// - Malformed stat lines are skipped with logging
// - Never panics - all errors are returned to caller
func (c *ProcessCollector) collectMinimalStats(ctx context.Context) ([]*minimalProcessStats, error) {
	entries, err := os.ReadDir(c.procPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", c.procPath, err)
	}

	var minimalStats []*minimalProcessStats
	currentTime := time.Now()

	// Get time delta and lastCPUTimes with read lock
	c.mu.RLock()
	timeDelta := currentTime.Sub(c.lastUpdateTime).Seconds()
	lastCPUTimesCopy := make(map[int32]*processCPUTime, len(c.lastCPUTimes))
	for k, v := range c.lastCPUTimes {
		lastCPUTimesCopy[k] = v
	}
	c.mu.RUnlock()

	if timeDelta <= 0 {
		timeDelta = 1.0 // Avoid division by zero on first run
	}

	// Pre-allocate with reasonable capacity
	minimalStats = make([]*minimalProcessStats, 0, 256)

	for _, entry := range entries {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return minimalStats, ctx.Err()
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

		// Read only /proc/[pid]/stat - the minimal data we need
		minimal, err := c.readMinimalStats(int32(pid), timeDelta, lastCPUTimesCopy)
		if err != nil {
			// Process might have disappeared, continue with others
			c.Logger().V(3).Info("Failed to read minimal stats", "pid", pid, "error", err)
			continue
		}

		minimalStats = append(minimalStats, minimal)
	}

	c.lastUpdateTime = currentTime
	return minimalStats, nil
}

// readMinimalStats reads only /proc/[pid]/stat for CPU calculation
func (c *ProcessCollector) readMinimalStats(pid int32, timeDelta float64, lastCPUTimes map[int32]*processCPUTime) (*minimalProcessStats, error) {
	statPath := filepath.Join(c.procPath, strconv.Itoa(int(pid)), "stat")
	statData, err := os.ReadFile(statPath)
	if err != nil {
		return nil, err
	}

	minimal := &minimalProcessStats{pid: pid}

	// Parse only what we need from stat
	statStr := string(statData)

	// Find the last ')' to skip the command field entirely
	lastParen := strings.LastIndex(statStr, ")")
	if lastParen == -1 {
		return nil, fmt.Errorf("invalid stat format")
	}

	// Split fields after command - we don't need the command itself
	fieldsStr := strings.TrimSpace(statStr[lastParen+1:])
	fields := strings.Fields(fieldsStr)
	if len(fields) < 13 { // We need at least up to stime (field 12)
		return nil, fmt.Errorf("insufficient fields in stat")
	}

	// Get utime (field 11) and stime (field 12)
	var utime, stime uint64
	if u, err := strconv.ParseUint(fields[11], 10, 64); err == nil {
		utime = u
	}
	if s, err := strconv.ParseUint(fields[12], 10, 64); err == nil {
		stime = s
	}
	minimal.cpuTime = utime + stime

	// Calculate CPU percentage
	if lastCPU, exists := lastCPUTimes[pid]; exists && timeDelta > 0 {
		cpuDelta := float64(minimal.cpuTime - lastCPU.totalTime)
		userHZ, err := c.procUtils.GetUserHZ()
		if err != nil {
			userHZ = 100
		}
		minimal.cpuPercent = (cpuDelta / float64(userHZ)) / timeDelta * 100.0
	}

	return minimal, nil
}

// collectFullProcessData collects all process details for a single process
//
// Error handling strategy:
// - /proc/[pid]/stat is critical - returns error if unavailable
// - /proc/[pid]/status is critical - returns error if unavailable
// - /proc/[pid]/cmdline is optional - logs warning but continues if unavailable
// - /proc/[pid]/exe is optional - logs warning but continues if unavailable
// - Never panics - all errors are returned to caller
func (c *ProcessCollector) collectFullProcessData(pid int32, minimal *minimalProcessStats) (*performance.ProcessStats, error) {
	stats := &performance.ProcessStats{
		PID:        pid,
		CPUTime:    minimal.cpuTime,
		CPUPercent: minimal.cpuPercent,
	}

	// Re-read /proc/[pid]/stat for full parsing (process might have changed slightly)
	statPath := filepath.Join(c.procPath, strconv.Itoa(int(pid)), "stat")
	statData, err := os.ReadFile(statPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read stat: %w", err)
	}

	// Parse the full stat data (we already have CPU data from minimal)
	if err := c.parseStatFull(stats, string(statData)); err != nil {
		return nil, fmt.Errorf("failed to parse stat: %w", err)
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

// parseStatFull parses /proc/[pid]/stat for everything except CPU time (already calculated)
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html#id10
//
// Error handling strategy:
// - Returns error if unable to find command boundaries or parse fields
// - Validates field count to ensure correct format
// - Handles division by zero for RSS percentage calculation
// - Never panics - all errors are returned to caller
func (c *ProcessCollector) parseStatFull(stats *performance.ProcessStats, statData string) error {
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

	// Skip CPU calculation - we already have it from minimal stats

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
		pageSize, err := c.procUtils.GetPageSize()
		if err != nil {
			pageSize = 4096 // Safe assumption if we can't determine it
		}
		stats.MemoryRSS = rss * uint64(pageSize)
	}

	// Field 19: starttime (in clock ticks since boot)
	if len(fields) > 19 {
		if starttime, err := strconv.ParseUint(fields[19], 10, 64); err == nil {
			bootTime, err := c.procUtils.GetBootTime()
			if err == nil {
				userHZ, err := c.procUtils.GetUserHZ()
				if err != nil {
					userHZ = 100 // Safe assumption if we can't determine it
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
//   - Pss: Proportional Set Size - memory shared with other processes, divided by
//     the number of processes sharing it. More accurate than RSS for multi-process apps.
//   - Private_Clean + Private_Dirty: Unique Set Size (USS) - memory unique to this process
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

// updateLastCPUTimesFromMinimal updates the lastCPUTimes map from minimal stats. Must be called with c.mu held.
// This tracks CPU times for ALL processes to ensure accurate CPU percentage calculations.
func (c *ProcessCollector) updateLastCPUTimesFromMinimal(minimalStats []*minimalProcessStats) {
	now := time.Now()
	newTimes := make(map[int32]*processCPUTime, len(minimalStats))

	for _, minimal := range minimalStats {
		newTimes[minimal.pid] = &processCPUTime{
			totalTime: minimal.cpuTime,
			timestamp: now,
		}
	}

	if len(newTimes) > 20000 {
		c.Logger().V(1).Info("Large number of processes being tracked",
			"count", len(newTimes))
	}

	c.lastCPUTimes = newTimes
}
