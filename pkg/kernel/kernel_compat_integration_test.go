// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package kernel_test

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/ebpf/core"
	"github.com/antimetal/agent/pkg/kernel"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// KernelFeature represents a kernel feature to test
type KernelFeature struct {
	Name        string
	MinVersion  kernel.Version
	CheckFunc   func() error
	Description string
}

func TestKernelCompatibility(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Kernel compatibility tests only run on Linux")
	}

	currentKernel, err := kernel.GetCurrentVersion()
	require.NoError(t, err, "Failed to get current kernel version")

	t.Logf("Running on kernel version: %d.%d.%d", currentKernel.Major, currentKernel.Minor, currentKernel.Patch)

	features := []KernelFeature{
		{
			Name:        "BTF Support",
			MinVersion:  kernel.Version{Major: 5, Minor: 2, Patch: 0},
			CheckFunc:   core.CheckBTFSupport,
			Description: "Native BTF (BPF Type Format) support",
		},
		{
			Name:        "Ring Buffer",
			MinVersion:  kernel.Version{Major: 5, Minor: 8, Patch: 0},
			CheckFunc:   core.CheckRingBufferSupport,
			Description: "BPF Ring Buffer for efficient data transfer",
		},
		{
			Name:        "CO-RE Support",
			MinVersion:  kernel.Version{Major: 4, Minor: 18, Patch: 0},
			CheckFunc:   core.CheckCORESupport,
			Description: "Compile Once - Run Everywhere support",
		},
		{
			Name:        "Perf Buffer",
			MinVersion:  kernel.Version{Major: 4, Minor: 4, Patch: 0},
			CheckFunc:   core.CheckPerfBufferSupport,
			Description: "BPF Perf Buffer support",
		},
	}

	for _, feature := range features {
		t.Run(feature.Name, func(t *testing.T) {
			err := feature.CheckFunc()

			if currentKernel.IsAtLeast(feature.MinVersion.Major, feature.MinVersion.Minor) {
				// Feature should be available
				if err != nil {
					t.Errorf("Feature %s should be available on kernel %d.%d.%d (min: %d.%d.%d) but got error: %v",
						feature.Name,
						currentKernel.Major, currentKernel.Minor, currentKernel.Patch,
						feature.MinVersion.Major, feature.MinVersion.Minor, feature.MinVersion.Patch,
						err)
				} else {
					t.Logf("✓ %s: %s", feature.Name, feature.Description)
				}
			} else {
				// Feature should not be available
				if err == nil {
					t.Errorf("Feature %s should not be available on kernel %d.%d.%d (requires %d.%d.%d+)",
						feature.Name,
						currentKernel.Major, currentKernel.Minor, currentKernel.Patch,
						feature.MinVersion.Major, feature.MinVersion.Minor, feature.MinVersion.Patch)
				} else {
					t.Logf("✓ %s: Correctly unavailable (requires kernel %d.%d.%d+)",
						feature.Name,
						feature.MinVersion.Major, feature.MinVersion.Minor, feature.MinVersion.Patch)
				}
			}
		})
	}
}

