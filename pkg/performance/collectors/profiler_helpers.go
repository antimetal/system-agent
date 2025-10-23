// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"github.com/antimetal/agent/pkg/performance/cpuutil"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"golang.org/x/sys/unix"
)

// onlineCPUs returns a list of online CPUs
func (c *ProfilerCollector) onlineCPUs() ([]int, error) {
	// Try to read from /sys/devices/system/cpu/online first
	onlinePath := filepath.Join(c.sysPath, "devices/system/cpu/online")
	data, err := os.ReadFile(onlinePath)
	if err == nil {
		cpus32, err := cpuutil.ParseCPUList(string(data))
		if err != nil {
			return nil, err
		}
		// Convert []int32 to []int
		cpus := make([]int, len(cpus32))
		for i, cpu := range cpus32 {
			cpus[i] = int(cpu)
		}
		return cpus, nil
	}

	// Fallback: count cpu directories
	cpuDir := filepath.Join(c.sysPath, "devices/system/cpu")
	entries, err := os.ReadDir(cpuDir)
	if err != nil {
		return nil, fmt.Errorf("reading CPU directory: %w", err)
	}

	var cpus []int
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "cpu") {
			cpuStr := strings.TrimPrefix(entry.Name(), "cpu")
			if cpu, err := strconv.Atoi(cpuStr); err == nil {
				cpus = append(cpus, cpu)
			}
		}
	}

	if len(cpus) == 0 {
		// Last fallback: assume at least one CPU
		return []int{0}, nil
	}

	return cpus, nil
}

// attachPerfEvent attaches an eBPF program to a perf event on a specific CPU.
// It opens a perf event with the configured event type/config/sample period,
// then attaches the eBPF program to that event using PERF_EVENT_IOC_SET_BPF ioctl.
func (c *ProfilerCollector) attachPerfEvent(prog *ebpf.Program, cpu int) (link.Link, error) {
	if c.resolvedEventConfig == nil {
		return nil, fmt.Errorf("no event configuration available")
	}

	// Validate program
	if prog == nil {
		return nil, fmt.Errorf("program is nil")
	}
	if prog.FD() < 0 {
		return nil, fmt.Errorf("invalid program file descriptor")
	}

	// Configure perf event attributes
	// Use period-based sampling (Sample = period, not frequency)
	attr := unix.PerfEventAttr{
		Type:   c.resolvedEventConfig.Type,
		Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
		Config: c.resolvedEventConfig.Config,
		Sample: c.resolvedEventConfig.SamplePeriod,
		Bits:   0, // Period-based sampling (not frequency)
	}

	// Open the perf event on the specified CPU for all processes (-1)
	// This allows us to profile all activity on that CPU
	perfFD, err := unix.PerfEventOpen(&attr, -1, cpu, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("opening perf event on CPU %d: %w", cpu, err)
	}

	// Attach the eBPF program using the BPF link API
	// This creates a proper BPF link via BPF_LINK_CREATE syscall
	rawLink, err := link.AttachRawLink(link.RawLinkOptions{
		Target:  perfFD,
		Program: prog,
		Attach:  ebpf.AttachPerfEvent,
	})
	if err != nil {
		unix.Close(perfFD)
		return nil, fmt.Errorf("creating BPF link for perf event: %w", err)
	}

	// Enable the perf event after attaching the BPF program
	if err := unix.IoctlSetInt(perfFD, unix.PERF_EVENT_IOC_ENABLE, 0); err != nil {
		rawLink.Close()
		unix.Close(perfFD)
		return nil, fmt.Errorf("enabling perf event: %w", err)
	}

	return rawLink, nil
}
