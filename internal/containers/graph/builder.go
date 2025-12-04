// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package containergraph

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/antimetal/agent/internal/resource"
	hardwarev1 "github.com/antimetal/agent/pkg/api/antimetal/hardware/v1"
	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/config/environment"
	"github.com/antimetal/agent/pkg/cpu"
	"github.com/antimetal/agent/pkg/host"
	"github.com/go-logr/logr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
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

	// Human-readable identifiers (container-specific only, not K8s Pod data)
	ContainerName string // Container name (not pod name)
	WorkloadName  string // Derived: workload name with hash stripped

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

func ref(rsrc *resourcev1.Resource) *resourcev1.ResourceRef {
	return &resourcev1.ResourceRef{
		TypeUrl:   rsrc.GetType().GetType(),
		Name:      rsrc.GetMetadata().GetName(),
		Namespace: rsrc.GetMetadata().GetNamespace(),
	}
}

// Builder constructs the runtime graph from runtime discovery data
type Builder struct {
	logger    logr.Logger
	store     resource.Store
	systemID  string
	hostPaths environment.HostPaths
}

// NewBuilder creates a new runtime graph builder
func NewBuilder(logger logr.Logger, store resource.Store) *Builder {
	systemID, err := host.CanonicalName()
	if err != nil {
		logger.Error(err, "Failed to get canonical name, using 'unknown'")
		systemID = "unknown"
	}

	hostPaths := environment.GetHostPaths()

	return &Builder{
		logger:    logger,
		store:     store,
		systemID:  systemID,
		hostPaths: hostPaths,
	}
}

// BuildFromSnapshot builds the runtime graph from a runtime snapshot
func (b *Builder) BuildFromSnapshot(ctx context.Context, snapshot RuntimeSnapshot) error {
	b.logger.V(1).Info("Building runtime graph from snapshot")

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

	b.logger.V(1).Info("Successfully built runtime graph",
		"containers", len(containers),
		"processes", len(processes))
	return nil
}