func TestCollectorCompatibility(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Collector compatibility tests only run on Linux")
	}

	currentKernel, err := kernel.GetCurrentVersion()
	require.NoError(t, err, "Failed to get current kernel version")

	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
		Interval:     time.Second, // Set a default interval for continuous collectors
	}

	logger := logr.Discard()

	// Test each collector type
	collectorTests := []struct {
		name         string
		metricType   performance.MetricType
		minKernel    kernel.Version
		requiresRoot bool
		requiresEBPF bool
	}{
		{
			name:       "LoadCollector",
			metricType: performance.MetricTypeLoad,
			minKernel:  kernel.Version{Major: 2, Minor: 6, Patch: 0},
		},
		{
			name:       "MemoryCollector",
			metricType: performance.MetricTypeMemory,
			minKernel:  kernel.Version{Major: 2, Minor: 6, Patch: 0},
		},
		{
			name:       "CPUCollector",
			metricType: performance.MetricTypeCPU,
			minKernel:  kernel.Version{Major: 2, Minor: 6, Patch: 0},
		},
		{
			name:       "NetworkCollector",
			metricType: performance.MetricTypeNetwork,
			minKernel:  kernel.Version{Major: 2, Minor: 6, Patch: 0},
		},
		{
			name:       "DiskCollector",
			metricType: performance.MetricTypeDisk,
			minKernel:  kernel.Version{Major: 2, Minor: 6, Patch: 0},
		},
		{
			name:       "ProcessCollector",
			metricType: performance.MetricTypeProcess,
			minKernel:  kernel.Version{Major: 2, Minor: 6, Patch: 0},
		},
		{
			name:       "NUMACollector",
			metricType: performance.MetricTypeNUMAStats,
			minKernel:  kernel.Version{Major: 2, Minor: 6, Patch: 0},
		},
	}

	for _, tc := range collectorTests {
		t.Run(tc.name, func(t *testing.T) {
			// Skip if we don't have root and collector requires it
			if tc.requiresRoot && os.Geteuid() != 0 {
				t.Skipf("Skipping %s: requires root", tc.name)
			}

			// Check if kernel version is sufficient
			if !currentKernel.IsAtLeast(tc.minKernel.Major, tc.minKernel.Minor) {
				t.Skipf("Skipping %s: requires kernel %d.%d.%d+, current is %d.%d.%d",
					tc.name,
					tc.minKernel.Major, tc.minKernel.Minor, tc.minKernel.Patch,
					currentKernel.Major, currentKernel.Minor, currentKernel.Patch)
			}

			// Try to create the collector
			constructor, err := performance.GetCollector(tc.metricType)
			if err != nil {
				t.Errorf("Collector constructor for %s not found in registry: %v", tc.name, err)
				return
			}

			collector, err := constructor(logger, config)
			if err != nil {
				t.Errorf("Failed to create %s: %v", tc.name, err)
				return
			}

			// Test collection
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			ch, err := collector.Start(ctx)
			if err != nil {
				t.Errorf("Failed to start %s: %v", tc.name, err)
				return
			}

			// Wait for at least one data point
			select {
			case data := <-ch:
				if data == nil {
					t.Errorf("%s produced nil data", tc.name)
				} else {
					t.Logf("✓ %s: Successfully collected data of type %T", tc.name, data)
				}
			case <-ctx.Done():
				t.Errorf("%s: Timeout waiting for data", tc.name)
			}

			// Stop the collector
			err = collector.Stop()
			assert.NoError(t, err, "Failed to stop %s", tc.name)
		})
	}
}

func TestProcFileCompatibility(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Proc file compatibility tests only run on Linux")
	}

	// Test that expected /proc files exist
	procFiles := []struct {
		path        string
		minKernel   kernel.Version
		description string
	}{
		{"/proc/loadavg", kernel.Version{Major: 2, Minor: 6, Patch: 0}, "Load average information"},
		{"/proc/meminfo", kernel.Version{Major: 2, Minor: 6, Patch: 0}, "Memory information"},
		{"/proc/stat", kernel.Version{Major: 2, Minor: 6, Patch: 0}, "CPU statistics"},
		{"/proc/net/dev", kernel.Version{Major: 2, Minor: 6, Patch: 0}, "Network device statistics"},
		{"/proc/diskstats", kernel.Version{Major: 2, Minor: 6, Patch: 0}, "Disk I/O statistics"},
		{"/proc/pressure/cpu", kernel.Version{Major: 4, Minor: 20, Patch: 0}, "CPU pressure stall information"},
		{"/proc/pressure/memory", kernel.Version{Major: 4, Minor: 20, Patch: 0}, "Memory pressure stall information"},
		{"/proc/pressure/io", kernel.Version{Major: 4, Minor: 20, Patch: 0}, "I/O pressure stall information"},
	}

	currentKernel, err := kernel.GetCurrentVersion()
	require.NoError(t, err)

	for _, pf := range procFiles {
		t.Run(pf.path, func(t *testing.T) {
			_, err := os.Stat(pf.path)

			if currentKernel.IsAtLeast(pf.minKernel.Major, pf.minKernel.Minor) {
				// File should exist
				if os.IsNotExist(err) {
					t.Errorf("File %s should exist on kernel %d.%d.%d+ but was not found",
						pf.path,
						pf.minKernel.Major, pf.minKernel.Minor, pf.minKernel.Patch)
				} else if err != nil {
					t.Errorf("Error accessing %s: %v", pf.path, err)
				} else {
					t.Logf("✓ %s: %s", pf.path, pf.description)
				}
			} else {
				// File might not exist
				if err == nil {
					t.Logf("Note: %s exists on kernel %d.%d.%d (min version: %d.%d.%d)",
						pf.path,
						currentKernel.Major, currentKernel.Minor, currentKernel.Patch,
						pf.minKernel.Major, pf.minKernel.Minor, pf.minKernel.Patch)
				} else if os.IsNotExist(err) {
					t.Logf("✓ %s: Correctly unavailable (requires kernel %d.%d.%d+)",
						pf.path,
						pf.minKernel.Major, pf.minKernel.Minor, pf.minKernel.Patch)
				}
			}
		})
	}
}
