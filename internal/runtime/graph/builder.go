// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	"context"
	"fmt"

	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	"github.com/antimetal/agent/pkg/resource"
	"github.com/go-logr/logr"
)

// RuntimeSnapshot contains all runtime information at a specific point in time
// This is imported from the runtime package to avoid circular dependencies
type RuntimeSnapshot interface {
	GetContainers() []ContainerInfo
	GetProcesses() []ProcessInfo
}

// ContainerInfo represents container information for graph building
type ContainerInfo struct {
	ID            string
	Runtime       string
	CgroupVersion int
	CgroupPath    string
	ImageName     string
	ImageTag      string
	Labels        map[string]string
	// Resource limits
	CPUShares        *int32
	CPUQuotaUs       *int32
	CPUPeriodUs      *int32
	MemoryLimitBytes *uint64
	CpusetCpus       string
	CpusetMems       string
}

// ProcessInfo represents process information for graph building
type ProcessInfo struct {
	PID     int32
	PPID    int32
	PGID    int32
	SID     int32
	Command string
	Cmdline string
	State   string
}

// Builder constructs the runtime graph from runtime discovery data
type Builder struct {
	logger logr.Logger
	store  resource.Store
}

// NewBuilder creates a new runtime graph builder
func NewBuilder(logger logr.Logger, store resource.Store) *Builder {
	return &Builder{
		logger: logger,
		store:  store,
	}
}

// BuildFromSnapshot builds the runtime graph from a runtime snapshot
func (b *Builder) BuildFromSnapshot(ctx context.Context, snapshot RuntimeSnapshot) error {
	b.logger.Info("Building runtime graph from snapshot")

	// Build container topology
	containers := snapshot.GetContainers()
	if len(containers) > 0 {
		if err := b.buildContainerTopology(ctx, containers); err != nil {
			return fmt.Errorf("failed to build container topology: %w", err)
		}
	}

	// Build process topology
	processes := snapshot.GetProcesses()
	if len(processes) > 0 {
		if err := b.buildProcessTopology(ctx, processes); err != nil {
			return fmt.Errorf("failed to build process topology: %w", err)
		}
	}

	b.logger.Info("Successfully built runtime graph",
		"containers", len(containers),
		"processes", len(processes))
	return nil
}

// buildContainerTopology builds the container nodes and relationships
func (b *Builder) buildContainerTopology(ctx context.Context, containers []ContainerInfo) error {
	for _, container := range containers {
		containerNode, containerRef, err := b.createContainerNode(&container)
		if err != nil {
			return fmt.Errorf("failed to create container node: %w", err)
		}

		if err := b.store.AddResource(containerNode); err != nil {
			return fmt.Errorf("failed to add container node: %w", err)
		}

		b.logger.V(1).Info("Created container node", "id", containerRef.Name)

		// TODO: Create relationships to hardware nodes based on cpuset_cpus and cpuset_mems
		// This would involve:
		// 1. Parsing cpuset_cpus to get CPU core assignments
		// 2. Creating RunsOn relationships to CPU cores
		// 3. Parsing cpuset_mems to get NUMA node assignments
		// 4. Creating AllocatedTo relationships to memory modules
	}

	return nil
}

// buildProcessTopology builds the process nodes and relationships
func (b *Builder) buildProcessTopology(ctx context.Context, processes []ProcessInfo) error {
	// Build a map of PID to ProcessInfo for relationship building
	processMap := make(map[int32]*ProcessInfo)
	for i, process := range processes {
		processMap[process.PID] = &processes[i]
	}

	for _, process := range processes {
		processNode, processRef, err := b.createProcessNode(&process)
		if err != nil {
			return fmt.Errorf("failed to create process node: %w", err)
		}

		if err := b.store.AddResource(processNode); err != nil {
			return fmt.Errorf("failed to add process node: %w", err)
		}

		b.logger.V(1).Info("Created process node", "pid", processRef.Name)

		// Create parent-child relationships
		if process.PPID != 0 {
			if parentProcess, exists := processMap[process.PPID]; exists {
				_, parentRef, err := b.createProcessRef(parentProcess.PID)
				if err == nil {
					// Create ParentOf relationship: parent -> child
					if err := b.createParentOfRelationship(parentRef, processRef); err != nil {
						b.logger.Error(err, "Failed to create parent-child relationship",
							"parent", process.PPID, "child", process.PID)
					}
				}
			}
		}

		// TODO: Create relationships to containers
		// This would involve:
		// 1. Determining which container (if any) this process belongs to
		// 2. Creating Contains relationships from container to process
		// 3. This could be done by checking cgroup membership
	}

	return nil
}