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

	"github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

func init() {
	performance.Register(performance.MetricTypeCgroupCPU, performance.PartialNewContinuousPointCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewCgroupCPUCollector(logger, config)
		},
	))
}

// Compile-time interface check
var _ performance.PointCollector = (*CgroupCPUCollector)(nil)

// CgroupCPUCollector collects CPU metrics from cgroup filesystems
//
// This collector reads CPU usage and throttling information from cgroup
// controllers to monitor container resource consumption and contention.
//
// Supports both cgroup v1 and v2 hierarchies.
//
// Example output format:
//
//	[]CgroupCPUStats{
//	    {
//	        ContainerID:     "abc123def456",                    // First 12+ chars of container ID
//	        CgroupPath:      "/sys/fs/cgroup/cpu/docker/abc123def456",
//	        UsageNanos:      123456789000,                      // Total CPU time in nanoseconds
//	        NrPeriods:       1000,                              // Number of enforcement periods
//	        NrThrottled:     50,                                // Periods when throttled
//	        ThrottledTime:   5000000000,                        // Time throttled (nanoseconds)
//	        CpuShares:       1024,                              // CPU shares (relative weight)
//	        CpuQuotaUs:      50000,                             // CPU quota in microseconds (-1 = unlimited)
//	        CpuPeriodUs:     100000,                            // CPU period in microseconds
//	        ThrottlePercent: 5.0,                               // Percentage of periods throttled
//	    },
//	}
type CgroupCPUCollector struct {
	performance.BaseCollector
	cgroupPath string
	discovery  *containers.Discovery
}

// NewCgroupCPUCollector creates a new cgroup CPU collector
func NewCgroupCPUCollector(logger logr.Logger, config performance.CollectionConfig) (*CgroupCPUCollector, error) {
	if err := config.Validate(performance.ValidateOptions{RequireHostSysPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   false,
		RequiredCapabilities: nil,      // Cgroup files are typically world-readable
		MinKernelVersion:     "2.6.24", // When cgroups were introduced
	}

	// Construct cgroup path from sys path
	cgroupPath := filepath.Join(config.HostSysPath, "fs", "cgroup")

	return &CgroupCPUCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeCgroupCPU,
			"Cgroup CPU Statistics Collector",
			logger,
			config,
			capabilities,
		),
		cgroupPath: cgroupPath,
		discovery:  containers.NewDiscovery(cgroupPath),
	}, nil
}

// Collect performs a one-shot collection of cgroup CPU statistics
func (c *CgroupCPUCollector) Collect(ctx context.Context) (any, error) {
	// Detect cgroup version
	version, err := c.discovery.DetectCgroupVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to detect cgroup version: %w", err)
	}

	c.Logger().V(2).Info("Detected cgroup version", "version", version)

	// Discover containers
	containers, err := c.discovery.DiscoverContainers("cpu", version)
	if err != nil {
		return nil, fmt.Errorf("failed to discover containers: %w", err)
	}

	// Collect stats for each container
	var stats []performance.CgroupCPUStats
	for _, container := range containers {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		stat, err := c.collectContainerStats(container)
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

// collectContainerStats collects CPU stats for a single container
func (c *CgroupCPUCollector) collectContainerStats(container containers.Container) (performance.CgroupCPUStats, error) {
	stats := performance.CgroupCPUStats{
		ContainerID: container.ID,
		CgroupPath:  container.CgroupPath,
	}

	if container.CgroupVersion == 1 {
		// Cgroup v1: Read from separate cpu and cpuacct controllers
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
	if stats.NrPeriods > 0 {
		stats.ThrottlePercent = float64(stats.NrThrottled) / float64(stats.NrPeriods) * 100
	}

	return stats, nil
}

// readCgroupV1Stats reads CPU stats from cgroup v1 files
//
// Error handling strategy:
// - All files are treated as optional - we read what's available
// - Missing files are silently ignored to support partial data collection
// - This allows graceful degradation when some cgroup files are unavailable
func (c *CgroupCPUCollector) readCgroupV1Stats(stats *performance.CgroupCPUStats, container containers.Container) error {
	// Read CPU throttling stats
	cpuStatPath := filepath.Join(container.CgroupPath, "cpu.stat")
	if data, err := os.ReadFile(cpuStatPath); err == nil {
		c.parseCPUStat(string(data), stats)
	}

	// Read CPU usage
	// Note: In v1, cpu and cpuacct are separate controllers. The container discovery
	// finds containers in the cpu controller, but usage stats are in cpuacct controller.
	// We construct the cpuacct path by replacing /cpu/ with /cpuacct/ in the path.
	cpuacctPath := strings.Replace(container.CgroupPath, "/cpu/", "/cpuacct/", 1)
	usagePath := filepath.Join(cpuacctPath, "cpuacct.usage")
	if data, err := os.ReadFile(usagePath); err == nil {
		if usage, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
			stats.UsageNanos = usage
		}
	}

	// Read CPU shares
	sharesPath := filepath.Join(container.CgroupPath, "cpu.shares")
	if data, err := os.ReadFile(sharesPath); err == nil {
		if shares, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
			stats.CpuShares = shares
		}
	}

	// Read CPU quota and period
	quotaPath := filepath.Join(container.CgroupPath, "cpu.cfs_quota_us")
	if data, err := os.ReadFile(quotaPath); err == nil {
		if quota, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
			stats.CpuQuotaUs = quota
		}
	}

	periodPath := filepath.Join(container.CgroupPath, "cpu.cfs_period_us")
	if data, err := os.ReadFile(periodPath); err == nil {
		if period, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
			stats.CpuPeriodUs = period
		}
	}

	return nil
}

