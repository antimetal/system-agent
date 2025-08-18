// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package capabilities

import (
	"fmt"

	"github.com/antimetal/agent/pkg/kernel"
)

// KernelVersion interface for kernel version comparison
type KernelVersion interface {
	IsAtLeast(major, minor int) bool
}

// DetectKernelVersion detects the current kernel version
func DetectKernelVersion() (KernelVersion, error) {
	return kernel.GetCurrentVersion()
}

// Capability represents a Linux capability
type Capability int

const (
	// CAP_SYS_ADMIN allows a range of system administration operations
	// Required for eBPF on older kernels, tracepoints
	CAP_SYS_ADMIN Capability = 21

	// CAP_SYSLOG allows reading kernel message ring buffer via /dev/kmsg
	// Required for kernel message collection
	CAP_SYSLOG Capability = 34

	// CAP_BPF allows loading BPF programs and maps (kernel 5.8+)
	// Required for eBPF programs on newer kernels
	CAP_BPF Capability = 39

	// CAP_PERFMON allows performance monitoring operations (kernel 5.8+)
	// Required for eBPF performance monitoring programs
	CAP_PERFMON Capability = 38
)

// String returns the string representation of the capability
func (c Capability) String() string {
	switch c {
	case CAP_SYS_ADMIN:
		return "CAP_SYS_ADMIN"
	case CAP_SYSLOG:
		return "CAP_SYSLOG"
	case CAP_BPF:
		return "CAP_BPF"
	case CAP_PERFMON:
		return "CAP_PERFMON"
	default:
		return "UNKNOWN"
	}
}

// MissingCapabilityError represents a missing capability error
type MissingCapabilityError struct {
	Capability Capability
}

func (e MissingCapabilityError) Error() string {
	return fmt.Sprintf("missing capability: %s", e.Capability.String())
}

// GetEBPFCapabilities returns the capabilities required for eBPF programs
// based on kernel version. For newer kernels (5.8+), returns CAP_BPF and CAP_PERFMON.
// For older kernels, returns CAP_SYS_ADMIN.
func GetEBPFCapabilities() []Capability {
	return GetEBPFCapabilitiesForKernel(nil)
}

// GetEBPFCapabilitiesForKernel returns the capabilities required for eBPF programs
// based on the provided kernel version. If kernelVersion is nil, it will detect
// the current kernel version automatically.
func GetEBPFCapabilitiesForKernel(kernelVersion KernelVersion) []Capability {
	// If no kernel version provided, try to detect it
	if kernelVersion == nil {
		detected, err := DetectKernelVersion()
		if err != nil {
			// If we can't detect kernel version, fall back to legacy capabilities
			// for maximum compatibility
			return []Capability{CAP_SYS_ADMIN}
		}
		kernelVersion = detected
	}

	// Kernel 5.8+ introduced CAP_BPF and CAP_PERFMON
	if kernelVersion.IsAtLeast(5, 8) {
		return []Capability{CAP_BPF, CAP_PERFMON}
	}

	// For older kernels, use CAP_SYS_ADMIN
	return []Capability{CAP_SYS_ADMIN}
}
