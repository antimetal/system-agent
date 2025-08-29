// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/performance"
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
	Runtime       runtimev1.ContainerRuntime
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
		if err := b.buildProcessTopology(ctx, processes, containers); err != nil {
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

		// Create container-hardware relationships based on cpuset constraints
		if container.CpusetCpus != "" {
			if err := b.createContainerCPURelationships(containerRef, container.CpusetCpus); err != nil {
				b.logger.Error(err, "Failed to create container-CPU relationships",
					"container", container.ID, "cpuset_cpus", container.CpusetCpus)
			}
		}

		if container.CpusetMems != "" {
			if err := b.createContainerNUMARelationships(containerRef, container.CpusetMems); err != nil {
				b.logger.Error(err, "Failed to create container-NUMA relationships",
					"container", container.ID, "cpuset_mems", container.CpusetMems)
			}
		}
	}

	return nil
}

// buildProcessTopology builds the process nodes and relationships
func (b *Builder) buildProcessTopology(ctx context.Context, processes []ProcessInfo, containers []ContainerInfo) error {
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

		// Create container-process relationships by analyzing cgroup membership
		if containerID := b.findProcessContainer(process.PID, containers); containerID != "" {
			_, containerRef, err := b.createContainerRef(containerID)
			if err == nil {
				if err := b.createContainerProcessRelationship(containerRef, processRef); err != nil {
					b.logger.Error(err, "Failed to create container-process relationship",
						"container", containerID, "pid", process.PID)
				}
			}
		}
	}

	return nil
}

// findProcessContainer determines which container (if any) a process belongs to by reading its cgroup
func (b *Builder) findProcessContainer(pid int32, containers []ContainerInfo) string {
	// First check if the process directory exists
	procDir := filepath.Join("/proc", strconv.Itoa(int(pid)))
	if _, err := os.Stat(procDir); os.IsNotExist(err) {
		// Process no longer exists, this is normal during container churn
		b.logger.V(2).Info("Process directory does not exist", "pid", pid)
		return ""
	} else if err != nil {
		// Unexpected error accessing proc directory
		b.logger.Error(err, "Failed to stat process directory", "pid", pid, "path", procDir)
		return ""
	}

	cgroupPath := filepath.Join(procDir, "cgroup")
	cgroupData, err := os.ReadFile(cgroupPath)
	if err != nil {
		// Distinguish between different error types for better debugging
		if os.IsNotExist(err) {
			// Process may have exited between stat and read
			b.logger.V(2).Info("Process cgroup file no longer exists", "pid", pid, "path", cgroupPath)
		} else if os.IsPermission(err) {
			// Permission issues should be logged at higher level as they indicate configuration problems
			b.logger.V(1).Info("Permission denied reading process cgroup", "pid", pid, "path", cgroupPath)
		} else {
			// Unexpected errors should be logged with full context
			b.logger.Error(err, "Failed to read process cgroup file", "pid", pid, "path", cgroupPath)
		}
		return ""
	}

	cgroupContent := string(cgroupData)

	// Check each container to see if this process matches
	for _, container := range containers {
		if b.processMatchesContainer(cgroupContent, container) {
			return container.ID
		}
	}

	return ""
}

// processMatchesContainer checks if a process's cgroup content indicates it belongs to the container
func (b *Builder) processMatchesContainer(cgroupContent string, container ContainerInfo) bool {
	// Parse cgroup lines looking for container patterns
	lines := strings.Split(strings.TrimSpace(cgroupContent), "\n")

	for _, line := range lines {
		// cgroup format: hierarchy-id:controller-list:cgroup-path
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}

		cgroupPath := parts[2]

		// Check for direct container ID match in cgroup path
		if container.ID != "" && strings.Contains(cgroupPath, container.ID) {
			return true
		}

		// Check for container cgroup path match (for more complex hierarchies)
		if container.CgroupPath != "" && strings.Contains(cgroupPath, container.CgroupPath) {
			return true
		}

		// Check for runtime-specific patterns
		switch container.Runtime {
		case runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER:
			if strings.Contains(cgroupPath, "docker") && strings.Contains(cgroupPath, container.ID) {
				return true
			}
		case runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CONTAINERD, runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CRI_CONTAINERD:
			if strings.Contains(cgroupPath, "containerd") && strings.Contains(cgroupPath, container.ID) {
				return true
			}
		case runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CRI_O:
			if strings.Contains(cgroupPath, "crio") && strings.Contains(cgroupPath, container.ID) {
				return true
			}
		}
	}

	return false
}

// createContainerCPURelationships creates relationships between container and CPU cores based on cpuset
func (b *Builder) createContainerCPURelationships(containerRef *resourcev1.ResourceRef, cpusetCpus string) error {
	// Parse CPU list using our shared utility
	cpuList, err := performance.ParseCPUList(cpusetCpus)
	if err != nil {
		return fmt.Errorf("failed to parse cpuset_cpus %s: %w", cpusetCpus, err)
	}

	// Create relationships to each CPU core
	for _, cpuID := range cpuList {
		cpuRef := &resourcev1.ResourceRef{
			TypeUrl: "hardware.antimetal.com/v1/CPU",
			Name:    fmt.Sprintf("cpu-%d", cpuID),
		}

		if err := b.createContainerHardwareRelationship(containerRef, cpuRef); err != nil {
			b.logger.Error(err, "Failed to create container-CPU relationship",
				"container", containerRef.Name, "cpu", cpuID)
		}
	}

	return nil
}

// createContainerNUMARelationships creates relationships between container and NUMA nodes based on cpuset
func (b *Builder) createContainerNUMARelationships(containerRef *resourcev1.ResourceRef, cpusetMems string) error {
	// Parse NUMA node list using our shared utility
	numaList, err := performance.ParseCPUList(cpusetMems)
	if err != nil {
		return fmt.Errorf("failed to parse cpuset_mems %s: %w", cpusetMems, err)
	}

	// Create relationships to each NUMA node
	for _, numaID := range numaList {
		numaRef := &resourcev1.ResourceRef{
			TypeUrl: "hardware.antimetal.com/v1/NUMANode",
			Name:    fmt.Sprintf("numa-node-%d", numaID),
		}

		if err := b.createContainerHardwareRelationship(containerRef, numaRef); err != nil {
			b.logger.Error(err, "Failed to create container-NUMA relationship",
				"container", containerRef.Name, "numa", numaID)
		}
	}

	return nil
}
