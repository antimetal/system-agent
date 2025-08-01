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
type CgroupCPUCollector struct {
	performance.BaseCollector
	cgroupPath string
}

// NewCgroupCPUCollector creates a new cgroup CPU collector
func NewCgroupCPUCollector(logger logr.Logger, config performance.CollectionConfig) (*CgroupCPUCollector, error) {
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

	return &CgroupCPUCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeCgroupCPU,
			"Cgroup CPU Statistics Collector",
			logger,
			config,
			capabilities,
		),
		cgroupPath: config.HostCgroupPath,
	}, nil
}

// Collect performs a one-shot collection of cgroup CPU statistics
func (c *CgroupCPUCollector) Collect(ctx context.Context) (any, error) {
	// Detect cgroup version
	version, err := c.detectCgroupVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to detect cgroup version: %w", err)
	}

	c.Logger().V(2).Info("Detected cgroup version", "version", version)

	// Discover containers
	containers, err := c.discoverContainers(version)
	if err != nil {
		return nil, fmt.Errorf("failed to discover containers: %w", err)
	}

	// Collect stats for each container
	var stats []performance.CgroupCPUStats
	for _, container := range containers {
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

// containerPath represents a discovered container
type containerPath struct {
	ID         string
	Runtime    string
	CgroupPath string
}

// detectCgroupVersion determines if we're using cgroup v1 or v2
func (c *CgroupCPUCollector) detectCgroupVersion() (int, error) {
	// Check for cgroup v2 unified hierarchy
	v2Marker := filepath.Join(c.cgroupPath, "cgroup.controllers")
	if _, err := os.Stat(v2Marker); err == nil {
		return 2, nil
	}

	// Check for cgroup v1 cpu controller
	v1CPU := filepath.Join(c.cgroupPath, "cpu")
	if _, err := os.Stat(v1CPU); err == nil {
		return 1, nil
	}

	return 0, fmt.Errorf("unable to detect cgroup version at %s", c.cgroupPath)
}

// discoverContainers finds all containers by scanning cgroup directories
func (c *CgroupCPUCollector) discoverContainers(version int) ([]containerPath, error) {
	var containers []containerPath

	if version == 1 {
		// Cgroup v1: Look in cpu and cpuacct controllers
		cpuPath := filepath.Join(c.cgroupPath, "cpu")
		containers = append(containers, c.scanCgroupV1Directory(cpuPath)...)
	} else {
		// Cgroup v2: Scan unified hierarchy
		containers = c.scanCgroupV2Directory(c.cgroupPath)
	}

	return containers, nil
}

// scanCgroupV1Directory scans a cgroup v1 controller directory for containers
func (c *CgroupCPUCollector) scanCgroupV1Directory(basePath string) []containerPath {
	var containers []containerPath
	
	// Common runtime paths to check
	runtimePaths := map[string]string{
		"docker":     filepath.Join(basePath, "docker"),
		"containerd": filepath.Join(basePath, "containerd"),
		"crio":       filepath.Join(basePath, "crio"),
		"podman":     filepath.Join(basePath, "machine.slice"),
	}

	// Also check systemd slice format
	systemdPath := filepath.Join(basePath, "system.slice")
	if entries, err := os.ReadDir(systemdPath); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "docker-") {
				if id := extractContainerID(entry.Name()); id != "" {
					containers = append(containers, containerPath{
						ID:         id,
						Runtime:    "docker",
						CgroupPath: filepath.Join(systemdPath, entry.Name()),
					})
				}
			}
		}
	}

	// Check each runtime path
	for runtime, path := range runtimePaths {
		entries, err := os.ReadDir(path)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			// Extract container ID
			id := entry.Name()
			// Skip if it doesn't look like a container ID
			if len(id) < 12 || !isHexString(id) {
				continue
			}

			containers = append(containers, containerPath{
				ID:         id,
				Runtime:    runtime,
				CgroupPath: filepath.Join(path, id),
			})
		}
	}

	return containers
}

// scanCgroupV2Directory scans a cgroup v2 unified hierarchy for containers
func (c *CgroupCPUCollector) scanCgroupV2Directory(basePath string) []containerPath {
	var containers []containerPath

	// In cgroup v2, containers might be in various locations
	// Check system.slice for systemd-managed containers
	systemSlice := filepath.Join(basePath, "system.slice")
	if entries, err := os.ReadDir(systemSlice); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			// Look for patterns like docker-<id>.scope
			if strings.HasPrefix(entry.Name(), "docker-") && strings.HasSuffix(entry.Name(), ".scope") {
				if id := extractContainerID(entry.Name()); id != "" {
					containers = append(containers, containerPath{
						ID:         id,
						Runtime:    "docker",
						CgroupPath: filepath.Join(systemSlice, entry.Name()),
					})
				}
			}
		}
	}

	// Check for Kubernetes pods
	kubepods := filepath.Join(basePath, "kubepods.slice")
	if _, err := os.Stat(kubepods); err == nil {
		// Walk through kubepods hierarchy
		filepath.Walk(kubepods, func(path string, info os.FileInfo, err error) error {
			if err != nil || !info.IsDir() {
				return nil
			}

			// Look for container directories (usually long hex strings)
			name := filepath.Base(path)
			if len(name) >= 64 && isHexString(name) {
				containers = append(containers, containerPath{
					ID:         name,
					Runtime:    "containerd", // Kubernetes typically uses containerd
					CgroupPath: path,
				})
			}
			return nil
		})
	}

	return containers
}

// collectContainerStats collects CPU stats for a single container
func (c *CgroupCPUCollector) collectContainerStats(container containerPath, version int) (performance.CgroupCPUStats, error) {
	stats := performance.CgroupCPUStats{
		ContainerID: container.ID,
		CgroupPath:  container.CgroupPath,
	}

	if version == 1 {
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
func (c *CgroupCPUCollector) readCgroupV1Stats(stats *performance.CgroupCPUStats, container containerPath) error {
	// Read CPU throttling stats
	cpuStatPath := filepath.Join(container.CgroupPath, "cpu.stat")
	if data, err := os.ReadFile(cpuStatPath); err == nil {
		c.parseCPUStat(string(data), stats)
	}

	// Read CPU usage
	// Note: In v1, we need to use the cpuacct controller path
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
func (c *CgroupCPUCollector) readCgroupV2Stats(stats *performance.CgroupCPUStats, container containerPath) error {
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

// Helper functions

// extractContainerID extracts container ID from various cgroup path formats
func extractContainerID(name string) string {
	// Handle docker-<id>.scope format
	if strings.HasPrefix(name, "docker-") && strings.HasSuffix(name, ".scope") {
		id := strings.TrimPrefix(name, "docker-")
		id = strings.TrimSuffix(id, ".scope")
		return id
	}
	
	// Handle containerd format
	if strings.Contains(name, "containerd-") {
		parts := strings.Split(name, "containerd-")
		if len(parts) > 1 {
			return strings.TrimSuffix(parts[1], ".scope")
		}
	}

	return ""
}

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}