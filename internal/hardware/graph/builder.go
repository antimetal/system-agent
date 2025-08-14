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
	"time"

	hardwarev1 "github.com/antimetal/agent/pkg/api/hardware/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/resource"
	"github.com/go-logr/logr"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Builder constructs the hardware graph from performance collector data
type Builder struct {
	logger logr.Logger
	store  resource.Store
}

// NewBuilder creates a new hardware graph builder
func NewBuilder(logger logr.Logger, store resource.Store) *Builder {
	return &Builder{
		logger: logger,
		store:  store,
	}
}

// BuildFromSnapshot builds the hardware graph from a performance snapshot
func (b *Builder) BuildFromSnapshot(ctx context.Context, snapshot *performance.Snapshot) error {
	b.logger.Info("Building hardware graph from snapshot")

	// Create the root system node
	systemNode, systemRef, err := b.createSystemNode()
	if err != nil {
		return fmt.Errorf("failed to create system node: %w", err)
	}

	// Store the system node
	if err := b.store.AddResource(systemNode); err != nil {
		return fmt.Errorf("failed to add system node: %w", err)
	}

	// Build CPU topology
	if snapshot.Metrics.CPUInfo != nil {
		if err := b.buildCPUTopology(ctx, snapshot.Metrics.CPUInfo, systemRef); err != nil {
			return fmt.Errorf("failed to build CPU topology: %w", err)
		}
	}

	// Build memory topology
	if snapshot.Metrics.MemoryInfo != nil {
		if err := b.buildMemoryTopology(ctx, snapshot.Metrics.MemoryInfo, systemRef); err != nil {
			return fmt.Errorf("failed to build memory topology: %w", err)
		}
	}

	// Build disk topology
	if len(snapshot.Metrics.DiskInfo) > 0 {
		if err := b.buildDiskTopology(ctx, snapshot.Metrics.DiskInfo, systemRef); err != nil {
			return fmt.Errorf("failed to build disk topology: %w", err)
		}
	}

	// Build network topology
	if len(snapshot.Metrics.NetworkInfo) > 0 {
		if err := b.buildNetworkTopology(ctx, snapshot.Metrics.NetworkInfo, systemRef); err != nil {
			return fmt.Errorf("failed to build network topology: %w", err)
		}
	}

	b.logger.Info("Hardware graph build complete")
	return nil
}

// createSystemNode creates the root system node
func (b *Builder) createSystemNode() (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	hostname, _ := os.Hostname()

	systemSpec := &hardwarev1.SystemNode{
		Hostname:      hostname,
		Architecture:  "x86_64",                    // TODO: Get from runtime
		BootTime:      timestamppb.New(time.Now()), // TODO: Get actual boot time
		KernelVersion: "",                          // TODO: Get from uname
		OsInfo:        "",                          // TODO: Get OS info
	}

	specAny, err := anypb.New(systemSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal system spec: %w", err)
	}

	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: "SystemNode",
			Type: string(systemSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_HARDWARE,
			ProviderId: hostname,
			Name:       hostname,
		},
		Spec: specAny,
	}

	ref := &resourcev1.ResourceRef{
		TypeUrl: string(systemSpec.ProtoReflect().Descriptor().FullName()),
		Name:    hostname,
	}

	return resource, ref, nil
}

// buildCPUTopology builds the CPU nodes and relationships
func (b *Builder) buildCPUTopology(ctx context.Context, cpuInfo *performance.CPUInfo, systemRef *resourcev1.ResourceRef) error {
	// Track unique physical packages
	packages := make(map[int32]*resourcev1.ResourceRef)

	// Create CPU package nodes
	for _, core := range cpuInfo.Cores {
		if _, exists := packages[core.PhysicalID]; !exists {
			packageNode, packageRef, err := b.createCPUPackageNode(cpuInfo, core.PhysicalID)
			if err != nil {
				return fmt.Errorf("failed to create CPU package %d: %w", core.PhysicalID, err)
			}

			// Store the package node
			if err := b.store.AddResource(packageNode); err != nil {
				return fmt.Errorf("failed to add CPU package: %w", err)
			}

			packages[core.PhysicalID] = packageRef

			// Create containment relationship: System -> CPU Package
			if err := b.createContainsRelationship(systemRef, packageRef, "physical"); err != nil {
				return fmt.Errorf("failed to create system->package relationship: %w", err)
			}
		}
	}

	// Create CPU core nodes
	for _, core := range cpuInfo.Cores {
		coreNode, coreRef, err := b.createCPUCoreNode(&core)
		if err != nil {
			return fmt.Errorf("failed to create CPU core %d: %w", core.Processor, err)
		}

		// Store the core node
		if err := b.store.AddResource(coreNode); err != nil {
			return fmt.Errorf("failed to add CPU core: %w", err)
		}

		// Create containment relationship: CPU Package -> CPU Core
		if packageRef, exists := packages[core.PhysicalID]; exists {
			if err := b.createContainsRelationship(packageRef, coreRef, "physical"); err != nil {
				return fmt.Errorf("failed to create package->core relationship: %w", err)
			}
		}
	}

	return nil
}

