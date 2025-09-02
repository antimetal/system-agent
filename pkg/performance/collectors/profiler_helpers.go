// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

// onlineCPUs returns a list of online CPUs
func (c *ProfilerCollector) onlineCPUs() ([]int, error) {
	// Try to read from /sys/devices/system/cpu/online first
	onlinePath := filepath.Join(c.sysPath, "devices/system/cpu/online")
	data, err := os.ReadFile(onlinePath)
	if err == nil {
		return parseCPUList(string(data))
	}

	// Fallback: count cpu directories
	cpuDir := filepath.Join(c.sysPath, "devices/system/cpu")
	entries, err := os.ReadDir(cpuDir)
	if err != nil {
		return nil, fmt.Errorf("reading CPU directory: %w", err)
	}

	var cpus []int
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "cpu") {
			cpuStr := strings.TrimPrefix(entry.Name(), "cpu")
			if cpu, err := strconv.Atoi(cpuStr); err == nil {
				cpus = append(cpus, cpu)
			}
		}
	}

	if len(cpus) == 0 {
		// Last fallback: assume at least one CPU
		return []int{0}, nil
	}

	return cpus, nil
}

// parseCPUList parses CPU list format like "0-3,5,7-9"
func parseCPUList(cpuList string) ([]int, error) {
	var cpus []int
	cpuList = strings.TrimSpace(cpuList)

	for _, part := range strings.Split(cpuList, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			// Range format: "0-3"
			bounds := strings.Split(part, "-")
			if len(bounds) != 2 {
				return nil, fmt.Errorf("invalid CPU range: %s", part)
			}

			start, err := strconv.Atoi(bounds[0])
			if err != nil {
				return nil, fmt.Errorf("invalid CPU range start: %s", bounds[0])
			}

			end, err := strconv.Atoi(bounds[1])
			if err != nil {
				return nil, fmt.Errorf("invalid CPU range end: %s", bounds[1])
			}

			for i := start; i <= end; i++ {
				cpus = append(cpus, i)
			}
		} else {
			// Single CPU
			cpu, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid CPU number: %s", part)
			}
			cpus = append(cpus, cpu)
		}
	}

	return cpus, nil
}

// attachPerfEvent attaches an eBPF program to a perf event on a specific CPU
func (c *ProfilerCollector) attachPerfEvent(prog *ebpf.Program, cpu int) (link.Link, error) {
	// For now, return an error - perf event attachment needs to be implemented
	// using the proper cilium/ebpf API
	return nil, fmt.Errorf("perf event attachment not yet implemented for CPU %d", cpu)
}
