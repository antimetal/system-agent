// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !linux
// +build !linux

package collectors

import (
	"fmt"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

// Non-Linux stub implementation of attachPerfEvent
func (c *ProfilerCollector) attachPerfEvent(prog *ebpf.Program, cpu int) (link.Link, error) {
	return nil, fmt.Errorf("perf event profiling is only supported on Linux")
}

// Non-Linux stub implementation of onlineCPUs
func (c *ProfilerCollector) onlineCPUs() ([]int, error) {
	return nil, fmt.Errorf("CPU detection is only supported on Linux")
}
