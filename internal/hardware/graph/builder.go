// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	"context"
	"fmt"

	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/resource"
	"github.com/go-logr/logr"
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

	// Add system node to store
	if err := b.store.UpdateResource(systemNode); err != nil {
		return fmt.Errorf("failed to add system node: %w", err)
	}

	b.logger.V(1).Info("Created system node", "name", systemRef.Name)

	// Build CPU topology if CPU info is available
	if snapshot.Metrics.CPUInfo != nil {
		if err := b.buildCPUTopology(ctx, snapshot.Metrics.CPUInfo, systemRef); err != nil {
			return fmt.Errorf("failed to build CPU topology: %w", err)
		}
	}

	// Build memory topology if memory info is available
	if snapshot.Metrics.MemoryInfo != nil {
		if err := b.buildMemoryTopology(ctx, snapshot.Metrics.MemoryInfo, systemRef); err != nil {
			return fmt.Errorf("failed to build memory topology: %w", err)
		}
	}

	// Build disk topology if disk info is available
	if len(snapshot.Metrics.DiskInfo) > 0 {
		if err := b.buildDiskTopology(ctx, snapshot.Metrics.DiskInfo, systemRef); err != nil {
			return fmt.Errorf("failed to build disk topology: %w", err)
		}
	}

	// Build network topology if network info is available
	if len(snapshot.Metrics.NetworkInfo) > 0 {
		if err := b.buildNetworkTopology(ctx, snapshot.Metrics.NetworkInfo, systemRef); err != nil {
			return fmt.Errorf("failed to build network topology: %w", err)
		}
	}

	b.logger.Info("Successfully built hardware graph")
	return nil
}

// buildCPUTopology builds the CPU nodes and relationships
func (b *Builder) buildCPUTopology(ctx context.Context, cpuInfo *performance.CPUInfo, systemRef *resourcev1.ResourceRef) error {
	// Track unique physical packages and cores by socket
	packages := make(map[int32]*resourcev1.ResourceRef)
	coresBySocket := make(map[int32][]*resourcev1.ResourceRef)

	// Create CPU package nodes
	for _, core := range cpuInfo.Cores {
		if _, exists := packages[core.PhysicalID]; !exists {
			pkg, pkgRef, err := b.createCPUPackageNode(cpuInfo, core.PhysicalID, systemRef)
			if err != nil {
				return fmt.Errorf("failed to create CPU package: %w", err)
			}

			if err := b.store.UpdateResource(pkg); err != nil {
				return fmt.Errorf("failed to add CPU package: %w", err)
			}

			packages[core.PhysicalID] = pkgRef

			// Create containment relationship: System -> CPU Package
			if err := b.createContainsRelationship(systemRef, pkgRef, "physical"); err != nil {
				return fmt.Errorf("failed to create system->package relationship: %w", err)
			}
		}
	}

	// Create CPU core nodes
	for _, core := range cpuInfo.Cores {
		coreNode, coreRef, err := b.createCPUCoreNode(&core, systemRef)
		if err != nil {
			return fmt.Errorf("failed to create CPU core: %w", err)
		}

		if err := b.store.UpdateResource(coreNode); err != nil {
			return fmt.Errorf("failed to add CPU core: %w", err)
		}

		// Track cores by socket for SharesSocket relationships
		coresBySocket[core.PhysicalID] = append(coresBySocket[core.PhysicalID], coreRef)

		// Create containment relationship: CPU Package -> CPU Core
		if packageRef, exists := packages[core.PhysicalID]; exists {
			if err := b.createContainsRelationship(packageRef, coreRef, "physical"); err != nil {
				return fmt.Errorf("failed to create package->core relationship: %w", err)
			}
		}
	}

	// Create SharesSocket relationships between cores on the same physical socket
	for physicalID, cores := range coresBySocket {
		for i, core1 := range cores {
			for j, core2 := range cores {
				if i != j { // Don't create self-relationships
					if err := b.createSocketSharingRelationship(core1, core2, physicalID); err != nil {
						return fmt.Errorf("failed to create socket sharing relationship: %w", err)
					}
				}
			}
		}
	}

	return nil
}