// createCPUPackageNode creates a CPU package node
func (b *Builder) createCPUPackageNode(cpuInfo *performance.CPUInfo, physicalID int32) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	packageSpec := &hardwarev1.CPUPackageNode{
		SocketId:      physicalID,
		VendorId:      cpuInfo.VendorID,
		ModelName:     cpuInfo.ModelName,
		CpuFamily:     cpuInfo.CPUFamily,
		Model:         cpuInfo.Model,
		Stepping:      cpuInfo.Stepping,
		Microcode:     cpuInfo.Microcode,
		CacheSize:     cpuInfo.CacheSize,
		PhysicalCores: cpuInfo.PhysicalCores,
		LogicalCores:  cpuInfo.LogicalCores,
	}

	specAny, err := anypb.New(packageSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal package spec: %w", err)
	}

	name := fmt.Sprintf("cpu-package-%d", physicalID)
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: "CPUPackageNode",
			Type: string(packageSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_HARDWARE,
			ProviderId: name,
			Name:       name,
		},
		Spec: specAny,
	}

	ref := &resourcev1.ResourceRef{
		TypeUrl: string(packageSpec.ProtoReflect().Descriptor().FullName()),
		Name:    name,
	}

	return resource, ref, nil
}

// createCPUCoreNode creates a CPU core node
func (b *Builder) createCPUCoreNode(core *performance.CPUCore) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	coreSpec := &hardwarev1.CPUCoreNode{
		ProcessorId:  core.Processor,
		CoreId:       core.CoreID,
		PhysicalId:   core.PhysicalID,
		FrequencyMhz: core.CPUMHz,
		Siblings:     core.Siblings,
	}

	specAny, err := anypb.New(coreSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal core spec: %w", err)
	}

	name := fmt.Sprintf("cpu-core-%d", core.Processor)
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: "CPUCoreNode",
			Type: string(coreSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_HARDWARE,
			ProviderId: name,
			Name:       name,
		},
		Spec: specAny,
	}

	ref := &resourcev1.ResourceRef{
		TypeUrl: string(coreSpec.ProtoReflect().Descriptor().FullName()),
		Name:    name,
	}

	return resource, ref, nil
}

// buildMemoryTopology builds the memory nodes and relationships
func (b *Builder) buildMemoryTopology(ctx context.Context, memInfo *performance.MemoryInfo, systemRef *resourcev1.ResourceRef) error {
	// Create memory module node
	memNode, memRef, err := b.createMemoryModuleNode(memInfo)
	if err != nil {
		return fmt.Errorf("failed to create memory module: %w", err)
	}

	// Store the memory node
	if err := b.store.AddResource(memNode); err != nil {
		return fmt.Errorf("failed to add memory module: %w", err)
	}

	// Create containment relationship: System -> Memory Module
	if err := b.createContainsRelationship(systemRef, memRef, "physical"); err != nil {
		return fmt.Errorf("failed to create system->memory relationship: %w", err)
	}

	// Create NUMA nodes if enabled
	if memInfo.NUMAEnabled {
		for _, numaNode := range memInfo.NUMANodes {
			numa, numaRef, err := b.createNUMANode(&numaNode)
			if err != nil {
				return fmt.Errorf("failed to create NUMA node %d: %w", numaNode.NodeID, err)
			}

			// Store the NUMA node
			if err := b.store.AddResource(numa); err != nil {
				return fmt.Errorf("failed to add NUMA node: %w", err)
			}

			// Create containment relationship: System -> NUMA Node
			if err := b.createContainsRelationship(systemRef, numaRef, "logical"); err != nil {
				return fmt.Errorf("failed to create system->numa relationship: %w", err)
			}

			// Create NUMA affinity relationship: Memory Module -> NUMA Node
			if err := b.createNUMAAffinityRelationship(memRef, numaRef, numaNode.NodeID); err != nil {
				return fmt.Errorf("failed to create memory->numa affinity: %w", err)
			}
		}
	}

	return nil
}

