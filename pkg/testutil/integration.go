// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

// Package testutil provides utilities for testing, with a focus on integration test helpers.
package testutil

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/antimetal/agent/pkg/performance/capabilities"
	"golang.org/x/sys/unix"
)

// RequireLinux skips the test if not running on Linux.
func RequireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Test requires Linux")
	}
}

// RequireLinuxFilesystem verifies that essential Linux filesystems are available.
// This checks for /proc and /sys which are required for most system-level operations.
func RequireLinuxFilesystem(t *testing.T) {
	t.Helper()
	RequireLinux(t)

	// Check /proc filesystem
	if _, err := os.Stat("/proc/self"); err != nil {
		t.Skipf("Test requires /proc filesystem: %v", err)
	}

	// Check /sys filesystem
	if _, err := os.Stat("/sys/kernel"); err != nil {
		t.Skipf("Test requires /sys filesystem: %v", err)
	}
}

// RequireCapability checks if the process has the specified Linux capability.
// If the capability is not available, the test is skipped.
func RequireCapability(t *testing.T, cap capabilities.Capability) {
	t.Helper()
	RequireLinux(t)

	hasAll, missing, err := capabilities.HasAllCapabilities([]capabilities.Capability{cap})
	if err != nil {
		t.Skipf("Failed to check capability %s: %v", cap, err)
	}

	if !hasAll {
		t.Skipf("Test requires capability %s (missing: %v)", cap, missing)
	}
}

// RequireKernelVersion checks if the kernel version meets the minimum requirement.
// The test is skipped if the kernel version is lower than required.
func RequireKernelVersion(t *testing.T, major, minor, patch int) {
	t.Helper()
	RequireLinux(t)

	var utsname unix.Utsname
	if err := unix.Uname(&utsname); err != nil {
		t.Skipf("Failed to get kernel version: %v", err)
	}

	release := string(utsname.Release[:])
	release = strings.TrimRight(release, "\x00")

	// Parse kernel version (e.g., "5.15.0-generic" -> 5, 15, 0)
	parts := strings.Split(release, ".")
	if len(parts) < 2 {
		t.Skipf("Unable to parse kernel version: %s", release)
	}

	currentMajor, err := strconv.Atoi(parts[0])
	if err != nil {
		t.Skipf("Unable to parse kernel major version: %s", parts[0])
	}

	currentMinor, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Skipf("Unable to parse kernel minor version: %s", parts[1])
	}

	currentPatch := 0
	if len(parts) >= 3 {
		// Parse patch version, handling suffixes like "0-generic"
		patchStr := strings.Split(parts[2], "-")[0]
		currentPatch, _ = strconv.Atoi(patchStr)
	}

	// Compare versions
	if currentMajor < major ||
		(currentMajor == major && currentMinor < minor) ||
		(currentMajor == major && currentMinor == minor && currentPatch < patch) {
		t.Skipf("Test requires kernel %d.%d.%d or higher, current is %d.%d.%d",
			major, minor, patch, currentMajor, currentMinor, currentPatch)
	}
}

// RequireBTF checks if the system has BTF (BPF Type Format) support.
// BTF is required for CO-RE eBPF programs.
func RequireBTF(t *testing.T) {
	t.Helper()
	RequireLinux(t)

	// Check for BTF in standard location
	btfPath := "/sys/kernel/btf/vmlinux"
	if _, err := os.Stat(btfPath); err != nil {
		t.Skipf("Test requires BTF support (missing %s): %v", btfPath, err)
	}
}

// RequireRoot checks if the test is running as root.
// Some operations require root privileges.
func RequireRoot(t *testing.T) {
	t.Helper()
	RequireLinux(t)

	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}
}

// RequireCgroup checks if cgroup filesystem is available.
// Version can be 1 or 2 to check for specific cgroup version, or 0 for any.
func RequireCgroup(t *testing.T, version int) {
	t.Helper()
	RequireLinux(t)

	cgroupV1Path := "/sys/fs/cgroup/memory"
	cgroupV2Path := "/sys/fs/cgroup/cgroup.controllers"

	switch version {
	case 1:
		if _, err := os.Stat(cgroupV1Path); err != nil {
			t.Skipf("Test requires cgroup v1 (missing %s): %v", cgroupV1Path, err)
		}
	case 2:
		if _, err := os.Stat(cgroupV2Path); err != nil {
			t.Skipf("Test requires cgroup v2 (missing %s): %v", cgroupV2Path, err)
		}
	default:
		// Check for either version
		_, v1Err := os.Stat(cgroupV1Path)
		_, v2Err := os.Stat(cgroupV2Path)
		if v1Err != nil && v2Err != nil {
			t.Skip("Test requires cgroup filesystem (neither v1 nor v2 found)")
		}
	}
}

// RequireDockerRuntime checks if Docker is available and running.
func RequireDockerRuntime(t *testing.T) {
	t.Helper()
	RequireLinux(t)

	// Check for Docker socket
	dockerSocket := "/var/run/docker.sock"
	if _, err := os.Stat(dockerSocket); err != nil {
		t.Skipf("Test requires Docker runtime (missing %s): %v", dockerSocket, err)
	}
}

// KernelVersion represents a parsed kernel version.
type KernelVersion struct {
	Major int
	Minor int
	Patch int
	Full  string
}

// GetKernelVersion returns the current kernel version.
func GetKernelVersion() (KernelVersion, error) {
	var utsname unix.Utsname
	if err := unix.Uname(&utsname); err != nil {
		return KernelVersion{}, fmt.Errorf("failed to get kernel version: %w", err)
	}

	release := string(utsname.Release[:])
	release = strings.TrimRight(release, "\x00")

	parts := strings.Split(release, ".")
	if len(parts) < 2 {
		return KernelVersion{}, fmt.Errorf("unable to parse kernel version: %s", release)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return KernelVersion{}, fmt.Errorf("unable to parse major version: %w", err)
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return KernelVersion{}, fmt.Errorf("unable to parse minor version: %w", err)
	}

	patch := 0
	if len(parts) >= 3 {
		patchStr := strings.Split(parts[2], "-")[0]
		patch, _ = strconv.Atoi(patchStr)
	}

	return KernelVersion{
		Major: major,
		Minor: minor,
		Patch: patch,
		Full:  release,
	}, nil
}

// IsKernelVersionAtLeast checks if the current kernel version is at least the specified version.
func IsKernelVersionAtLeast(major, minor, patch int) bool {
	kv, err := GetKernelVersion()
	if err != nil {
		return false
	}

	if kv.Major > major {
		return true
	}
	if kv.Major < major {
		return false
	}

	// Major versions are equal
	if kv.Minor > minor {
		return true
	}
	if kv.Minor < minor {
		return false
	}

	// Major and minor versions are equal
	return kv.Patch >= patch
}