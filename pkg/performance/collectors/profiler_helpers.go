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
	"syscall"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

// onlineCPUs returns a list of online CPUs
func (c *ProfilerCollector) onlineCPUs() ([]int, error) {
	// Try to read from /sys/devices/system/cpu/online first
	onlinePath := filepath.Join(c.sysPath, "devices/system/cpu/online")
	data, err := os.ReadFile(onlinePath)
	if err == nil {
		return parseCPUList(string(data))
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

// parseCPUList parses CPU list format like "0-3,5,7-9"
func parseCPUList(cpuList string) ([]int, error) {
	var cpus []int
	cpuList = strings.TrimSpace(cpuList)

	for _, part := range strings.Split(cpuList, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			// Range format: "0-3"
			bounds := strings.Split(part, "-")
			if len(bounds) != 2 {
				return nil, fmt.Errorf("invalid CPU range: %s", part)
			}

			start, err := strconv.Atoi(bounds[0])
			if err != nil {
				return nil, fmt.Errorf("invalid CPU range start: %s", bounds[0])
			}

			end, err := strconv.Atoi(bounds[1])
			if err != nil {
				return nil, fmt.Errorf("invalid CPU range end: %s", bounds[1])
			}

			for i := start; i <= end; i++ {
				cpus = append(cpus, i)
			}
		} else {
			// Single CPU
			cpu, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid CPU number: %s", part)
			}
			cpus = append(cpus, cpu)
		}
	}

	return cpus, nil
}

// attachPerfEvent attaches an eBPF program to a perf event on a specific CPU
func (c *ProfilerCollector) attachPerfEvent(prog *ebpf.Program, cpu int) (link.Link, error) {
	// Use the resolved event configuration
	if c.resolvedEventConfig == nil {
		return nil, fmt.Errorf("no event configuration set")
	}

	// Create perf event attributes
	attr := &syscall.PerfEventAttr{
		Type:   c.resolvedEventConfig.Type,
		Config: c.resolvedEventConfig.Config,
		Sample: c.resolvedEventConfig.SamplePeriod,
		Bits:   0x400 | 0x1, // PERF_SAMPLE_PERIOD | PERF_SAMPLE_FREQUENCY
		Size:   uint32(unsafe.Sizeof(syscall.PerfEventAttr{})),
	}

	// Open perf event
	fd, err := syscall.PerfEventOpen(
		attr,
		-1,  // pid (-1 = all processes)
		cpu, // cpu
		-1,  // group_fd
		0x8, // flags (PERF_FLAG_FD_CLOEXEC)
	)
	if err != nil {
		return nil, fmt.Errorf("opening perf event: %w", err)
	}

	// Attach eBPF program to perf event
	perfLink, err := link.AttachPerfEvent(link.PerfEventOptions{
		Program: prog,
		FD:      fd,
	})
	if err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("attaching to perf event: %w", err)
	}

	return perfLink, nil
}

// isPerfEventAvailable checks if a perf event is available on this system
func isPerfEventAvailable(eventType uint32, config uint64) bool {
	// Try to open the perf event with minimal configuration
	attr := &syscall.PerfEventAttr{
		Type:   eventType,
		Config: config,
		Size:   uint32(unsafe.Sizeof(syscall.PerfEventAttr{})),
		Bits:   0x8, // PERF_FLAG_FD_CLOEXEC
	}

	// Try to open on CPU 0 for the calling thread
	fd, err := syscall.PerfEventOpen(
		attr,
		0,   // pid (0 = current process)
		0,   // cpu
		-1,  // group_fd
		0x8, // flags (PERF_FLAG_FD_CLOEXEC)
	)

	if err != nil {
		return false
	}

	// Close the file descriptor
	syscall.Close(fd)
	return true
}

// GetAvailablePerfEventNames returns just the names of available perf events
func GetAvailablePerfEventNames() ([]string, error) {
	events, err := EnumerateAvailablePerfEvents()
	if err != nil {
		return nil, err
	}

	var names []string
	for _, event := range events {
		if event.Available {
			names = append(names, event.Name)
		}
	}

	return names, nil
}