// createMemoryModuleNode creates a memory module node
func (b *Builder) createMemoryModuleNode(memInfo *performance.MemoryInfo) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	memSpec := &hardwarev1.MemoryModuleNode{
		TotalBytes:             memInfo.TotalBytes,
		NumaEnabled:            memInfo.NUMAEnabled,
		NumaBalancingAvailable: memInfo.NUMABalancingAvailable,
		NumaNodeCount:          int32(len(memInfo.NUMANodes)),
	}

	specAny, err := anypb.New(memSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal memory spec: %w", err)
	}

	name := "memory-module"
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: "MemoryModuleNode",
			Type: string(memSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_HARDWARE,
			ProviderId: name,
			Name:       name,
		},
		Spec: specAny,
	}

	ref := &resourcev1.ResourceRef{
		TypeUrl: string(memSpec.ProtoReflect().Descriptor().FullName()),
		Name:    name,
	}

	return resource, ref, nil
}

// createNUMANode creates a NUMA node
func (b *Builder) createNUMANode(numa *performance.NUMANode) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	numaSpec := &hardwarev1.NUMANode{
		NodeId:     numa.NodeID,
		TotalBytes: numa.TotalBytes,
		Cpus:       numa.CPUs,
		Distances:  numa.Distances,
	}

	specAny, err := anypb.New(numaSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal NUMA spec: %w", err)
	}

	name := fmt.Sprintf("numa-node-%d", numa.NodeID)
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: "NUMANode",
			Type: string(numaSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_HARDWARE,
			ProviderId: name,
			Name:       name,
		},
		Spec: specAny,
	}

	ref := &resourcev1.ResourceRef{
		TypeUrl: string(numaSpec.ProtoReflect().Descriptor().FullName()),
		Name:    name,
	}

	return resource, ref, nil
}

// buildDiskTopology builds the disk nodes and relationships
func (b *Builder) buildDiskTopology(ctx context.Context, diskInfo []performance.DiskInfo, systemRef *resourcev1.ResourceRef) error {
	for _, disk := range diskInfo {
		// Create disk device node
		diskNode, diskRef, err := b.createDiskDeviceNode(&disk)
		if err != nil {
			return fmt.Errorf("failed to create disk %s: %w", disk.Device, err)
		}

		// Store the disk node
		if err := b.store.AddResource(diskNode); err != nil {
			return fmt.Errorf("failed to add disk device: %w", err)
		}

		// Create containment relationship: System -> Disk Device
		if err := b.createContainsRelationship(systemRef, diskRef, "physical"); err != nil {
			return fmt.Errorf("failed to create system->disk relationship: %w", err)
		}

		// Create partition nodes
		for _, partition := range disk.Partitions {
			partNode, partRef, err := b.createDiskPartitionNode(&partition, disk.Device)
			if err != nil {
				return fmt.Errorf("failed to create partition %s: %w", partition.Name, err)
			}

			// Store the partition node
			if err := b.store.AddResource(partNode); err != nil {
				return fmt.Errorf("failed to add partition: %w", err)
			}

			// Create containment relationship: Disk Device -> Partition
			if err := b.createContainsRelationship(diskRef, partRef, "partition"); err != nil {
				return fmt.Errorf("failed to create disk->partition relationship: %w", err)
			}
		}
	}

	return nil
}

// createDiskDeviceNode creates a disk device node
func (b *Builder) createDiskDeviceNode(disk *performance.DiskInfo) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	diskSpec := &hardwarev1.DiskDeviceNode{
		Device:            disk.Device,
		Model:             disk.Model,
		Vendor:            disk.Vendor,
		SizeBytes:         disk.SizeBytes,
		Rotational:        disk.Rotational,
		BlockSize:         disk.BlockSize,
		PhysicalBlockSize: disk.PhysicalBlockSize,
		Scheduler:         disk.Scheduler,
		QueueDepth:        disk.QueueDepth,
	}

	specAny, err := anypb.New(diskSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal disk spec: %w", err)
	}

	name := fmt.Sprintf("disk-%s", disk.Device)
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: "DiskDeviceNode",
			Type: string(diskSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_HARDWARE,
			ProviderId: name,
			Name:       name,
		},
		Spec: specAny,
	}

	ref := &resourcev1.ResourceRef{
		TypeUrl: string(diskSpec.ProtoReflect().Descriptor().FullName()),
		Name:    name,
	}

	return resource, ref, nil
}

