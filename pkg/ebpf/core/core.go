// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

// Package core provides CO-RE (Compile Once - Run Everywhere) support for eBPF programs.
package core

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/btf"
	"github.com/cilium/ebpf/features"
	"github.com/go-logr/logr"
)

type KernelFeatures struct {
	KernelVersion  string
	HasBTF         bool
	BTFPath        string
	CORESupport    string // "full", "partial", "none"
	RequiredKernel string // Minimum kernel version for features
}

type Manager struct {
	logger         logr.Logger
	kernelBTF      *btf.Spec
	kernelFeatures *KernelFeatures
}

// kernelVersionRequirement defines a minimum kernel version for a feature
type kernelVersionRequirement struct {
	major, minor int
	feature      string
}

var (
	// Define all kernel version requirements in one place
	requirementCORE     = kernelVersionRequirement{4, 18, "CO-RE"}
	requirementFullCORE = kernelVersionRequirement{5, 2, "full CO-RE with BTF"}
)

func NewManager(logger logr.Logger) (*Manager, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("CO-RE is only supported on Linux")
	}

	features, err := detectKernelFeatures()
	if err != nil {
		return nil, fmt.Errorf("detecting kernel features: %w", err)
	}

	logger.Info("Kernel CO-RE features detected",
		"kernel", features.KernelVersion,
		"btf", features.HasBTF,
		"core_support", features.CORESupport,
	)

	var kernelBTF *btf.Spec
	if features.HasBTF {
		// Try to load kernel BTF
		kernelBTF, err = btf.LoadKernelSpec()
		if err != nil {
			logger.Error(err, "Failed to load kernel BTF, CO-RE relocations may fail")
			// Don't fail here - cilium/ebpf can handle missing BTF gracefully
		}
	}

	return &Manager{
		logger:         logger,
		kernelBTF:      kernelBTF,
		kernelFeatures: features,
	}, nil
}

// LoadCollection loads an eBPF collection with CO-RE support.
func (m *Manager) LoadCollection(path string) (*ebpf.Collection, error) {
	spec, err := ebpf.LoadCollectionSpec(path)
	if err != nil {
		return nil, fmt.Errorf("loading collection spec: %w", err)
	}

	// If we have kernel BTF, it will be used automatically by cilium/ebpf
	// for CO-RE relocations during collection creation
	opts := ebpf.CollectionOptions{}
	if m.kernelBTF != nil {
		// The cilium/ebpf library handles BTF automatically when available
		m.logger.V(1).Info("Loading with kernel BTF for CO-RE relocations")
	}

	coll, err := ebpf.NewCollectionWithOptions(spec, opts)
	if err != nil {
		return nil, fmt.Errorf("creating collection: %w", err)
	}

	return coll, nil
}

// GetKernelFeatures returns information about kernel CO-RE support.
func (m *Manager) GetKernelFeatures() *KernelFeatures {
	return m.kernelFeatures
}

// HasFullCORESupport returns true if the kernel has full CO-RE support.
func (m *Manager) HasFullCORESupport() bool {
	return m.kernelFeatures.CORESupport == "full"
}

// detectKernelFeatures checks the running kernel for CO-RE capabilities.
func detectKernelFeatures() (*KernelFeatures, error) {
	features := &KernelFeatures{
		KernelVersion: getKernelVersion(),
	}

	// Check for native BTF support
	btfPath := "/sys/kernel/btf/vmlinux"
	if _, err := os.Stat(btfPath); err == nil {
		features.HasBTF = true
		features.BTFPath = btfPath
	}

	// Determine CO-RE support level based on kernel version
	major, minor, _ := parseKernelVersion(features.KernelVersion)

	switch {
	case meetsRequirement(major, minor, requirementFullCORE):
		// Kernel 5.2+ has full CO-RE support with native BTF
		features.CORESupport = "full"
		features.RequiredKernel = "5.2"
	case meetsRequirement(major, minor, requirementCORE):
		// Kernel 4.18-5.1 can use CO-RE with external BTF
		features.CORESupport = "partial"
		features.RequiredKernel = "4.18"
	default:
		// Older kernels don't support CO-RE
		features.CORESupport = "none"
		features.RequiredKernel = "4.18"
	}

	return features, nil
}

