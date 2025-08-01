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

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

func init() {
	performance.TryRegister(performance.MetricTypeCPUInfo, performance.PartialNewOnceContinuousCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewCPUInfoCollector(logger, config)
		},
	))
}

// CPUInfoCollector collects CPU hardware configuration from /proc/cpuinfo.
//
// IMPORTANT: The /proc/cpuinfo format is NOT standardized across architectures.
// Each CPU architecture (x86, ARM, PowerPC, etc.) implements its own format with
// different field names and meanings. The Linux kernel provides no guarantees
// about which fields will be present.
//
// Common variations:
// - x86: Uses "flags" for CPU features, "vendor_id", "model name", etc.
// - ARM: Uses "Features" (capital F) instead of "flags", "BogoMIPS" instead of "bogomips"
// - PowerPC: Uses "cpu" instead of "model name", includes "clock", no feature flags
// - VMs: May have missing or dummy physical/core IDs
//
// This collector handles these variations gracefully by checking field existence
// before parsing and using fallback logic where appropriate.
type CPUInfoCollector struct {
	performance.BaseCollector
	cpuinfoPath    string
	cpufreqPath    string
	nodeSystemPath string
}

// Compile-time interface check
var _ performance.PointCollector = (*CPUInfoCollector)(nil)

func NewCPUInfoCollector(logger logr.Logger, config performance.CollectionConfig) (*CPUInfoCollector, error) {
	if err := config.Validate(performance.ValidateOptions{RequireHostProcPath: true, RequireHostSysPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   false,
		RequiredCapabilities: nil, // No special capabilities required
		MinKernelVersion:     "2.6.0",
	}

	return &CPUInfoCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeCPUInfo,
			"CPU Hardware Info Collector",
			logger,
			config,
			capabilities,
		),
		cpuinfoPath:    filepath.Join(config.HostProcPath, "cpuinfo"),
		cpufreqPath:    filepath.Join(config.HostSysPath, "devices", "system", "cpu"),
		nodeSystemPath: filepath.Join(config.HostSysPath, "devices", "system", "node"),
	}, nil
}

func (c *CPUInfoCollector) Collect(ctx context.Context) (any, error) {
	return c.collectCPUInfo()
}

func (c *CPUInfoCollector) collectCPUInfo() (*performance.CPUInfo, error) {
	info := &performance.CPUInfo{
		Cores: make([]performance.CPUCore, 0),
		Flags: make([]string, 0),
	}

	// Parse /proc/cpuinfo
	if err := c.parseCPUInfo(info); err != nil {
		return nil, fmt.Errorf("failed to parse cpuinfo: %w", err)
	}

	// Get CPU frequency info from sysfs
	c.parseCPUFrequency(info)

	// Count NUMA nodes
	c.countNUMANodes(info)

	return info, nil
}

