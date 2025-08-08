// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

func init() {
	performance.Register(performance.MetricTypeCgroupMemory, performance.PartialNewContinuousPointCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewCgroupMemoryCollector(logger, config)
		},
	))
}

// Compile-time interface check
var _ performance.PointCollector = (*CgroupMemoryCollector)(nil)

// CgroupMemoryCollector collects memory metrics from cgroup filesystems
//
// This collector reads memory usage, limits, and pressure information from
// cgroup controllers to monitor container memory consumption and detect
// memory pressure situations.
//
// Supports both cgroup v1 and v2 hierarchies.
//
// Example output format:
//
//	[]CgroupMemoryStats{
//	    {
//	        ContainerID:   "abc123def456",                      // First 12+ chars of container ID
//	        CgroupPath:    "/sys/fs/cgroup/memory/docker/abc123def456",
//	        UsageBytes:    1610612736,                          // Current memory usage in bytes
//	        LimitBytes:    2147483648,                          // Memory limit (math.MaxUint64 = unlimited)
//	        MaxUsageBytes: 1879048192,                          // Peak memory usage (v1 only)
//	        RSS:           1073741824,                          // Resident set size
//	        Cache:         536870912,                           // Page cache memory
//	        MappedFile:    134217728,                           // Memory-mapped files
//	        Swap:          0,                                   // Swap usage
//	        FailCount:     5,                                   // Number of times limit was hit
//	        OOMKillCount:  0,                                   // Number of OOM kills
//	        UnderOOM:      false,                               // Currently under OOM condition
//	        UsagePercent:  75.0,                                // Usage as percentage of limit
//	        CachePercent:  33.3,                                // Cache as percentage of usage
//	    },
//	}
type CgroupMemoryCollector struct {
	performance.BaseCollector
	cgroupPath string
	discovery  *ContainerDiscovery
}

// NewCgroupMemoryCollector creates a new cgroup memory collector
func NewCgroupMemoryCollector(logger logr.Logger, config performance.CollectionConfig) (*CgroupMemoryCollector, error) {
	if err := config.Validate(performance.ValidateOptions{RequireHostCgroupPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    true,
		SupportsContinuous: false,
		RequiresRoot:       false,
		RequiresEBPF:       false,
		MinKernelVersion:   "2.6.24", // When cgroups were introduced
	}

	return &CgroupMemoryCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeCgroupMemory,
			"Cgroup Memory Statistics Collector",
			logger,
			config,
			capabilities,
		),
		cgroupPath: config.HostCgroupPath,
		discovery:  NewContainerDiscovery(config.HostCgroupPath),
	}, nil
}

// Collect performs a one-shot collection of cgroup memory statistics
func (c *CgroupMemoryCollector) Collect(ctx context.Context) (any, error) {
	// Detect cgroup version
	version, err := c.discovery.DetectCgroupVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to detect cgroup version: %w", err)
	}

	c.Logger().V(2).Info("Detected cgroup version", "version", version)

	// Discover containers
	containers, err := c.discovery.DiscoverContainers("memory", version)
	if err != nil {
		return nil, fmt.Errorf("failed to discover containers: %w", err)
	}

	// Collect stats for each container
	var stats []performance.CgroupMemoryStats
	for _, container := range containers {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}
		
		stat, err := c.collectContainerStats(container, version)
		if err != nil {
			// Log error but continue with other containers
			c.Logger().V(1).Info("Failed to collect stats for container",
				"containerID", container.ID,
				"error", err)
			continue
		}
		stats = append(stats, stat)
	}

	return stats, nil
}

// collectContainerStats collects memory stats for a single container
func (c *CgroupMemoryCollector) collectContainerStats(container ContainerPath, version int) (performance.CgroupMemoryStats, error) {
	stats := performance.CgroupMemoryStats{
		ContainerID: container.ID,
		CgroupPath:  container.CgroupPath,
	}

	if version == 1 {
		// Cgroup v1: Read from memory controller files
		if err := c.readCgroupV1Stats(&stats, container); err != nil {
			return stats, err
		}
	} else {
		// Cgroup v2: Read from unified hierarchy
		if err := c.readCgroupV2Stats(&stats, container); err != nil {
			return stats, err
		}
	}

	// Calculate derived metrics
	if stats.LimitBytes > 0 && stats.LimitBytes < math.MaxUint64/2 {
		stats.UsagePercent = float64(stats.UsageBytes) / float64(stats.LimitBytes) * 100
	}
	if stats.UsageBytes > 0 {
		stats.CachePercent = float64(stats.Cache) / float64(stats.UsageBytes) * 100
	}

	return stats, nil
}

