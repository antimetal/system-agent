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

	"github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

func init() {
	performance.Register(performance.MetricTypeCgroupPSI, performance.PartialNewContinuousPointCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewCgroupPSICollector(logger, config)
		},
	))
}

var _ performance.PointCollector = (*CgroupPSICollector)(nil)

// CgroupPSICollector collects Pressure Stall Information from container cgroups
// PSI provides metrics about resource contention (CPU, memory, I/O) per container
// Reference: https://www.kernel.org/doc/html/latest/accounting/psi.html
//
// PSI is standard in cgroup v2 (kernel 4.20+) but rarely available in cgroup v1
// Gracefully handles missing PSI files by skipping unavailable resources
type CgroupPSICollector struct {
	performance.BaseCollector
	cgroupPath string
	discovery  *containers.Discovery
}

func NewCgroupPSICollector(logger logr.Logger, config performance.CollectionConfig) (*CgroupPSICollector, error) {
	if err := config.Validate(performance.ValidateOptions{RequireHostSysPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   false,
		RequiredCapabilities: nil,
		MinKernelVersion:     "4.20.0", // PSI introduced in 4.20
	}

	cgroupPath := filepath.Join(config.HostSysPath, "fs", "cgroup")

	return &CgroupPSICollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeCgroupPSI,
			"Cgroup PSI Collector",
			logger,
			config,
			capabilities,
		),
		cgroupPath: cgroupPath,
		discovery:  containers.NewDiscovery(cgroupPath),
	}, nil
}

func (c *CgroupPSICollector) Collect(ctx context.Context) (performance.Event, error) {
	version, err := c.discovery.DetectCgroupVersion()
	if err != nil {
		return performance.Event{}, fmt.Errorf("failed to detect cgroup version: %w", err)
	}

	c.Logger().V(2).Info("Detected cgroup version", "version", version)

	// Discover containers - use empty subsystem since PSI files are in container's root cgroup
	discoveredContainers, err := c.discovery.DiscoverContainers("", version)
	if err != nil {
		return performance.Event{}, fmt.Errorf("failed to discover containers: %w", err)
	}

	var stats []*performance.CgroupPSIStats
	for _, container := range discoveredContainers {
		select {
		case <-ctx.Done():
			return performance.Event{Metric: performance.MetricTypeCgroupPSI, Data: stats}, ctx.Err()
		default:
		}

		stat, err := c.collectContainerPSI(container)
		if err != nil {
			c.Logger().V(1).Info("Failed to collect PSI for container",
				"containerID", container.ID,
				"error", err)
			continue
		}
		stats = append(stats, stat)
	}

	return performance.Event{Metric: performance.MetricTypeCgroupPSI, Data: stats}, nil
}

func (c *CgroupPSICollector) collectContainerPSI(container containers.Container) (*performance.CgroupPSIStats, error) {
	stats := &performance.CgroupPSIStats{
		ContainerID: container.ID,
		CgroupPath:  container.CgroupPath,
	}

	// Read cpu.pressure (optional - may not exist in v1)
	cpuPath := filepath.Join(container.CgroupPath, "cpu.pressure")
	if data, err := os.ReadFile(cpuPath); err == nil {
		if parsed, err := ParsePSIData(string(data)); err == nil {
			stats.CPU = parsed
		}
	}

	// Read memory.pressure (optional - may not exist in v1)
	memPath := filepath.Join(container.CgroupPath, "memory.pressure")
	if data, err := os.ReadFile(memPath); err == nil {
		if parsed, err := ParsePSIData(string(data)); err == nil {
			stats.Memory = parsed
		}
	}

	// Read io.pressure (optional - may not exist in v1)
	ioPath := filepath.Join(container.CgroupPath, "io.pressure")
	if data, err := os.ReadFile(ioPath); err == nil {
		if parsed, err := ParsePSIData(string(data)); err == nil {
			stats.IO = parsed
		}
	}

	// Return error only if ALL PSI files are missing
	if stats.CPU == nil && stats.Memory == nil && stats.IO == nil {
		return nil, fmt.Errorf("no PSI data available for container %s (cgroup v1 may not support PSI)", container.ID)
	}

	return stats, nil
}