// createDiskPartitionNode creates a disk partition node
func (b *Builder) createDiskPartitionNode(partition *performance.PartitionInfo, parentDevice string) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	partSpec := &hardwarev1.DiskPartitionNode{
		Name:         partition.Name,
		ParentDevice: parentDevice,
		SizeBytes:    partition.SizeBytes,
		StartSector:  partition.StartSector,
	}

	specAny, err := anypb.New(partSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal partition spec: %w", err)
	}

	name := fmt.Sprintf("partition-%s", partition.Name)
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: "DiskPartitionNode",
			Type: string(partSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_HARDWARE,
			ProviderId: name,
			Name:       name,
		},
		Spec: specAny,
	}

	ref := &resourcev1.ResourceRef{
		TypeUrl: string(partSpec.ProtoReflect().Descriptor().FullName()),
		Name:    name,
	}

	return resource, ref, nil
}

// buildNetworkTopology builds the network nodes and relationships
func (b *Builder) buildNetworkTopology(ctx context.Context, netInfo []performance.NetworkInfo, systemRef *resourcev1.ResourceRef) error {
	for _, iface := range netInfo {
		// Create network interface node
		netNode, netRef, err := b.createNetworkInterfaceNode(&iface)
		if err != nil {
			return fmt.Errorf("failed to create network interface %s: %w", iface.Interface, err)
		}

		// Store the network node
		if err := b.store.AddResource(netNode); err != nil {
			return fmt.Errorf("failed to add network interface: %w", err)
		}

		// Create containment relationship: System -> Network Interface
		if err := b.createContainsRelationship(systemRef, netRef, "physical"); err != nil {
			return fmt.Errorf("failed to create system->network relationship: %w", err)
		}
	}

	return nil
}

// createNetworkInterfaceNode creates a network interface node
func (b *Builder) createNetworkInterfaceNode(iface *performance.NetworkInfo) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	netSpec := &hardwarev1.NetworkInterfaceNode{
		Interface:  iface.Interface,
		MacAddress: iface.MACAddress,
		Speed:      iface.Speed,
		Duplex:     iface.Duplex,
		Mtu:        iface.MTU,
		Driver:     iface.Driver,
		Type:       iface.Type,
		OperState:  iface.OperState,
		Carrier:    iface.Carrier,
	}

	specAny, err := anypb.New(netSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal network spec: %w", err)
	}

	name := fmt.Sprintf("network-%s", iface.Interface)
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: "NetworkInterfaceNode",
			Type: string(netSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_HARDWARE,
			ProviderId: name,
			Name:       name,
		},
		Spec: specAny,
	}

	ref := &resourcev1.ResourceRef{
		TypeUrl: string(netSpec.ProtoReflect().Descriptor().FullName()),
		Name:    name,
	}

	return resource, ref, nil
}

// createContainsRelationship creates a containment relationship between two resources
func (b *Builder) createContainsRelationship(subject, object *resourcev1.ResourceRef, containsType string) error {
	predicate := &hardwarev1.ContainsPredicate{
		Type: containsType,
	}

	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return fmt.Errorf("failed to marshal contains predicate: %w", err)
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: "Contains",
			Type: string(predicate.ProtoReflect().Descriptor().FullName()),
		},
		Subject:   subject,
		Object:    object,
		Predicate: predicateAny,
	}

	if err := b.store.AddRelationships(relationship); err != nil {
		return fmt.Errorf("failed to add contains relationship: %w", err)
	}

	return nil
}

// createNUMAAffinityRelationship creates a NUMA affinity relationship
func (b *Builder) createNUMAAffinityRelationship(subject, object *resourcev1.ResourceRef, nodeID int32) error {
	predicate := &hardwarev1.NUMAAffinityPredicate{
		NodeId: nodeID,
	}

	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return fmt.Errorf("failed to marshal NUMA affinity predicate: %w", err)
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: "BelongsToNUMA",
			Type: string(predicate.ProtoReflect().Descriptor().FullName()),
		},
		Subject:   subject,
		Object:    object,
		Predicate: predicateAny,
	}

	if err := b.store.AddRelationships(relationship); err != nil {
		return fmt.Errorf("failed to add NUMA affinity relationship: %w", err)
	}

	return nil
}
