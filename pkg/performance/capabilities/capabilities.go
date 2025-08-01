// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package capabilities

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

// GetEBPFCapabilities returns the capabilities required for eBPF programs
// based on kernel version. For newer kernels (5.8+), returns CAP_BPF and CAP_PERFMON.
// For older kernels, returns CAP_SYS_ADMIN.
func GetEBPFCapabilities() []Capability {
	// For maximum compatibility, we return both modern and legacy capabilities
	// The actual capability checking logic will determine which ones are available
	return []Capability{CAP_BPF, CAP_PERFMON, CAP_SYS_ADMIN}
}
