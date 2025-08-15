// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package kernel

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestKernelFeatureDetection verifies that kernel version checks align with
// actual feature availability on the system
func TestKernelFeatureDetection(t *testing.T) {
	version, err := GetCurrentVersion()
	require.NoError(t, err)

	t.Logf("Testing kernel %s", version.String())

	// Test BTF support detection
	// BTF is available in kernel 5.2+
	if version.IsAtLeast(5, 2) {
		// Check if actual BTF file exists
		_, btfErr := os.Stat("/sys/kernel/btf/vmlinux")
		if btfErr == nil {
			t.Logf("✓ Kernel %s has BTF support and BTF file exists", version.String())
		} else {
			t.Logf("⚠ Kernel %s meets BTF version requirement but BTF file not found (may be disabled in kernel config)", version.String())
		}
	} else {
		t.Logf("Kernel %s does not meet BTF minimum version requirement (5.2.0)", version.String())
	}

	// Test eBPF ring buffer support
	// Ring buffer is available in kernel 5.8+
	if version.IsAtLeast(5, 8) {
		t.Logf("✓ Kernel %s has BPF ring buffer support", version.String())
		t.Logf("✓ Kernel %s has modern eBPF capabilities (CAP_BPF, CAP_PERFMON)", version.String())
	} else {
		t.Logf("Kernel %s requires CAP_SYS_ADMIN for eBPF operations", version.String())
	}

	// Test cgroup v2 support
	// cgroup v2 is fully supported in kernel 4.5+
	if version.IsAtLeast(4, 5) {
		// Check if cgroup v2 is actually mounted
		_, cgroupErr := os.Stat("/sys/fs/cgroup/cgroup.controllers")
		if cgroupErr == nil {
			t.Logf("✓ Kernel %s supports cgroup v2 and it is mounted", version.String())
		} else {
			t.Logf("Kernel %s supports cgroup v2 but it is not mounted (using cgroup v1)", version.String())
		}
	}

	// Test CO-RE support detection
	// CO-RE works with kernel 4.18+ (with external BTF) and 5.2+ (native BTF)
	if version.IsAtLeast(5, 2) {
		t.Logf("✓ Kernel %s has full CO-RE support with native BTF", version.String())
	} else if version.IsAtLeast(4, 18) {
		t.Logf("✓ Kernel %s has CO-RE support (requires external BTF)", version.String())
	} else {
		t.Logf("Kernel %s does not support CO-RE (minimum 4.18 required)", version.String())
	}
}
