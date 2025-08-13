// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux
// +build linux

package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"golang.org/x/sys/unix"
)

func (c *ProfilerCollector) attachPerfEvent(prog *ebpf.Program, cpu int) (link.Link, error) {
	attr := unix.PerfEventAttr{
		Type:        c.resolvedEventConfig.Type,
		Config:      c.resolvedEventConfig.Config,
		Sample_type: unix.PERF_SAMPLE_RAW,
		Sample:      c.resolvedEventConfig.SamplePeriod,
		Wakeup:      1,
		Size:        uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
	}

	fd, err := unix.PerfEventOpen(&attr, -1, cpu, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("perf_event_open failed for event %s (type=%d, config=%d) on CPU %d: %w",
			c.resolvedEventConfig.Name, c.resolvedEventConfig.Type, c.resolvedEventConfig.Config, cpu, err)
	}

	perfLink, err := link.AttachRawLink(link.RawLinkOptions{
		Target:  fd,
		Program: prog,
		Attach:  ebpf.AttachPerfEvent,
	})
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("attaching BPF program to perf event: %w", err)
	}

	return perfLink, nil
}

func (c *ProfilerCollector) onlineCPUs() ([]int, error) {
	// Try to read from /sys/devices/system/cpu/online
	onlinePath := filepath.Join(c.sysPath, "devices/system/cpu/online")
	data, err := os.ReadFile(onlinePath)
	if err != nil {
		// Fallback: try to count CPU directories
		return c.countCPUDirs()
	}

	cpuList := strings.TrimSpace(string(data))
	return parseCPUList(cpuList)
}

// countCPUDirs counts cpu[0-9]+ directories as a fallback
func (c *ProfilerCollector) countCPUDirs() ([]int, error) {
	cpuPath := filepath.Join(c.sysPath, "devices/system/cpu")
	entries, err := os.ReadDir(cpuPath)
	if err != nil {
		return nil, fmt.Errorf("reading CPU directory: %w", err)
	}

	var cpus []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "cpu") && len(name) > 3 {
			cpuStr := name[3:]
			if cpu, err := strconv.Atoi(cpuStr); err == nil {
				cpus = append(cpus, cpu)
			}
		}
	}

	if len(cpus) == 0 {
		// Last resort: assume at least 1 CPU
		return []int{0}, nil
	}

	return cpus, nil
}

// parseCPUList parses CPU ranges like "0-3,5,7-8" into a slice of CPU numbers
// XXX This looks like logic I've seen before. is there something to reuse here?
func parseCPUList(cpuList string) ([]int, error) {
	var cpus []int

	// Handle empty or invalid input
	if cpuList == "" {
		return []int{0}, nil
	}

	// Split by comma
	ranges := strings.Split(cpuList, ",")
	for _, r := range ranges {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}

		// Check if it's a range (contains hyphen)
		if strings.Contains(r, "-") {
			parts := strings.Split(r, "-")
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid CPU range: %s", r)
			}

			start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid CPU range start: %s", parts[0])
			}

			end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid CPU range end: %s", parts[1])
			}

			if start > end {
				return nil, fmt.Errorf("invalid CPU range: start > end (%d > %d)", start, end)
			}

			for i := start; i <= end; i++ {
				cpus = append(cpus, i)
			}
		} else {
			// Single CPU number
			cpu, err := strconv.Atoi(r)
			if err != nil {
				return nil, fmt.Errorf("invalid CPU number: %s", r)
			}
			cpus = append(cpus, cpu)
		}
	}

	if len(cpus) == 0 {
		// Default to at least CPU 0
		return []int{0}, nil
	}

	return cpus, nil
}
