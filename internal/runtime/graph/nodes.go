// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	"fmt"
	"time"

	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// createContainerNode creates a container node and its resource reference
func (b *Builder) createContainerNode(container *ContainerInfo) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Runtime is already an enum, no conversion needed
	runtime := container.Runtime

	// Convert cgroup version
	cgroupVersion := parseCgroupVersion(container.CgroupVersion)

	// Create container data as a struct for JSON marshaling
	containerData := map[string]interface{}{
		"container_id":   container.ID,
		"runtime":        runtime.String(),
		"cgroup_version": cgroupVersion.String(),
		"cgroup_path":    container.CgroupPath,
		"image_name":     container.ImageName,
		"image_tag":      container.ImageTag,
	}

	// Handle labels safely - only add if not empty
	if len(container.Labels) > 0 {
		containerData["labels"] = container.Labels
	}

	// Add resource limits if available
	if container.CPUShares != nil {
		containerData["cpu_shares"] = *container.CPUShares
	}
	if container.CPUQuotaUs != nil {
		containerData["cpu_quota_us"] = *container.CPUQuotaUs
	}
	if container.CPUPeriodUs != nil {
		containerData["cpu_period_us"] = *container.CPUPeriodUs
	}
	if container.MemoryLimitBytes != nil {
		containerData["memory_limit_bytes"] = *container.MemoryLimitBytes
	}
	if container.CpusetCpus != "" {
		containerData["cpuset_cpus"] = container.CpusetCpus
	}
	if container.CpusetMems != "" {
		containerData["cpuset_mems"] = container.CpusetMems
	}

	// Convert to protobuf Struct
	spec, err := structpb.NewStruct(containerData)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal container data: %w", err)
	}

	specAny, err := anypb.New(spec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to wrap container spec: %w", err)
	}

	containerName := fmt.Sprintf("container-%s", container.ID)

	// Create the resource
	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: "ContainerNode",
			Type: "antimetal.runtime.v1.ContainerNode",
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider: resourcev1.Provider_PROVIDER_KUBERNETES,
			Service:  "runtime",
			Name:     containerName,
			Namespace: &resourcev1.Namespace{
				Namespace: &resourcev1.Namespace_Kube{
					Kube: &resourcev1.KubernetesNamespace{
						Cluster:   "runtime", // Using "runtime" as a pseudo-cluster
						Namespace: "antimetal-system",
					},
				},
			},
		},
		Spec: specAny,
	}

	// Create resource reference
	ref := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.runtime.v1/ContainerNode",
		Name:    containerName,
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Kube{
				Kube: &resourcev1.KubernetesNamespace{
					Cluster:   "runtime", // Using "runtime" as a pseudo-cluster
					Namespace: "antimetal-system",
				},
			},
		},
	}

	return rsrc, ref, nil
}

// createProcessNode creates a process node and its resource reference
func (b *Builder) createProcessNode(process *ProcessInfo) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Parse process state
	processState := parseProcessState(process.State)

	// Create process data as a struct for JSON marshaling
	processData := map[string]interface{}{
		"pid":     process.PID,
		"ppid":    process.PPID,
		"pgid":    process.PGID,
		"sid":     process.SID,
		"command": process.Command,
		"state":   processState.String(),
	}

	if process.Cmdline != "" {
		processData["cmdline"] = process.Cmdline
	}

	// Add current timestamp as start time (TODO: get actual start time)
	processData["start_time"] = time.Now().Format(time.RFC3339)

	// Convert to protobuf Struct
	spec, err := structpb.NewStruct(processData)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal process data: %w", err)
	}

	specAny, err := anypb.New(spec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to wrap process spec: %w", err)
	}

	processName := fmt.Sprintf("process-%d", process.PID)

	// Create the resource
	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: "ProcessNode",
			Type: "antimetal.runtime.v1.ProcessNode",
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider: resourcev1.Provider_PROVIDER_KUBERNETES,
			Service:  "runtime",
			Name:     processName,
			Namespace: &resourcev1.Namespace{
				Namespace: &resourcev1.Namespace_Kube{
					Kube: &resourcev1.KubernetesNamespace{
						Cluster:   "runtime", // Using "runtime" as a pseudo-cluster
						Namespace: "antimetal-system",
					},
				},
			},
		},
		Spec: specAny,
	}

	// Create resource reference
	ref := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.runtime.v1/ProcessNode",
		Name:    processName,
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Kube{
				Kube: &resourcev1.KubernetesNamespace{
					Cluster:   "runtime", // Using "runtime" as a pseudo-cluster
					Namespace: "antimetal-system",
				},
			},
		},
	}

	return rsrc, ref, nil
}

