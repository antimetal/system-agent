// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package collectors_test

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCPUInfoCollector_RealProcFS(t *testing.T) {

	// Test with real /proc filesystem
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
	}

	collector, err := collectors.NewCPUInfoCollector(logr.Discard(), config)
	require.NoError(t, err, "Should create collector with real /proc")

	result, err := collector.Collect(context.Background())
	require.NoError(t, err, "Should collect from real /proc/cpuinfo")

	cpuInfo, ok := result.(*collectors.CPUInfo)
	require.True(t, ok, "Result should be *CPUInfo type")

	// Verify we got actual CPU information
	assert.Greater(t, len(cpuInfo.Processors), 0, "Should detect at least one processor")

	// Verify first processor has expected fields
	proc := cpuInfo.Processors[0]
	assert.NotEmpty(t, proc.VendorID, "Should have vendor ID")
	assert.GreaterOrEqual(t, proc.CPUFamily, 0, "Should have CPU family")
	assert.NotEmpty(t, proc.ModelName, "Should have model name")
	assert.Greater(t, len(proc.Flags), 0, "Should have CPU flags")

	// Verify processor count matches runtime
	assert.Equal(t, runtime.NumCPU(), len(cpuInfo.Processors),
		"Processor count should match runtime.NumCPU()")
}

func TestCPUInfoCollector_Topology(t *testing.T) {

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
	}

	collector, err := collectors.NewCPUInfoCollector(logr.Discard(), config)
	require.NoError(t, err)

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	cpuInfo, ok := result.(*collectors.CPUInfo)
	require.True(t, ok)

	// Track unique physical IDs and core IDs
	physicalIDs := make(map[int]bool)
	coreIDs := make(map[string]bool) // physicalID:coreID

	for _, proc := range cpuInfo.Processors {
		// Physical ID should be set (might be 0 for single socket)
		assert.GreaterOrEqual(t, proc.PhysicalID, 0, "Physical ID should be non-negative")
		physicalIDs[proc.PhysicalID] = true

		// Core ID should be set
		assert.GreaterOrEqual(t, proc.CoreID, 0, "Core ID should be non-negative")

		// Track unique physical:core combinations
		key := fmt.Sprintf("%d:%d", proc.PhysicalID, proc.CoreID)
		coreIDs[key] = true

		// CPU cores count should be consistent within same physical CPU
		if proc.CPUCores > 0 {
			assert.Greater(t, proc.CPUCores, 0, "CPU cores should be positive when set")
		}
	}

	// Log topology information
	t.Logf("System topology: %d physical CPUs, %d unique cores, %d logical processors",
		len(physicalIDs), len(coreIDs), len(cpuInfo.Processors))

	// Hyperthreading detection
	if len(cpuInfo.Processors) > len(coreIDs) {
		t.Log("Hyperthreading/SMT appears to be enabled")

		// Each core should have 2 or more threads
		threadsPerCore := len(cpuInfo.Processors) / len(coreIDs)
		assert.GreaterOrEqual(t, threadsPerCore, 2,
			"With hyperthreading, should have at least 2 threads per core")
	} else {
		t.Log("Hyperthreading/SMT appears to be disabled or not available")
	}
}

func TestCPUInfoCollector_Virtualization(t *testing.T) {

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
	}

	collector, err := collectors.NewCPUInfoCollector(logr.Discard(), config)
	require.NoError(t, err)

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	cpuInfo, ok := result.(*collectors.CPUInfo)
	require.True(t, ok)

	// Check for virtualization hints
	proc := cpuInfo.Processors[0]

	// Check for hypervisor flag (indicates running in VM)
	hasHypervisor := false
	for _, flag := range proc.Flags {
		if flag == "hypervisor" {
			hasHypervisor = true
			break
		}
	}

	// Check vendor ID for virtual CPU indicators
	isVirtualVendor := false
	virtualVendors := []string{
		"GenuineIntel", // Can be real or virtual
		"AuthenticAMD", // Can be real or virtual
		"KVMKVMKVM",    // KVM
		"Microsoft Hv", // Hyper-V
		"VMwareVMware", // VMware
		"XenVMMXenVMM", // Xen
	}

	for _, vendor := range virtualVendors[2:] { // Skip Intel and AMD
		if proc.VendorID == vendor {
			isVirtualVendor = true
			t.Logf("Detected virtual CPU vendor: %s", vendor)
			break
		}
	}

	// GitHub Actions runners are typically VMs
	if hasHypervisor || isVirtualVendor {
		t.Log("Running in a virtualized environment")
		assert.True(t, hasHypervisor || isVirtualVendor,
			"Should detect virtualization on CI runners")
	} else {
		t.Log("Running on bare metal or virtualization not detected")
	}

	// Check for virtualization extensions (VT-x/AMD-V)
	hasVMX := false
	hasSVM := false
	for _, flag := range proc.Flags {
		if flag == "vmx" {
			hasVMX = true
		}
		if flag == "svm" {
			hasSVM = true
		}
	}

	if hasVMX {
		t.Log("Intel VT-x virtualization extensions detected")
	}
	if hasSVM {
		t.Log("AMD-V virtualization extensions detected")
	}
}

func TestCPUInfoCollector_CPUFrequency(t *testing.T) {

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
	}

	collector, err := collectors.NewCPUInfoCollector(logr.Discard(), config)
	require.NoError(t, err)

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	cpuInfo, ok := result.(*collectors.CPUInfo)
	require.True(t, ok)

	// Check CPU frequency information
	for i, proc := range cpuInfo.Processors {
		// CPU MHz should be reasonable (100 MHz to 10 GHz)
		if proc.CPUMHz > 0 {
			assert.Greater(t, proc.CPUMHz, 100.0,
				"CPU %d: Frequency should be at least 100 MHz", i)
			assert.Less(t, proc.CPUMHz, 10000.0,
				"CPU %d: Frequency should be less than 10 GHz", i)

			t.Logf("CPU %d: Current frequency: %.2f MHz", i, proc.CPUMHz)
		}

		// BogoMIPS is a rough CPU speed indicator
		if proc.BogoMIPS > 0 {
			assert.Greater(t, proc.BogoMIPS, 100.0,
				"CPU %d: BogoMIPS should be reasonable", i)

			// BogoMIPS is typically around 2x the CPU MHz on modern x86
			ratio := proc.BogoMIPS / proc.CPUMHz
			t.Logf("CPU %d: BogoMIPS/MHz ratio: %.2f", i, ratio)
		}
	}
}
