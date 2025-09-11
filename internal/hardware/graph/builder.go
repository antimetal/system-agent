// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	"context"
	"errors"
	"fmt"

	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/internal/resource"
	"github.com/go-logr/logr"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
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
	relationships := make([]*resourcev1.Relationship, 0)
	packages := make(map[int32]*resourcev1.ResourceRef)
	coresBySocket := make(map[int32][]*resourcev1.ResourceRef)

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

			rel, err := b.createContainsRelationship(systemRef, pkgRef, "physical")
			if err != nil {
				return fmt.Errorf("failed to create system->package relationship: %w", err)
			}
			relationships = append(relationships, rel)
		}
	}

	for _, core := range cpuInfo.Cores {
		coreNode, coreRef, err := b.createCPUCoreNode(&core, systemRef)
		if err != nil {
			return fmt.Errorf("failed to create CPU core: %w", err)
		}

		if err := b.store.UpdateResource(coreNode); err != nil {
			return fmt.Errorf("failed to add CPU core: %w", err)
		}

		coresBySocket[core.PhysicalID] = append(coresBySocket[core.PhysicalID], coreRef)

		if packageRef, exists := packages[core.PhysicalID]; exists {
			rel, err := b.createContainsRelationship(packageRef, coreRef, "physical")
			if err != nil {
				return fmt.Errorf("failed to create package->core relationship: %w", err)
			}
			relationships = append(relationships, rel)
		}
	}

	for physicalID, cores := range coresBySocket {
		for i, core1 := range cores {
			for j, core2 := range cores {
				if i != j {
					rel, err := b.createSocketSharingRelationship(core1, core2, physicalID)
					if err != nil {
						return fmt.Errorf("failed to create socket sharing relationship: %w", err)
					}
					relationships = append(relationships, rel)
				}
			}
		}
	}

	if len(relationships) > 0 {
		if err := b.addRelationships(relationships...); err != nil {
			return fmt.Errorf("failed to add CPU topology relationships: %w", err)
		}
	}

	return nil
}

// buildMemoryTopology builds memory nodes and relationships
func (b *Builder) buildMemoryTopology(ctx context.Context, memInfo *performance.MemoryInfo, systemRef *resourcev1.ResourceRef) error {
	relationships := make([]*resourcev1.Relationship, 0)
	memNode, memRef, err := b.createMemoryModuleNode(memInfo, systemRef)
	if err != nil {
		return fmt.Errorf("failed to create memory node: %w", err)
	}

	if err := b.store.UpdateResource(memNode); err != nil {
		return fmt.Errorf("failed to add memory node: %w", err)
	}

	rel, err := b.createContainsRelationship(systemRef, memRef, "physical")
	if err != nil {
		return fmt.Errorf("failed to create system->memory relationship: %w", err)
	}
	relationships = append(relationships, rel)

	if memInfo.NUMAEnabled {
		numaRefs := make(map[int32]*resourcev1.ResourceRef)
		numaNodes := make(map[int32]*performance.NUMANode)

		for _, numaNode := range memInfo.NUMANodes {
			numa, numaRef, err := b.createNUMANode(&numaNode, systemRef)
			if err != nil {
				return fmt.Errorf("failed to create NUMA node: %w", err)
			}

			if err := b.store.UpdateResource(numa); err != nil {
				return fmt.Errorf("failed to add NUMA node: %w", err)
			}

			numaRefs[numaNode.NodeID] = numaRef
			numaNodes[numaNode.NodeID] = &numaNode

			rel, err := b.createContainsRelationship(systemRef, numaRef, "logical")
			if err != nil {
				return fmt.Errorf("failed to create system->numa relationship: %w", err)
			}
			relationships = append(relationships, rel)

			rel, err = b.createNUMAAffinityRelationship(memRef, numaRef, numaNode.NodeID)
			if err != nil {
				return fmt.Errorf("failed to create memory->numa affinity: %w", err)
			}
			relationships = append(relationships, rel)
		}

		for nodeID, nodeRef := range numaRefs {
			node := numaNodes[nodeID]
			for otherNodeID, distance := range node.Distances {
				if otherNodeRef, exists := numaRefs[int32(otherNodeID)]; exists && nodeID != int32(otherNodeID) {
					rel, err := b.createNUMADistanceRelationship(nodeRef, otherNodeRef, nodeID, int32(otherNodeID), int32(distance))
					if err != nil {
						return fmt.Errorf("failed to create NUMA distance relationship %d->%d: %w", nodeID, otherNodeID, err)
					}
					relationships = append(relationships, rel)
				}
			}
		}
	}

	if len(relationships) > 0 {
		if err := b.addRelationships(relationships...); err != nil {
			return fmt.Errorf("failed to add memory topology relationships: %w", err)
		}
	}

	return nil
}