// createProcessRef creates just the resource reference for an existing process
func (b *Builder) createProcessRef(pid int32) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	processName := fmt.Sprintf("process-%d", pid)
	ref := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.runtime.v1/ProcessNode",
		Name:    processName,
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Kube{
				Kube: &resourcev1.KubernetesNamespace{
					Cluster:   "runtime", // Using "runtime" as a pseudo-cluster
					Namespace: "antimetal-system",
				},
			},
		},
	}
	return nil, ref, nil
}

// createContainerRef creates just the resource reference for an existing container
func (b *Builder) createContainerRef(containerID string) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	containerName := fmt.Sprintf("container-%s", containerID)
	ref := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.runtime.v1/ContainerNode",
		Name:    containerName,
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Kube{
				Kube: &resourcev1.KubernetesNamespace{
					Cluster:   "runtime", // Using "runtime" as a pseudo-cluster
					Namespace: "antimetal-system",
				},
			},
		},
	}
	return nil, ref, nil
}

// TODO: parseContainerRuntime will be used when container metadata extraction is implemented
// parseContainerRuntime converts runtime string to enum
/*
func parseContainerRuntime(runtime string) runtimev1.ContainerRuntime {
	switch runtime {
	case "docker":
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER
	case "containerd":
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CONTAINERD
	case "cri-containerd":
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CRI_CONTAINERD
	case "cri-o", "crio":
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CRI_O
	case "podman":
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_PODMAN
	default:
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_UNKNOWN
	}
}
*/

// parseCgroupVersion converts cgroup version int to enum
func parseCgroupVersion(version int) runtimev1.CgroupVersion {
	switch version {
	case 1:
		return runtimev1.CgroupVersion_CGROUP_VERSION_V1
	case 2:
		return runtimev1.CgroupVersion_CGROUP_VERSION_V2
	default:
		return runtimev1.CgroupVersion_CGROUP_VERSION_V1 // Default to V1 as most common
	}
}

// parseProcessState converts state string to enum
func parseProcessState(state string) runtimev1.ProcessState {
	switch state {
	case "R":
		return runtimev1.ProcessState_PROCESS_STATE_RUNNING
	case "S":
		return runtimev1.ProcessState_PROCESS_STATE_SLEEPING
	case "D":
		return runtimev1.ProcessState_PROCESS_STATE_DISK_SLEEP
	case "Z":
		return runtimev1.ProcessState_PROCESS_STATE_ZOMBIE
	case "T":
		return runtimev1.ProcessState_PROCESS_STATE_STOPPED
	case "t":
		return runtimev1.ProcessState_PROCESS_STATE_TRACING_STOP
	case "W":
		return runtimev1.ProcessState_PROCESS_STATE_PAGING
	case "X":
		return runtimev1.ProcessState_PROCESS_STATE_DEAD
	case "K":
		return runtimev1.ProcessState_PROCESS_STATE_WAKEKILL
	case "P":
		return runtimev1.ProcessState_PROCESS_STATE_PARKED
	default:
		return runtimev1.ProcessState_PROCESS_STATE_UNKNOWN
	}
}