// buildContainerTopology builds the container nodes and relationships
func (b *Builder) buildContainerTopology(ctx context.Context, containers []ContainerInfo) error {
	for _, container := range containers {
		containerNode, err := b.createContainerNode(&container)
		if err != nil {
			return fmt.Errorf("failed to create container node: %w", err)
		}

		if err := b.store.UpdateResource(containerNode); err != nil {
			return fmt.Errorf("failed to add container node: %w", err)
		}

		containerRef := ref(containerNode)
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
	processMap := make(map[int32]*resourcev1.ResourceRef)

	for _, process := range processes {
		processNode, err := b.createProcessNode(&process, containers)
		if err != nil {
			return fmt.Errorf("failed to create process node: %w", err)
		}

		if err := b.store.UpdateResource(processNode); err != nil {
			return fmt.Errorf("failed to add process node: %w", err)
		}

		processRef := ref(processNode)
		b.logger.V(1).Info("Created process node", "pid", processRef.Name)
		processMap[process.PID] = processRef
	}

	relationships := make([]*resourcev1.Relationship, 0)

	for _, process := range processes {
		processRef, exists := processMap[process.PID]
		if !exists {
			continue
		}

		// Create parent-child relationships
		if process.PPID != 0 {
			if parentRef, exists := processMap[process.PPID]; exists {
				// Create ParentOf relationship: parent -> child
				if rels, err := b.createParentOfRelationship(parentRef, processRef); err != nil {
					b.logger.Error(err, "Failed to create parent-child relationship",
						"parent", process.PPID, "child", process.PID)
				} else {
					relationships = append(relationships, rels...)
				}
			}
		}

		// Create container-process relationships by getting container ref from process namespace
		if containerRef := b.getContainerRefFromProcessNamespace(processRef); containerRef != nil {
			if rels, err := b.createContainerProcessRelationship(containerRef, processRef); err != nil {
				b.logger.Error(err, "Failed to create container-process relationship",
					"container", containerRef.GetName(), "pid", process.PID)
			} else {
				relationships = append(relationships, rels...)
			}
		}
	}

	if len(relationships) > 0 {
		if err := b.addRelationships(relationships...); err != nil {
			return fmt.Errorf("failed to add process relationships: %w", err)
		}
	}

	return nil
}

// findProcessContainer determines which container (if any) a process belongs to by reading its cgroup
func (b *Builder) findProcessContainer(pid int32, containers []ContainerInfo) string {
	// First check if the process directory exists
	procDir := filepath.Join(b.hostPaths.Proc, strconv.Itoa(int(pid)))
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
	cpuList, err := cpu.ParseCPUList(cpusetCpus)
	if err != nil {
		return fmt.Errorf("failed to parse cpuset_cpus %s: %w", cpusetCpus, err)
	}

	relationships := make([]*resourcev1.Relationship, 0)

	// Create relationships to each CPU core
	for _, cpuID := range cpuList {
		cpuRef := &resourcev1.ResourceRef{
			TypeUrl: string(proto.MessageName(&hardwarev1.CPUCoreNode{})),
			Name:    fmt.Sprintf("%s-cpu-core-%d", b.systemID, cpuID),
		}

		if rels, err := b.createContainerHardwareRelationship(containerRef, cpuRef); err != nil {
			b.logger.Error(err, "Failed to create container-CPU relationship",
				"container", containerRef.Name, "cpu", cpuID)
		} else {
			relationships = append(relationships, rels...)
		}
	}

	if len(relationships) > 0 {
		if err := b.addRelationships(relationships...); err != nil {
			return fmt.Errorf("failed to add container-CPU relationships: %w", err)
		}
	}

	return nil
}

// createContainerNUMARelationships creates relationships between container and NUMA nodes based on cpuset
func (b *Builder) createContainerNUMARelationships(containerRef *resourcev1.ResourceRef, cpusetMems string) error {
	// Parse NUMA node list using our shared utility
	numaList, err := cpu.ParseCPUList(cpusetMems)
	if err != nil {
		return fmt.Errorf("failed to parse cpuset_mems %s: %w", cpusetMems, err)
	}

	relationships := make([]*resourcev1.Relationship, 0)

	// Create relationships to each NUMA node
	for _, numaID := range numaList {
		numaRef := &resourcev1.ResourceRef{
			TypeUrl: string(proto.MessageName(&hardwarev1.NUMANode{})),
			Name:    fmt.Sprintf("%s-numa-node-%d", b.systemID, numaID),
		}

		if rels, err := b.createContainerHardwareRelationship(containerRef, numaRef); err != nil {
			b.logger.Error(err, "Failed to create container-NUMA relationship",
				"container", containerRef.Name, "numa", numaID)
		} else {
			relationships = append(relationships, rels...)
		}
	}

	if len(relationships) > 0 {
		if err := b.addRelationships(relationships...); err != nil {
			return fmt.Errorf("failed to add container-NUMA relationships: %w", err)
		}
	}

	return nil
}

// getContainerRefFromProcessNamespace extracts container reference from process resource namespace
func (b *Builder) getContainerRefFromProcessNamespace(processRef *resourcev1.ResourceRef) *resourcev1.ResourceRef {
	if processRef.GetNamespace() == nil {
		return nil
	}

	hostNS := processRef.GetNamespace().GetHost()
	if hostNS == nil || hostNS.GetContainer() == "" {
		return nil
	}

	return &resourcev1.ResourceRef{
		TypeUrl: typeContainer,
		Name:    hostNS.GetContainer(),
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Host{
				Host: &resourcev1.HostNamespace{
					Host: b.systemID,
				},
			},
		},
	}
}

// addRelationships adds multiple relationships with deduplication
func (b *Builder) addRelationships(rels ...*resourcev1.Relationship) error {
	relsToAdd := make([]*resourcev1.Relationship, 0)
	for _, rel := range rels {
		msgType, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(rel.Type.Type))
		if err != nil {
			return fmt.Errorf("failed to find message type %q in registry: %w", rel.Type.Type, err)
		}
		pred := msgType.New().Interface()

		_, err = b.store.GetRelationships(rel.GetSubject(), rel.GetObject(), pred)
		if err != nil {
			if !errors.Is(err, resource.ErrRelationshipsNotFound) {
				return fmt.Errorf("failed to find existing relationships: %w", err)
			}
			relsToAdd = append(relsToAdd, rel)
		}
	}
	if len(relsToAdd) > 0 {
		if err := b.store.AddRelationships(relsToAdd...); err != nil {
			return fmt.Errorf("failed to add relationship: %w", err)
		}
	}
	return nil
}
