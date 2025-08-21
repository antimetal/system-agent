// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	"fmt"
	"time"

	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// createContainerNode creates a container node and its resource reference
func (b *Builder) createContainerNode(container *ContainerInfo) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Convert string runtime to enum
	runtime := parseContainerRuntime(container.Runtime)
	
	// Convert cgroup version
	cgroupVersion := runtimev1.CgroupVersion(container.CgroupVersion)

	node := &runtimev1.ContainerNode{
		ContainerID:   container.ID,
		Runtime:       runtime,
		CgroupVersion: cgroupVersion,
		CgroupPath:    container.CgroupPath,
		ImageName:     container.ImageName,
		ImageTag:      container.ImageTag,
		Labels:        container.Labels,
		CPUShares:     container.CPUShares,
		CPUQuotaUs:    container.CPUQuotaUs,
		CPUPeriodUs:   container.CPUPeriodUs,
		MemoryLimitBytes: container.MemoryLimitBytes,
		CpusetCpus:    container.CpusetCpus,
		CpusetMems:    container.CpusetMems,
	}

	// Create resource reference
	ref := &resourcev1.ResourceRef{
		Namespace: &resourcev1.Namespace{Name: "runtime.antimetal.com/v1"},
		Type:      &resourcev1.TypeDescriptor{Name: "ContainerNode"},
		Name:      fmt.Sprintf("container-%s", container.ID),
	}

	// Create the spec as protobuf Any
	spec, err := anypb.New(node)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal container node: %w", err)
	}

	// Create the resource
	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{Name: "ContainerNode"},
		Metadata: &resourcev1.ResourceMeta{
			Name: fmt.Sprintf("container-%s", container.ID),
			Namespace: &resourcev1.Namespace{Name: "runtime.antimetal.com/v1"},
		},
		Spec: spec,
	}

	return rsrc, ref, nil
}

// createProcessNode creates a process node and its resource reference
func (b *Builder) createProcessNode(process *ProcessInfo) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Parse process state
	processState := parseProcessState(process.State)

	node := &runtimev1.ProcessNode{
		PID:     process.PID,
		PPID:    process.PPID,
		PGID:    process.PGID,
		SID:     process.SID,
		Command: process.Command,
		Cmdline: process.Cmdline,
		State:     processState,
		StartTime: timestamppb.New(time.Now()), // TODO: Get actual start time from ProcessInfo
	}

	// Create resource reference
	ref := &resourcev1.ResourceRef{
		Namespace: &resourcev1.Namespace{Name: "runtime.antimetal.com/v1"},
		Type:      &resourcev1.TypeDescriptor{Name: "ProcessNode"},
		Name:      fmt.Sprintf("process-%d", process.PID),
	}

	// Create the spec as protobuf Any
	spec, err := anypb.New(node)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal process node: %w", err)
	}

	// Create the resource
	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{Name: "ProcessNode"},
		Metadata: &resourcev1.ResourceMeta{
			Name:      fmt.Sprintf("process-%d", process.PID),
			Namespace: &resourcev1.Namespace{Name: "runtime.antimetal.com/v1"},
		},
		Spec: spec,
	}

	return rsrc, ref, nil
}

// createProcessRef creates just the resource reference for an existing process
func (b *Builder) createProcessRef(pid int32) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	ref := &resourcev1.ResourceRef{
		Namespace: &resourcev1.Namespace{Name: "runtime.antimetal.com/v1"},
		Type:      &resourcev1.TypeDescriptor{Name: "ProcessNode"},
		Name:      fmt.Sprintf("process-%d", pid),
	}
	return nil, ref, nil
}

// parseContainerRuntime converts runtime string to enum
func parseContainerRuntime(runtime string) runtimev1.ContainerRuntime {
	switch runtime {
	case "docker":
		return runtimev1.ContainerRuntimeDocker
	case "containerd":
		return runtimev1.ContainerRuntimeContainerd
	case "cri-containerd":
		return runtimev1.ContainerRuntimeCRIContainerd
	case "cri-o", "crio":
		return runtimev1.ContainerRuntimeCRIO
	case "podman":
		return runtimev1.ContainerRuntimePodman
	default:
		return runtimev1.ContainerRuntimeUnknown
	}
}

// parseProcessState converts state string to enum
func parseProcessState(state string) runtimev1.ProcessState {
	switch state {
	case "R":
		return runtimev1.ProcessStateRunning
	case "S":
		return runtimev1.ProcessStateSleeping
	case "D":
		return runtimev1.ProcessStateDiskSleep
	case "Z":
		return runtimev1.ProcessStateZombie
	case "T":
		return runtimev1.ProcessStateStopped
	case "t":
		return runtimev1.ProcessStateTracingStop
	case "W":
		return runtimev1.ProcessStatePaging
	case "X":
		return runtimev1.ProcessStateDead
	case "K":
		return runtimev1.ProcessStateWakeKill
	case "P":
		return runtimev1.ProcessStateParked
	default:
		return runtimev1.ProcessStateUnknown
	}
}