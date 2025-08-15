// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

package capabilities

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// HasAllCapabilities checks if the current process has all required capabilities
func HasAllCapabilities(required []Capability) (bool, []Capability, error) {
	if len(required) == 0 {
		return true, nil, nil
	}

	current, err := getCurrentCapabilities()
	if err != nil {
		return false, required, fmt.Errorf("failed to get current capabilities: %w", err)
	}

	var missing []Capability
	for _, cap := range required {
		if !hasCapability(current, cap) {
			missing = append(missing, cap)
		}
	}

	return len(missing) == 0, missing, nil
}

// getCurrentCapabilities reads the current process capabilities from /proc/self/status
func getCurrentCapabilities() (map[Capability]bool, error) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/self/status: %w", err)
	}

	caps := make(map[Capability]bool)
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, "CapEff:") {
			// Extract the effective capabilities value
			parts := strings.Fields(line)
			if len(parts) != 2 {
				continue
			}

			capValue, err := strconv.ParseUint(parts[1], 16, 64)
			if err != nil {
				continue
			}

			// Check each capability we care about
			checkCaps := []Capability{CAP_SYS_ADMIN, CAP_SYSLOG, CAP_BPF, CAP_PERFMON}
			for _, cap := range checkCaps {
				if capValue&(1<<uint(cap)) != 0 {
					caps[cap] = true
				}
			}
			break
		}
	}

	return caps, nil
}

func hasCapability(caps map[Capability]bool, cap Capability) bool {
	return caps[cap]
}
