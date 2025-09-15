// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	"fmt"

	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"google.golang.org/protobuf/types/known/anypb"
)

// Define Kind and Type constants using proto descriptor full names
var (
	kindResource  = string((&resourcev1.Resource{}).ProtoReflect().Descriptor().FullName())
	typeContainer = string((&runtimev1.ContainerNode{}).ProtoReflect().Descriptor().FullName())
	typeProcess   = string((&runtimev1.ProcessNode{}).ProtoReflect().Descriptor().FullName())
)

// Constants for resource metadata
const (
	// RuntimePseudoCluster is used as the cluster name for runtime-discovered resources
	RuntimePseudoCluster = "runtime"

	// SystemNamespace is the default namespace for runtime resources
	SystemNamespace = "antimetal-system"

	// RuntimeService identifies the service that created these resources
	RuntimeService = "runtime"
)

// createContainerNode creates a container node and its resource reference
func (b *Builder) createContainerNode(container *ContainerInfo) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Convert cgroup version
	cgroupVersion := parseCgroupVersion(container.CgroupVersion)

	// Create concrete proto type
	containerNode := &runtimev1.ContainerNode{
		ContainerId:   container.ID,
		Runtime:       container.Runtime,
		CgroupVersion: cgroupVersion,
		CgroupPath:    container.CgroupPath,
		ImageName:     container.ImageName,
		ImageTag:      container.ImageTag,
		Labels:        container.Labels,
		// Timestamps would be set if we had them
		// CreatedAt: container.CreatedAt,
		// StartedAt: container.StartedAt,
		CpuShares:        container.CPUShares,
		CpuQuotaUs:       container.CPUQuotaUs,
		CpuPeriodUs:      container.CPUPeriodUs,
		MemoryLimitBytes: container.MemoryLimitBytes,
		CpusetCpus:       container.CpusetCpus,
		CpusetMems:       container.CpusetMems,
	}

	// Wrap in Any for the Resource spec
	specAny, err := anypb.New(containerNode)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to wrap container spec: %w", err)
	}

	containerName := fmt.Sprintf("container-%s", container.ID)

	// Create the resource with host namespace
	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: typeContainer,
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider: resourcev1.Provider_PROVIDER_ANTIMETAL,
			Name:     containerName,
			Namespace: &resourcev1.Namespace{
				Namespace: &resourcev1.Namespace_Kube{
					Kube: &resourcev1.KubernetesNamespace{
						Cluster:   b.machineID, // Use machine ID as cluster for host-scoped resources
						Namespace: "host",      // Fixed namespace for host-level resources
					},
				},
			},
		},
		Spec: specAny,
	}

	// Create resource reference with namespace
	ref := &resourcev1.ResourceRef{
		TypeUrl: typeContainer,
		Name:    containerName,
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Kube{
				Kube: &resourcev1.KubernetesNamespace{
					Cluster:   b.machineID, // Use machine ID as cluster for host-scoped resources
					Namespace: "host",      // Fixed namespace for host-level resources
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

	// Create concrete proto type
	processNode := &runtimev1.ProcessNode{
		Pid:     process.PID,
		Ppid:    process.PPID,
		Pgid:    process.PGID,
		Sid:     process.SID,
		Command: process.Command,
		Cmdline: process.Cmdline,
		State:   processState,
		// StartTime would be set if we had it
		// StartTime: timestamppb.New(process.StartTime),
	}

	// Wrap in Any for the Resource spec
	specAny, err := anypb.New(processNode)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to wrap process spec: %w", err)
	}

	processName := fmt.Sprintf("process-%d", process.PID)

	// Create the resource with host namespace
	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: typeProcess,
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider: resourcev1.Provider_PROVIDER_ANTIMETAL,
			Name:     processName,
			Namespace: &resourcev1.Namespace{
				Namespace: &resourcev1.Namespace_Kube{
					Kube: &resourcev1.KubernetesNamespace{
						Cluster:   b.machineID, // Use machine ID as cluster for host-scoped resources
						Namespace: "host",      // Fixed namespace for host-level resources
					},
				},
			},
		},
		Spec: specAny,
	}

	// Create resource reference with namespace
	ref := &resourcev1.ResourceRef{
		TypeUrl: typeProcess,
		Name:    processName,
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Kube{
				Kube: &resourcev1.KubernetesNamespace{
					Cluster:   b.machineID, // Use machine ID as cluster for host-scoped resources
					Namespace: "host",      // Fixed namespace for host-level resources
				},
			},
		},
	}

	return rsrc, ref, nil
}

// createProcessRef creates just the resource reference for an existing process
func (b *Builder) createProcessRef(pid int32) *resourcev1.ResourceRef {
	processName := fmt.Sprintf("process-%d", pid)
	return &resourcev1.ResourceRef{
		TypeUrl: typeProcess,
		Name:    processName,
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Kube{
				Kube: &resourcev1.KubernetesNamespace{
					Cluster:   b.machineID, // Use machine ID as cluster for host-scoped resources
					Namespace: "host",      // Fixed namespace for host-level resources
				},
			},
		},
	}
}

// createContainerRef creates just the resource reference for an existing container
func (b *Builder) createContainerRef(containerID string) *resourcev1.ResourceRef {
	containerName := fmt.Sprintf("container-%s", containerID)
	return &resourcev1.ResourceRef{
		TypeUrl: typeContainer,
		Name:    containerName,
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Kube{
				Kube: &resourcev1.KubernetesNamespace{
					Cluster:   b.machineID, // Use machine ID as cluster for host-scoped resources
					Namespace: "host",      // Fixed namespace for host-level resources
				},
			},
		},
	}
}

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
