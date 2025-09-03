// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"strconv"
	"strings"
)

// ParseCPUList parses CPU list in kernel-standardized format used by cpuset and NUMA nodes.
//
// Format specification:
// - Comma-separated list of individual CPUs and ranges
// - Range format: "start-end" (inclusive, e.g., "0-3" means CPUs 0, 1, 2, 3)
// - Single CPU format: just the number (e.g., "5")
// - Mixed format: "0-3,6,8-10" means CPUs 0, 1, 2, 3, 6, 8, 9, 10
//
// Examples:
// - "0-3" -> [0, 1, 2, 3]
// - "0,2,4,6" -> [0, 2, 4, 6]
// - "0-3,6,8-10" -> [0, 1, 2, 3, 6, 8, 9, 10]
// - "" -> []
//
// This format is used by:
// - /sys/devices/system/node/nodeX/cpulist (NUMA node CPU affinity)
// - /sys/fs/cgroup/.../cpuset.cpus (container CPU constraints)
// - /sys/fs/cgroup/.../cpuset.mems (container NUMA node constraints)
func ParseCPUList(cpuList string) ([]int, error) {
	cpus := make([]int, 0)

	// Handle empty input
	cpuList = strings.TrimSpace(cpuList)
	if cpuList == "" {
		return cpus, nil
	}

	// Split by commas to handle multiple ranges/individual CPUs
	ranges := strings.Split(cpuList, ",")
	for _, r := range ranges {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}

		if strings.Contains(r, "-") {
			// Range format: "0-3" means CPUs 0, 1, 2, 3
			parts := strings.Split(r, "-")
			if len(parts) != 2 {
				return nil, &ErrInvalidCPURange{Input: r, Reason: "invalid range format"}
			}

			start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return nil, &ErrInvalidCPURange{Input: r, Reason: "invalid start CPU ID: " + err.Error()}
			}

			end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, &ErrInvalidCPURange{Input: r, Reason: "invalid end CPU ID: " + err.Error()}
			}

			// Add all CPUs in the range
			for cpu := start; cpu <= end; cpu++ {
				cpus = append(cpus, cpu)
			}
		} else {
			// Single CPU format: "5" means CPU 5
			cpu, err := strconv.Atoi(r)
			if err != nil {
				return nil, &ErrInvalidCPURange{Input: r, Reason: "invalid CPU ID: " + err.Error()}
			}
			cpus = append(cpus, cpu)
		}
	}

	return cpus, nil
}

// ErrInvalidCPURange represents an error in parsing CPU range format
type ErrInvalidCPURange struct {
	Input  string
	Reason string
}

func (e *ErrInvalidCPURange) Error() string {
	return "invalid CPU range '" + e.Input + "': " + e.Reason
}