// buildMemoryTopology builds memory nodes and relationships
func (b *Builder) buildMemoryTopology(ctx context.Context, memInfo *performance.MemoryInfo, systemRef *resourcev1.ResourceRef) error {
	// Create memory module node
	memNode, memRef, err := b.createMemoryModuleNode(memInfo, systemRef)
	if err != nil {
		return fmt.Errorf("failed to create memory node: %w", err)
	}

	if err := b.store.UpdateResource(memNode); err != nil {
		return fmt.Errorf("failed to add memory node: %w", err)
	}

	// Create containment relationship: System -> Memory Module
	if err := b.createContainsRelationship(systemRef, memRef, "physical"); err != nil {
		return fmt.Errorf("failed to create system->memory relationship: %w", err)
	}

	// Create NUMA nodes if enabled
	if memInfo.NUMAEnabled {
		// Track NUMA node references for distance relationships
		numaRefs := make(map[int32]*resourcev1.ResourceRef)
		numaNodes := make(map[int32]*performance.NUMANode)

		// First pass: create all NUMA nodes
		for _, numaNode := range memInfo.NUMANodes {
			numa, numaRef, err := b.createNUMANode(&numaNode, systemRef)
			if err != nil {
				return fmt.Errorf("failed to create NUMA node: %w", err)
			}

			if err := b.store.UpdateResource(numa); err != nil {
				return fmt.Errorf("failed to add NUMA node: %w", err)
			}

			// Track for distance relationships
			numaRefs[numaNode.NodeID] = numaRef
			numaNodes[numaNode.NodeID] = &numaNode

			// Create containment relationship: System -> NUMA Node
			if err := b.createContainsRelationship(systemRef, numaRef, "logical"); err != nil {
				return fmt.Errorf("failed to create system->numa relationship: %w", err)
			}

			// Create NUMA affinity relationship: Memory Module -> NUMA Node
			if err := b.createNUMAAffinityRelationship(memRef, numaRef, numaNode.NodeID); err != nil {
				return fmt.Errorf("failed to create memory->numa affinity: %w", err)
			}
		}

		// Second pass: create NUMA distance relationships between nodes
		for nodeID, nodeRef := range numaRefs {
			node := numaNodes[nodeID]
			// Create distance relationships to other NUMA nodes
			for otherNodeID, distance := range node.Distances {
				if otherNodeRef, exists := numaRefs[int32(otherNodeID)]; exists && nodeID != int32(otherNodeID) {
					if err := b.createNUMADistanceRelationship(nodeRef, otherNodeRef, nodeID, int32(otherNodeID), int32(distance)); err != nil {
						return fmt.Errorf("failed to create NUMA distance relationship %d->%d: %w", nodeID, otherNodeID, err)
					}
				}
			}
		}
	}

	return nil
}

// buildDiskTopology builds disk device and partition nodes and relationships
func (b *Builder) buildDiskTopology(ctx context.Context, diskInfo []*performance.DiskInfo, systemRef *resourcev1.ResourceRef) error {
	for _, disk := range diskInfo {
		// Create disk device node
		diskNode, diskRef, err := b.createDiskDeviceNode(disk, systemRef)
		if err != nil {
			return fmt.Errorf("failed to create disk node: %w", err)
		}

		if err := b.store.UpdateResource(diskNode); err != nil {
			return fmt.Errorf("failed to add disk node: %w", err)
		}

		// Create containment relationship: System -> Disk Device
		if err := b.createContainsRelationship(systemRef, diskRef, "physical"); err != nil {
			return fmt.Errorf("failed to create system->disk relationship: %w", err)
		}

		// Create bus connection relationship: Disk Device -> System (via hardware bus)
		busType := b.inferDiskBusType(disk.Device)
		if err := b.createBusConnectionRelationship(diskRef, systemRef, busType, ""); err != nil {
			return fmt.Errorf("failed to create disk->system bus relationship: %w", err)
		}

		// Create partition nodes
		for _, partition := range disk.Partitions {
			partNode, partRef, err := b.createDiskPartitionNode(&partition, disk.Device, systemRef)
			if err != nil {
				return fmt.Errorf("failed to create partition node: %w", err)
			}

			if err := b.store.UpdateResource(partNode); err != nil {
				return fmt.Errorf("failed to add partition node: %w", err)
			}

			// Create containment relationship: Disk Device -> Partition
			if err := b.createContainsRelationship(diskRef, partRef, "partition"); err != nil {
				return fmt.Errorf("failed to create disk->partition relationship: %w", err)
			}
		}
	}

	return nil
}

// buildNetworkTopology builds network interface nodes and relationships
func (b *Builder) buildNetworkTopology(ctx context.Context, netInfo []*performance.NetworkInfo, systemRef *resourcev1.ResourceRef) error {
	for _, iface := range netInfo {
		// Create network interface node
		netNode, netRef, err := b.createNetworkInterfaceNode(iface, systemRef)
		if err != nil {
			return fmt.Errorf("failed to create network node: %w", err)
		}

		if err := b.store.UpdateResource(netNode); err != nil {
			return fmt.Errorf("failed to add network node: %w", err)
		}

		// Create containment relationship: System -> Network Interface
		if err := b.createContainsRelationship(systemRef, netRef, "physical"); err != nil {
			return fmt.Errorf("failed to create system->network relationship: %w", err)
		}

		// Create bus connection relationship: Network Interface -> System (via hardware bus)
		busType := b.inferNetworkBusType(iface)
		if err := b.createBusConnectionRelationship(netRef, systemRef, busType, ""); err != nil {
			return fmt.Errorf("failed to create network->system bus relationship: %w", err)
		}
	}

	return nil
}
