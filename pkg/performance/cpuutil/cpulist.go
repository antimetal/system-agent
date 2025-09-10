// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package cpuutil

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseCPUList parses a Linux kernel CPU list format string into a slice of int32 CPU IDs.
// The format supports:
//   - Individual CPUs: "0", "1", "2"
//   - Ranges: "0-3" (includes 0, 1, 2, 3)
//   - Comma-separated combinations: "0,2-4,7"
//   - Empty string returns empty slice (not nil)
//
// This format is used in /sys/devices/system/cpu/online, /proc/self/status (Cpus_allowed_list),
// and NUMA node CPU lists. Returns int32 for compatibility with protobuf-generated types.
//
// Examples:
//   - "0" -> [0]
//   - "0-3" -> [0, 1, 2, 3]
//   - "0,2-4,7" -> [0, 2, 3, 4, 7]
//   - "" -> []
func ParseCPUList(cpuList string) ([]int32, error) {
	cpuList = strings.TrimSpace(cpuList)
	if cpuList == "" {
		return []int32{}, nil
	}

	var cpus []int32
	for _, part := range strings.Split(cpuList, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			// Range format: "0-3"
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid CPU range: %s", part)
			}

			start, err := strconv.ParseInt(strings.TrimSpace(rangeParts[0]), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid CPU number in range: %s", rangeParts[0])
			}

			end, err := strconv.ParseInt(strings.TrimSpace(rangeParts[1]), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid CPU number in range: %s", rangeParts[1])
			}

			if start > end {
				return nil, fmt.Errorf("invalid CPU range (start > end): %s", part)
			}

			// Note: We accept single-element ranges like "5-5" and treat them as [5].
			// While the Linux kernel never produces such output (it would output just "5"),
			// we parse them leniently for compatibility with various input sources.
			for cpu := start; cpu <= end; cpu++ {
				cpus = append(cpus, int32(cpu))
			}
		} else {
			// Single CPU
			cpu, err := strconv.ParseInt(part, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid CPU number: %s", part)
			}
			cpus = append(cpus, int32(cpu))
		}
	}

	return cpus, nil
}

// FormatCPUList formats a slice of int32 CPU IDs into the kernel CPU list format.
// It attempts to create compact ranges where possible.
//
// Examples:
//   - [0, 1, 2, 3] -> "0-3"
//   - [0, 2, 3, 4, 7] -> "0,2-4,7"
//   - [] -> ""
func FormatCPUList(cpus []int32) string {
	if len(cpus) == 0 {
		return ""
	}

	// Sort the CPUs first (make a copy to avoid modifying input)
	sorted := make([]int32, len(cpus))
	copy(sorted, cpus)
	
	// Simple bubble sort (usually small arrays)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Build ranges
	var parts []string
	i := 0
	for i < len(sorted) {
		start := sorted[i]
		end := start

		// Find consecutive CPUs
		for i+1 < len(sorted) && sorted[i+1] == sorted[i]+1 {
			i++
			end = sorted[i]
		}

		// Format the range or single CPU
		if start == end {
			parts = append(parts, strconv.Itoa(int(start)))
		} else if end == start+1 {
			// Two consecutive CPUs - list them individually (more readable)
			parts = append(parts, strconv.Itoa(int(start)))
			parts = append(parts, strconv.Itoa(int(end)))
		} else {
			// Range of 3 or more
			parts = append(parts, fmt.Sprintf("%d-%d", start, end))
		}

		i++
	}

	return strings.Join(parts, ",")
}