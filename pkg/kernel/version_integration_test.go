// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package kernel

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/antimetal/agent/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentVersion_RealKernel(t *testing.T) {
	testutil.RequireLinux(t)

	version, err := CurrentVersion()
	require.NoError(t, err, "Should be able to get current kernel version")

	// Verify we got reasonable values
	assert.Greater(t, version.Major, 0, "Major version should be positive")
	assert.GreaterOrEqual(t, version.Minor, 0, "Minor version should be non-negative")
	assert.GreaterOrEqual(t, version.Patch, 0, "Patch version should be non-negative")
	assert.NotEmpty(t, version.Raw, "Raw version string should not be empty")

	// Verify the raw string contains the parsed version numbers
	expectedPrefix := fmt.Sprintf("%d.%d", version.Major, version.Minor)
	assert.True(t, strings.HasPrefix(version.Raw, expectedPrefix),
		"Raw version %q should start with %q", version.Raw, expectedPrefix)
}

func TestCurrentVersion_ProcVersion(t *testing.T) {
	testutil.RequireLinux(t)
	testutil.RequireLinuxFilesystem(t)

	// Read /proc/version directly
	procVersionBytes, err := os.ReadFile("/proc/version")
	require.NoError(t, err, "Should be able to read /proc/version")
	procVersion := string(procVersionBytes)

	// Get version through our API
	version, err := CurrentVersion()
	require.NoError(t, err)

	// The raw version from uname should be contained in /proc/version
	// /proc/version format: "Linux version 5.15.0-generic (buildd@...) ..."
	assert.Contains(t, procVersion, version.Raw,
		"/proc/version should contain the kernel version string")
}

func TestIsAtLeast_RealKernel(t *testing.T) {
	testutil.RequireLinux(t)

	version, err := CurrentVersion()
	require.NoError(t, err)

	// Test with current version - should always be true
	assert.True(t, version.IsAtLeast(version.Major, version.Minor, version.Patch),
		"Current version should be at least itself")

	// Test with a very old version - should always be true on modern systems
	assert.True(t, version.IsAtLeast(2, 6, 0),
		"Current kernel should be at least 2.6.0")

	// Test with a future version - should be false
	assert.False(t, version.IsAtLeast(999, 0, 0),
		"Current kernel should not be version 999.0.0 or higher")

	// Test edge cases with current version
	if version.Patch > 0 {
		assert.True(t, version.IsAtLeast(version.Major, version.Minor, version.Patch-1),
			"Should be at least one patch version behind")
	}
	assert.False(t, version.IsAtLeast(version.Major, version.Minor, version.Patch+1),
		"Should not be at least one patch version ahead")
}

func TestCompare_RealKernel(t *testing.T) {
	testutil.RequireLinux(t)

	current, err := CurrentVersion()
	require.NoError(t, err)

	tests := []struct {
		name     string
		other    *Version
		expected int
	}{
		{
			name:     "same version",
			other:    current,
			expected: 0,
		},
		{
			name: "older major version",
			other: &Version{
				Major: current.Major - 1,
				Minor: current.Minor,
				Patch: current.Patch,
			},
			expected: 1,
		},
		{
			name: "newer major version",
			other: &Version{
				Major: current.Major + 1,
				Minor: current.Minor,
				Patch: current.Patch,
			},
			expected: -1,
		},
		{
			name: "older minor version",
			other: &Version{
				Major: current.Major,
				Minor: max(0, current.Minor-1),
				Patch: current.Patch,
			},
			expected: func() int {
				if current.Minor == 0 {
					return 0 // Can't go below 0
				}
				return 1
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := current.Compare(tt.other)
			assert.Equal(t, tt.expected, result,
				"Comparing %s with %s", current.String(), tt.other.String())
		})
	}
}

func TestFeatureChecks_RealKernel(t *testing.T) {
	testutil.RequireLinux(t)

	version, err := CurrentVersion()
	require.NoError(t, err)

	// Test BTF support detection
	// BTF is available in kernel 5.2+
	hasBTF := version.IsAtLeast(5, 2, 0)
	
	// Check if actual BTF file exists
	_, btfErr := os.Stat("/sys/kernel/btf/vmlinux")
	btfExists := btfErr == nil

	if hasBTF && btfExists {
		t.Logf("Kernel %s has BTF support and BTF file exists", version.String())
	} else if !hasBTF {
		t.Logf("Kernel %s does not meet BTF minimum version requirement (5.2.0)", version.String())
	} else if !btfExists {
		t.Logf("Kernel %s meets BTF version requirement but BTF file not found", version.String())
	}

	// Test eBPF capabilities detection
	// CAP_BPF and CAP_PERFMON available in kernel 5.8+
	hasModernBPF := version.IsAtLeast(5, 8, 0)
	if hasModernBPF {
		t.Logf("Kernel %s has modern eBPF capabilities (CAP_BPF, CAP_PERFMON)", version.String())
	} else {
		t.Logf("Kernel %s requires CAP_SYS_ADMIN for eBPF operations", version.String())
	}

	// Test cgroup v2 support
	// cgroup v2 is fully supported in kernel 4.5+
	hasCgroupV2 := version.IsAtLeast(4, 5, 0)
	if hasCgroupV2 {
		// Check if cgroup v2 is actually mounted
		_, cgroupErr := os.Stat("/sys/fs/cgroup/cgroup.controllers")
		if cgroupErr == nil {
			t.Logf("Kernel %s supports cgroup v2 and it is mounted", version.String())
		} else {
			t.Logf("Kernel %s supports cgroup v2 but it is not mounted", version.String())
		}
	}
}

// TestKernelVersionConsistency verifies that different methods of getting
// the kernel version return consistent results
func TestKernelVersionConsistency(t *testing.T) {
	testutil.RequireLinux(t)
	testutil.RequireLinuxFilesystem(t)

	// Get version through our API
	ourVersion, err := CurrentVersion()
	require.NoError(t, err)

	// Get version through testutil
	testutilVersion, err := testutil.GetKernelVersion()
	require.NoError(t, err)

	// They should match
	assert.Equal(t, ourVersion.Major, testutilVersion.Major, "Major versions should match")
	assert.Equal(t, ourVersion.Minor, testutilVersion.Minor, "Minor versions should match")
	assert.Equal(t, ourVersion.Patch, testutilVersion.Patch, "Patch versions should match")
	assert.Equal(t, ourVersion.Raw, testutilVersion.Full, "Full version strings should match")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}