func (c *CPUInfoCollector) parseCPUInfo(info *performance.CPUInfo) error {
	file, err := os.Open(c.cpuinfoPath)
	if err != nil {
		return err
	}
	defer file.Close()

	physicalIDs := make(map[int32]bool)
	coreMap := make(map[string]bool) // Map of "physical_id:core_id"

	var currentCore performance.CPUCore
	inProcessor := false

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Empty line indicates end of processor section
		if line == "" {
			if inProcessor {
				info.Cores = append(info.Cores, currentCore)
				info.LogicalCores++
				inProcessor = false
				currentCore = performance.CPUCore{}
			}
			continue
		}

		fields := strings.SplitN(line, ":", 2)
		if len(fields) != 2 {
			continue
		}

		key := strings.TrimSpace(fields[0])
		value := strings.TrimSpace(fields[1])

		switch key {
		case "processor":
			processor, err := strconv.ParseInt(value, 10, 32)
			if err == nil {
				currentCore.Processor = int32(processor)
				inProcessor = true
			}
		case "vendor_id":
			if info.VendorID == "" {
				info.VendorID = value
			}
		case "cpu family":
			if info.CPUFamily == 0 {
				if family, err := strconv.ParseInt(value, 10, 32); err == nil {
					info.CPUFamily = int32(family)
				}
			}
		case "model":
			if info.Model == 0 {
				if model, err := strconv.ParseInt(value, 10, 32); err == nil {
					info.Model = int32(model)
				}
			}
		case "model name":
			if info.ModelName == "" {
				info.ModelName = value
			}
		case "stepping":
			if info.Stepping == 0 {
				if stepping, err := strconv.ParseInt(value, 10, 32); err == nil {
					info.Stepping = int32(stepping)
				}
			}
		case "microcode":
			if info.Microcode == "" {
				info.Microcode = value
			}
		case "cpu MHz":
			mhz, err := strconv.ParseFloat(value, 64)
			if err == nil {
				currentCore.CPUMHz = mhz
				if info.CPUMHz == 0 {
					info.CPUMHz = mhz
				}
			}
		case "cache size":
			if info.CacheSize == "" {
				info.CacheSize = value
			}
		case "cache_alignment":
			if alignment, err := strconv.ParseInt(value, 10, 32); err == nil {
				info.CacheAlignment = int32(alignment)
			}
		case "physical id":
			if id, err := strconv.ParseInt(value, 10, 32); err == nil {
				currentCore.PhysicalID = int32(id)
				physicalIDs[int32(id)] = true
			}
		case "siblings":
			if siblings, err := strconv.ParseInt(value, 10, 32); err == nil {
				currentCore.Siblings = int32(siblings)
			}
		case "core id":
			if id, err := strconv.ParseInt(value, 10, 32); err == nil {
				currentCore.CoreID = int32(id)
			}
		case "flags", "Features":
			if len(info.Flags) == 0 {
				info.Flags = strings.Fields(value)
			}
		case "bogomips", "BogoMIPS":
			if mips, err := strconv.ParseFloat(value, 64); err == nil && info.BogoMIPS == 0 {
				info.BogoMIPS = mips
			}
		}
	}

	// Handle last processor if file doesn't end with empty line
	if inProcessor {
		info.Cores = append(info.Cores, currentCore)
		info.LogicalCores++
	}

	// Count physical cores using physical topology information
	// We detect meaningful physical topology by checking if we have variety in
	// PhysicalID or CoreID values, indicating real hardware topology information
	hasPhysicalInfo := false

	// Check if we have meaningful physical topology by looking for non-zero values
	// or multiple different values, indicating real hardware topology
	if len(info.Cores) > 0 {
		// Look for any non-zero PhysicalID or CoreID, or multiple different values
		seenPhysicalIDs := make(map[int32]bool)
		seenCoreIDs := make(map[int32]bool)

		for _, core := range info.Cores {
			seenPhysicalIDs[core.PhysicalID] = true
			seenCoreIDs[core.CoreID] = true

			// If we see non-zero values, we have physical topology info
			if core.PhysicalID != 0 || core.CoreID != 0 {
				hasPhysicalInfo = true
			}
		}

		// Even if all values are 0, if we have multiple processors with different
		// core IDs, we might have meaningful topology
		if !hasPhysicalInfo && len(seenCoreIDs) > 1 {
			hasPhysicalInfo = true
		}
	}

	// Build map of unique physical cores
	for _, core := range info.Cores {
		coreKey := fmt.Sprintf("%d:%d", core.PhysicalID, core.CoreID)
		coreMap[coreKey] = true
	}

	// If we have physical info, use the unique core count
	if hasPhysicalInfo {
		info.PhysicalCores = int32(len(coreMap))
	} else {
		// Fallback: If no physical/core IDs (e.g., in VMs), assume physical cores = logical cores
		info.PhysicalCores = info.LogicalCores
	}

	return scanner.Err()
}

func (c *CPUInfoCollector) parseCPUFrequency(info *performance.CPUInfo) {
	// Try to read CPU frequency limits from first CPU
	cpu0FreqPath := filepath.Join(c.cpufreqPath, "cpu0", "cpufreq")

	// Read min frequency
	minFreqPath := filepath.Join(cpu0FreqPath, "cpuinfo_min_freq")
	if data, err := os.ReadFile(minFreqPath); err == nil {
		if freq, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
			info.CPUMinMHz = freq / 1000 // Convert from KHz to MHz
		}
	}

	// Read max frequency
	maxFreqPath := filepath.Join(cpu0FreqPath, "cpuinfo_max_freq")
	if data, err := os.ReadFile(maxFreqPath); err == nil {
		if freq, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
			info.CPUMaxMHz = freq / 1000 // Convert from KHz to MHz
		}
	}
}

func (c *CPUInfoCollector) countNUMANodes(info *performance.CPUInfo) {
	// Count NUMA nodes by looking at /sys/devices/system/node/node*
	pattern := filepath.Join(c.nodeSystemPath, "node[0-9]*")
	matches, err := filepath.Glob(pattern)
	if err == nil && len(matches) > 0 {
		info.NUMANodes = int32(len(matches))
	} else {
		// Default to 1 NUMA node if we can't determine
		info.NUMANodes = 1
	}
}