// buildDiskTopology builds disk device and partition nodes and relationships
func (b *Builder) buildDiskTopology(ctx context.Context, diskInfo []*performance.DiskInfo, systemRef *resourcev1.ResourceRef) error {
	relationships := make([]*resourcev1.Relationship, 0)
	for _, disk := range diskInfo {
		diskNode, diskRef, err := b.createDiskDeviceNode(disk, systemRef)
		if err != nil {
			return fmt.Errorf("failed to create disk node: %w", err)
		}

		if err := b.store.UpdateResource(diskNode); err != nil {
			return fmt.Errorf("failed to add disk node: %w", err)
		}

		rel, err := b.createContainsRelationship(systemRef, diskRef, "physical")
		if err != nil {
			return fmt.Errorf("failed to create system->disk relationship: %w", err)
		}
		relationships = append(relationships, rel)

		busType := b.inferDiskBusType(disk.Device)
		rel, err = b.createBusConnectionRelationship(diskRef, systemRef, busType, "")
		if err != nil {
			return fmt.Errorf("failed to create disk->system bus relationship: %w", err)
		}
		relationships = append(relationships, rel)

		for _, partition := range disk.Partitions {
			partNode, partRef, err := b.createDiskPartitionNode(&partition, disk.Device, systemRef)
			if err != nil {
				return fmt.Errorf("failed to create partition node: %w", err)
			}

			if err := b.store.UpdateResource(partNode); err != nil {
				return fmt.Errorf("failed to add partition node: %w", err)
			}

			rel, err := b.createContainsRelationship(diskRef, partRef, "partition")
			if err != nil {
				return fmt.Errorf("failed to create disk->partition relationship: %w", err)
			}
			relationships = append(relationships, rel)
		}
	}

	if len(relationships) > 0 {
		if err := b.addRelationships(relationships...); err != nil {
			return fmt.Errorf("failed to add disk topology relationships: %w", err)
		}
	}

	return nil
}

// buildNetworkTopology builds network interface nodes and relationships
func (b *Builder) buildNetworkTopology(ctx context.Context, netInfo []*performance.NetworkInfo, systemRef *resourcev1.ResourceRef) error {
	relationships := make([]*resourcev1.Relationship, 0)
	for _, iface := range netInfo {
		netNode, netRef, err := b.createNetworkInterfaceNode(iface, systemRef)
		if err != nil {
			return fmt.Errorf("failed to create network node: %w", err)
		}

		if err := b.store.UpdateResource(netNode); err != nil {
			return fmt.Errorf("failed to add network node: %w", err)
		}

		rel, err := b.createContainsRelationship(systemRef, netRef, "physical")
		if err != nil {
			return fmt.Errorf("failed to create system->network relationship: %w", err)
		}
		relationships = append(relationships, rel)

		busType := b.inferNetworkBusType(iface)
		rel, err = b.createBusConnectionRelationship(netRef, systemRef, busType, "")
		if err != nil {
			return fmt.Errorf("failed to create network->system bus relationship: %w", err)
		}
		relationships = append(relationships, rel)
	}

	if len(relationships) > 0 {
		if err := b.addRelationships(relationships...); err != nil {
			return fmt.Errorf("failed to add network topology relationships: %w", err)
		}
	}

	return nil
}

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