// readCgroupV2Stats reads CPU stats from cgroup v2 files
//
// Error handling strategy:
// - cpu.stat is critical - returns error if unavailable (contains usage data)
// - Other files (cpu.max, cpu.weight) are optional and silently ignored if missing
// - This ensures we have at least basic usage data for v2 containers
func (c *CgroupCPUCollector) readCgroupV2Stats(stats *performance.CgroupCPUStats, container containers.Container) error {
	// Read cpu.stat which contains usage and throttling info
	cpuStatPath := filepath.Join(container.CgroupPath, "cpu.stat")
	if data, err := os.ReadFile(cpuStatPath); err == nil {
		c.parseCgroupV2CPUStat(string(data), stats)
	} else {
		return fmt.Errorf("failed to read cpu.stat: %w", err)
	}

	// Read cpu.max for quota and period
	cpuMaxPath := filepath.Join(container.CgroupPath, "cpu.max")
	if data, err := os.ReadFile(cpuMaxPath); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) == 2 {
			if parts[0] != "max" {
				if quota, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
					stats.CpuQuotaUs = quota
				}
			} else {
				stats.CpuQuotaUs = -1 // Unlimited
			}
			if period, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
				stats.CpuPeriodUs = period
			}
		}
	}

	// Read cpu.weight (v2 equivalent of shares)
	// Note: v2 uses weights 1-10000, v1 uses shares 2-262144
	// We'll store as v1-style shares for consistency
	cpuWeightPath := filepath.Join(container.CgroupPath, "cpu.weight")
	if data, err := os.ReadFile(cpuWeightPath); err == nil {
		if weight, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
			// Convert weight to shares: shares = (weight * 1024) / 100
			stats.CpuShares = (weight * 1024) / 100
		}
	}

	return nil
}

// parseCPUStat parses cgroup v1 cpu.stat file
func (c *CgroupCPUCollector) parseCPUStat(data string, stats *performance.CgroupCPUStats) {
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
		case "nr_periods":
			stats.NrPeriods = value
		case "nr_throttled":
			stats.NrThrottled = value
		case "throttled_time":
			stats.ThrottledTime = value
		}
	}
}

// parseCgroupV2CPUStat parses cgroup v2 cpu.stat file
func (c *CgroupCPUCollector) parseCgroupV2CPUStat(data string, stats *performance.CgroupCPUStats) {
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
		case "usage_usec":
			// Convert microseconds to nanoseconds
			stats.UsageNanos = value * 1000
		case "nr_periods":
			stats.NrPeriods = value
		case "nr_throttled":
			stats.NrThrottled = value
		case "throttled_usec":
			// Convert microseconds to nanoseconds
			stats.ThrottledTime = value * 1000
		}
	}
}
