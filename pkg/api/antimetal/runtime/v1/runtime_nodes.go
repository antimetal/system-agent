// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtimev1

import (
	"time"
)

// ContainerRuntime defines the supported container runtime implementations.
type ContainerRuntime int32

const (
	ContainerRuntimeUnknown         ContainerRuntime = 0
	ContainerRuntimeDocker          ContainerRuntime = 1
	ContainerRuntimeContainerd      ContainerRuntime = 2
	ContainerRuntimeCRIContainerd   ContainerRuntime = 3
	ContainerRuntimeCRIO            ContainerRuntime = 4
	ContainerRuntimePodman          ContainerRuntime = 5
)

// String returns the string representation of ContainerRuntime
func (r ContainerRuntime) String() string {
	switch r {
	case ContainerRuntimeDocker:
		return "docker"
	case ContainerRuntimeContainerd:
		return "containerd"
	case ContainerRuntimeCRIContainerd:
		return "cri-containerd"
	case ContainerRuntimeCRIO:
		return "cri-o"
	case ContainerRuntimePodman:
		return "podman"
	default:
		return "unknown"
	}
}

// ProcessState defines the possible states of a Linux process.
type ProcessState int32

const (
	ProcessStateUnknown     ProcessState = 0
	ProcessStateRunning     ProcessState = 1
	ProcessStateSleeping    ProcessState = 2
	ProcessStateDiskSleep   ProcessState = 3
	ProcessStateZombie      ProcessState = 4
	ProcessStateStopped     ProcessState = 5
	ProcessStateTracingStop ProcessState = 6
	ProcessStatePaging      ProcessState = 7
	ProcessStateDead        ProcessState = 8
	ProcessStateWakeKill    ProcessState = 9
	ProcessStateWaking      ProcessState = 10
	ProcessStateParked      ProcessState = 11
)

// String returns the string representation of ProcessState
func (s ProcessState) String() string {
	switch s {
	case ProcessStateRunning:
		return "R"
	case ProcessStateSleeping:
		return "S"
	case ProcessStateDiskSleep:
		return "D"
	case ProcessStateZombie:
		return "Z"
	case ProcessStateStopped:
		return "T"
	case ProcessStateTracingStop:
		return "t"
	case ProcessStatePaging:
		return "W"
	case ProcessStateDead:
		return "X"
	case ProcessStateWakeKill:
		return "K"
	case ProcessStateWaking:
		return "W"
	case ProcessStateParked:
		return "P"
	default:
		return "?"
	}
}

// ContainerNode represents a discovered container in the runtime topology.
// Container nodes connect to hardware resources and contain process nodes.
type ContainerNode struct {
	// ContainerID is the unique identifier for the container (may be truncated for some runtimes).
	ContainerID string `json:"container_id"`
	
	// Runtime identifies the container runtime that manages this container.
	Runtime ContainerRuntime `json:"runtime"`
	
	// CgroupVersion indicates whether this container uses cgroup v1 or v2.
	// Different runtimes on the same host may use different cgroup versions.
	CgroupVersion CgroupVersion `json:"cgroup_version"`
	
	// CgroupPath is the filesystem path to the container's cgroup directory.
	CgroupPath string `json:"cgroup_path"`
	
	// ImageName is the container image name (e.g., "nginx", "alpine").
	ImageName string `json:"image_name,omitempty"`
	
	// ImageTag is the container image tag (e.g., "latest", "1.21").
	ImageTag string `json:"image_tag,omitempty"`
	
	// Labels contains runtime-specific labels and annotations.
	Labels map[string]string `json:"labels,omitempty"`
	
	// CreatedAt is when the container was created.
	CreatedAt *time.Time `json:"created_at,omitempty"`
	
	// StartedAt is when the container was started (may differ from created_at).
	StartedAt *time.Time `json:"started_at,omitempty"`
	
	// CPUShares represents the relative CPU weight (cgroup v1 cpu.shares).
	CPUShares *int32 `json:"cpu_shares,omitempty"`
	
	// CPUQuotaUs represents the CPU quota in microseconds per period.
	CPUQuotaUs *int32 `json:"cpu_quota_us,omitempty"`
	
	// CPUPeriodUs represents the CPU quota enforcement period in microseconds.
	CPUPeriodUs *int32 `json:"cpu_period_us,omitempty"`
	
	// MemoryLimitBytes represents the memory limit in bytes (if set).
	MemoryLimitBytes *uint64 `json:"memory_limit_bytes,omitempty"`
	
	// CpusetCpus contains the CPU cores this container is allowed to use.
	CpusetCpus string `json:"cpuset_cpus,omitempty"`
	
	// CpusetMems contains the NUMA memory nodes this container is allowed to use.
	CpusetMems string `json:"cpuset_mems,omitempty"`
}

// ProcessNode represents a running process in the runtime topology.
// Process nodes connect to containers via relationships and hardware resources.
type ProcessNode struct {
	// PID is the process identifier.
	PID int32 `json:"pid"`
	
	// PPID is the parent process identifier.
	PPID int32 `json:"ppid"`
	
	// PGID is the process group identifier.
	PGID int32 `json:"pgid"`
	
	// SID is the session identifier.
	SID int32 `json:"sid"`
	
	// Command is the process command name.
	Command string `json:"command"`
	
	// Cmdline is the full command line with arguments.
	Cmdline string `json:"cmdline,omitempty"`
	
	// State is the current process state.
	State ProcessState `json:"state"`
	
	// StartTime is when the process was started.
	StartTime time.Time `json:"start_time"`
}