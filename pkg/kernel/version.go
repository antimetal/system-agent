// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

// Package kernel provides utilities for kernel version detection and comparison
package kernel

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Version represents a parsed kernel version
type Version struct {
	Major int
	Minor int
	Patch int
	Raw   string // Original version string
}

// GetCurrentVersion returns the current kernel version
func GetCurrentVersion() (*Version, error) {
	// Try to read from /proc/version
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/version: %w", err)
	}

	// Parse "Linux version X.Y.Z"
	parts := strings.Fields(string(data))
	if len(parts) < 3 {
		return nil, fmt.Errorf("unexpected /proc/version format: %s", string(data))
	}

	return ParseVersion(parts[2])
}

// ParseVersion parses a kernel version string (e.g., "5.15.0-generic" or "5.15.0")
func ParseVersion(version string) (*Version, error) {
	v := &Version{Raw: version}

	// Remove any suffix (e.g., "-generic")
	if idx := strings.Index(version, "-"); idx != -1 {
		version = version[:idx]
	}

	// Parse X.Y.Z format
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid kernel version format: %s", version)
	}

	// Parse major version
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid major version: %s", parts[0])
	}
	v.Major = major

	// Parse minor version
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid minor version: %s", parts[1])
	}
	v.Minor = minor

	// Parse patch version if present
	if len(parts) >= 3 {
		patch, err := strconv.Atoi(parts[2])
		if err != nil {
			// Patch might have additional info, just use 0
			v.Patch = 0
		} else {
			v.Patch = patch
		}
	}

	return v, nil
}

// IsAtLeast returns true if the current version is >= the specified version
func (v *Version) IsAtLeast(major, minor int) bool {
	if v.Major > major {
		return true
	}
	if v.Major == major && v.Minor >= minor {
		return true
	}
	return false
}

// String returns the version as a string
func (v *Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare returns -1 if v < other, 0 if v == other, 1 if v > other
func (v *Version) Compare(other *Version) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}
	return 0
}