func getKernelVersion() string {
	// Try to read from /proc/version first
	data, err := os.ReadFile("/proc/version")
	if err == nil {
		parts := strings.Fields(string(data))
		if len(parts) >= 3 {
			return parts[2]
		}
	}

	// Fallback to unknown if /proc/version is not available
	return "unknown"
}

func parseKernelVersion(version string) (major, minor, patch int) {
	// Remove any suffix (e.g., "-generic")
	parts := strings.Split(version, "-")
	if len(parts) > 0 {
		version = parts[0]
	}

	// Parse x.y.z format using a simpler approach
	nums := strings.Split(version, ".")
	parseVersionNumber := func(idx int) int {
		if idx < len(nums) {
			var val int
			if _, err := fmt.Sscanf(nums[idx], "%d", &val); err != nil {
				return 0
			}
			return val
		}
		return 0
	}

	major = parseVersionNumber(0)
	minor = parseVersionNumber(1)
	patch = parseVersionNumber(2)

	return major, minor, patch
}

func meetsRequirement(major, minor int, req kernelVersionRequirement) bool {
	return major > req.major || (major == req.major && minor >= req.minor)
}

// CheckBTFSupport checks if the kernel has BTF support.
// BTF (BPF Type Format) is required for CO-RE and is available in kernel 5.2+.
func CheckBTFSupport() error {
	btfPath := "/sys/kernel/btf/vmlinux"
	if _, err := os.Stat(btfPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("BTF not supported: %s not found", btfPath)
		}
		return fmt.Errorf("checking BTF support: %w", err)
	}
	return nil
}

// CheckRingBufferSupport checks if the kernel supports BPF ring buffer.
// Ring buffer is available in kernel 5.8+.
func CheckRingBufferSupport() error {
	version := getKernelVersion()
	major, minor, _ := parseKernelVersion(version)
	
	if !meetsRequirement(major, minor, kernelVersionRequirement{5, 8, "Ring Buffer"}) {
		return fmt.Errorf("ring buffer requires kernel 5.8+, current kernel is %s", version)
	}
	
	// Additional runtime check via feature detection
	// Ring buffer support is checked via map type availability
	if err := features.HaveMapType(ebpf.RingBuf); err != nil {
		return fmt.Errorf("ring buffer support check failed: %w", err)
	}
	
	return nil
}

// CheckCORESupport checks if the kernel supports CO-RE.
// CO-RE (Compile Once - Run Everywhere) requires kernel 4.18+ for basic support,
// and 5.2+ for full support with native BTF.
func CheckCORESupport() error {
	version := getKernelVersion()
	major, minor, _ := parseKernelVersion(version)
	
	if !meetsRequirement(major, minor, requirementCORE) {
		return fmt.Errorf("CO-RE requires kernel 4.18+, current kernel is %s", version)
	}
	
	// Check if we have at least partial CO-RE support
	features, err := detectKernelFeatures()
	if err != nil {
		return fmt.Errorf("detecting CO-RE support: %w", err)
	}
	
	if features.CORESupport == "none" {
		return fmt.Errorf("CO-RE not supported on kernel %s", version)
	}
	
	return nil
}

// CheckPerfBufferSupport checks if the kernel supports BPF perf buffer.
// Perf buffer is available in kernel 4.4+.
func CheckPerfBufferSupport() error {
	version := getKernelVersion()
	major, minor, _ := parseKernelVersion(version)
	
	if !meetsRequirement(major, minor, kernelVersionRequirement{4, 4, "Perf Buffer"}) {
		return fmt.Errorf("perf buffer requires kernel 4.4+, current kernel is %s", version)
	}
	
	// Additional runtime check via feature detection
	if err := features.HaveProgramType(ebpf.Kprobe); err != nil {
		return fmt.Errorf("perf buffer support check failed: %w", err)
	}
	
	return nil
}
