// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package cpu

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