// readCgroupV1Stats reads memory stats from cgroup v1 files
//
// Error handling strategy:
// - memory.stat is critical - returns error if unavailable (primary data source)
// - Other files are optional for graceful degradation
// - This ensures we at least have basic memory breakdown if stat file exists
func (c *CgroupMemoryCollector) readCgroupV1Stats(stats *performance.CgroupMemoryStats, container ContainerPath) error {
	// Read memory.stat for detailed breakdown
	statPath := filepath.Join(container.CgroupPath, "memory.stat")
	if data, err := os.ReadFile(statPath); err == nil {
		c.parseMemoryStat(string(data), stats)
	} else {
		// memory.stat is critical - if we can't read it, skip this container
		return fmt.Errorf("failed to read memory.stat: %w", err)
	}

	// Read current usage
	usagePath := filepath.Join(container.CgroupPath, "memory.usage_in_bytes")
	if data, err := os.ReadFile(usagePath); err == nil {
		if usage, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
			stats.UsageBytes = usage
		}
	}

	// Read memory limit
	limitPath := filepath.Join(container.CgroupPath, "memory.limit_in_bytes")
	if data, err := os.ReadFile(limitPath); err == nil {
		if limit, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
			stats.LimitBytes = limit
		}
	}

	// Read max usage (high water mark)
	maxUsagePath := filepath.Join(container.CgroupPath, "memory.max_usage_in_bytes")
	if data, err := os.ReadFile(maxUsagePath); err == nil {
		if maxUsage, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
			stats.MaxUsageBytes = maxUsage
		}
	}

	// Read fail count
	failcntPath := filepath.Join(container.CgroupPath, "memory.failcnt")
	if data, err := os.ReadFile(failcntPath); err == nil {
		if failcnt, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
			stats.FailCount = failcnt
		}
	}

	// Read OOM control
	oomPath := filepath.Join(container.CgroupPath, "memory.oom_control")
	if data, err := os.ReadFile(oomPath); err == nil {
		c.parseOOMControl(string(data), stats)
	}

	return nil
}

// readCgroupV2Stats reads memory stats from cgroup v2 files
//
// Error handling strategy:
// - memory.stat is critical - returns error if unavailable (primary data source)
// - Other files (memory.current, memory.max, memory.events) are optional
// - This ensures consistency with v1 behavior while leveraging v2 features when available
func (c *CgroupMemoryCollector) readCgroupV2Stats(stats *performance.CgroupMemoryStats, container ContainerPath) error {
	// Read memory.stat for detailed breakdown
	statPath := filepath.Join(container.CgroupPath, "memory.stat")
	if data, err := os.ReadFile(statPath); err == nil {
		c.parseCgroupV2MemoryStat(string(data), stats)
	} else {
		return fmt.Errorf("failed to read memory.stat: %w", err)
	}

	// Read current usage
	currentPath := filepath.Join(container.CgroupPath, "memory.current")
	if data, err := os.ReadFile(currentPath); err == nil {
		if usage, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
			stats.UsageBytes = usage
		}
	}

	// Read memory limit
	maxPath := filepath.Join(container.CgroupPath, "memory.max")
	if data, err := os.ReadFile(maxPath); err == nil {
		limitStr := strings.TrimSpace(string(data))
		if limitStr != "max" {
			if limit, err := strconv.ParseUint(limitStr, 10, 64); err == nil {
				stats.LimitBytes = limit
			}
		} else {
			// "max" means no limit
			stats.LimitBytes = math.MaxUint64
		}
	}

	// Read memory events (includes OOM kill count)
	eventsPath := filepath.Join(container.CgroupPath, "memory.events")
	if data, err := os.ReadFile(eventsPath); err == nil {
		c.parseMemoryEvents(string(data), stats)
	}

	// Note: v2 doesn't have max_usage_in_bytes equivalent

	return nil
}

// parseMemoryStat parses cgroup v1 memory.stat file
func (c *CgroupMemoryCollector) parseMemoryStat(data string, stats *performance.CgroupMemoryStats) {
	lines := strings.Split(strings.TrimSpace(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		switch fields[0] {
		case "rss":
			stats.RSS = value
		case "cache":
			stats.Cache = value
		case "mapped_file":
			stats.MappedFile = value
		case "swap":
			stats.Swap = value
		case "active_anon":
			stats.ActiveAnon = value
		case "inactive_anon":
			stats.InactiveAnon = value
		case "active_file":
			stats.ActiveFile = value
		case "inactive_file":
			stats.InactiveFile = value
		}
	}
}

// parseCgroupV2MemoryStat parses cgroup v2 memory.stat file
func (c *CgroupMemoryCollector) parseCgroupV2MemoryStat(data string, stats *performance.CgroupMemoryStats) {
	lines := strings.Split(strings.TrimSpace(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		switch fields[0] {
		case "anon":
			// In v2, anon is equivalent to RSS
			stats.RSS = value
		case "file":
			// In v2, file is equivalent to cache
			stats.Cache = value
		case "file_mapped":
			stats.MappedFile = value
		case "swap":
			stats.Swap = value
		case "active_anon":
			stats.ActiveAnon = value
		case "inactive_anon":
			stats.InactiveAnon = value
		case "active_file":
			stats.ActiveFile = value
		case "inactive_file":
			stats.InactiveFile = value
		}
	}
}

// parseOOMControl parses cgroup v1 memory.oom_control file
func (c *CgroupMemoryCollector) parseOOMControl(data string, stats *performance.CgroupMemoryStats) {
	lines := strings.Split(strings.TrimSpace(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		switch fields[0] {
		case "under_oom":
			if fields[1] == "1" {
				stats.UnderOOM = true
			}
		case "oom_kill":
			if value, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				stats.OOMKillCount = value
			}
		}
	}
}

// parseMemoryEvents parses cgroup v2 memory.events file
func (c *CgroupMemoryCollector) parseMemoryEvents(data string, stats *performance.CgroupMemoryStats) {
	lines := strings.Split(strings.TrimSpace(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		switch fields[0] {
		case "max":
			// Number of times memory.max was hit
			stats.FailCount = value
		case "oom_kill":
			stats.OOMKillCount = value
		}
	}
}
