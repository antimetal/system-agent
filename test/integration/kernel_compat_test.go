// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package integration

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// KernelVersion represents a parsed kernel version
type KernelVersion struct {
	Major int
	Minor int
	Patch int
}

// ParseKernelVersion parses a kernel version string
func ParseKernelVersion(version string) (KernelVersion, error) {
	var kv KernelVersion
	_, err := fmt.Sscanf(version, "%d.%d.%d", &kv.Major, &kv.Minor, &kv.Patch)
	if err != nil {
		return kv, fmt.Errorf("failed to parse kernel version %q: %w", version, err)
	}
	return kv, nil
}

// Compare returns -1 if v < other, 0 if v == other, 1 if v > other
func (v KernelVersion) Compare(other KernelVersion) int {
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

// GetCurrentKernelVersion returns the current kernel version
func GetCurrentKernelVersion() (KernelVersion, error) {
	var uname unix.Utsname
	if err := unix.Uname(&uname); err != nil {
		return KernelVersion{}, fmt.Errorf("failed to get kernel version: %w", err)
	}

	release := string(uname.Release[:])
	release = strings.TrimRight(release, "\x00")

	// Extract version from release string (e.g., "5.15.0-generic" -> "5.15.0")
	parts := strings.Split(release, "-")
	if len(parts) == 0 {
		return KernelVersion{}, fmt.Errorf("invalid kernel release string: %s", release)
	}

	return ParseKernelVersion(parts[0])
}

// KernelFeature represents a kernel feature to test
type KernelFeature struct {
	Name        string
	MinVersion  KernelVersion
	CheckFunc   func() error
	Description string
}

func TestKernelCompatibility(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Kernel compatibility tests only run on Linux")
	}

	currentKernel, err := GetCurrentKernelVersion()
	require.NoError(t, err, "Failed to get current kernel version")

	t.Logf("Running on kernel version: %d.%d.%d", currentKernel.Major, currentKernel.Minor, currentKernel.Patch)

	// TODO: These functions were removed from core.go during refactoring
	// They should be re-implemented if kernel feature checking is needed
	features := []KernelFeature{
		// {
		// 	Name:        "BTF Support",
		// 	MinVersion:  KernelVersion{5, 2, 0},
		// 	CheckFunc:   core.CheckBTFSupport,
		// 	Description: "Native BTF (BPF Type Format) support",
		// },
		// {
		// 	Name:        "Ring Buffer",
		// 	MinVersion:  KernelVersion{5, 8, 0},
		// 	CheckFunc:   core.CheckRingBufferSupport,
		// 	Description: "BPF Ring Buffer for efficient data transfer",
		// },
		// {
		// 	Name:        "CO-RE Support",
		// 	MinVersion:  KernelVersion{4, 18, 0},
		// 	CheckFunc:   core.CheckCORESupport,
		// 	Description: "Compile Once - Run Everywhere support",
		// },
		// {
		// 	Name:        "Perf Buffer",
		// 	MinVersion:  KernelVersion{4, 4, 0},
		// 	CheckFunc:   core.CheckPerfBufferSupport,
		// 	Description: "BPF Perf Buffer support",
		// },
	}

	for _, feature := range features {
		t.Run(feature.Name, func(t *testing.T) {
			err := feature.CheckFunc()

			if currentKernel.Compare(feature.MinVersion) >= 0 {
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

	currentKernel, err := GetCurrentKernelVersion()
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
		minKernel    KernelVersion
		requiresRoot bool
		requiresEBPF bool
	}{
		{
			name:       "LoadCollector",
			metricType: performance.MetricTypeLoad,
			minKernel:  KernelVersion{2, 6, 0},
		},
		{
			name:       "MemoryCollector",
			metricType: performance.MetricTypeMemory,
			minKernel:  KernelVersion{2, 6, 0},
		},
		{
			name:       "CPUCollector",
			metricType: performance.MetricTypeCPU,
			minKernel:  KernelVersion{2, 6, 0},
		},
		{
			name:       "NetworkCollector",
			metricType: performance.MetricTypeNetwork,
			minKernel:  KernelVersion{2, 6, 0},
		},
		{
			name:       "DiskCollector",
			metricType: performance.MetricTypeDisk,
			minKernel:  KernelVersion{2, 6, 0},
		},
		{
			name:       "ProcessCollector",
			metricType: performance.MetricTypeProcess,
			minKernel:  KernelVersion{2, 6, 0},
		},
		{
			name:       "NUMACollector",
			metricType: performance.MetricTypeNUMA,
			minKernel:  KernelVersion{2, 6, 0},
		},
	}

	for _, tc := range collectorTests {
		t.Run(tc.name, func(t *testing.T) {
			// Skip if we don't have root and collector requires it
			if tc.requiresRoot && os.Geteuid() != 0 {
				t.Skipf("Skipping %s: requires root", tc.name)
			}

			// Check if kernel version is sufficient
			if currentKernel.Compare(tc.minKernel) < 0 {
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
		minKernel   KernelVersion
		description string
	}{
		{"/proc/loadavg", KernelVersion{2, 6, 0}, "Load average information"},
		{"/proc/meminfo", KernelVersion{2, 6, 0}, "Memory information"},
		{"/proc/stat", KernelVersion{2, 6, 0}, "CPU statistics"},
		{"/proc/net/dev", KernelVersion{2, 6, 0}, "Network device statistics"},
		{"/proc/diskstats", KernelVersion{2, 6, 0}, "Disk I/O statistics"},
		{"/proc/pressure/cpu", KernelVersion{4, 20, 0}, "CPU pressure stall information"},
		{"/proc/pressure/memory", KernelVersion{4, 20, 0}, "Memory pressure stall information"},
		{"/proc/pressure/io", KernelVersion{4, 20, 0}, "I/O pressure stall information"},
	}

	currentKernel, err := GetCurrentKernelVersion()
	require.NoError(t, err)

	for _, pf := range procFiles {
		t.Run(pf.path, func(t *testing.T) {
			_, err := os.Stat(pf.path)

			if currentKernel.Compare(pf.minKernel) >= 0 {
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
