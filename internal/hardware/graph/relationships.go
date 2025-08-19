// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/types/known/anypb"

	hardwarev1 "github.com/antimetal/agent/pkg/api/antimetal/hardware/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/performance"
)

// createContainsRelationship creates a containment relationship between two resources
func (b *Builder) createContainsRelationship(subject, object *resourcev1.ResourceRef, containsType string) error {
	// Map string containment type to protobuf enum
	var containmentType hardwarev1.ContainmentType
	switch containsType {
	case "physical":
		containmentType = hardwarev1.ContainmentType_CONTAINMENT_TYPE_PHYSICAL
	case "logical":
		containmentType = hardwarev1.ContainmentType_CONTAINMENT_TYPE_LOGICAL
	case "partition":
		containmentType = hardwarev1.ContainmentType_CONTAINMENT_TYPE_PARTITION
	default:
		containmentType = hardwarev1.ContainmentType_CONTAINMENT_TYPE_UNKNOWN
	}

	// Create containment relationship data using protobuf message
	predicate := &hardwarev1.Contains{
		Type: containmentType,
	}

	// Use protobuf marshaling instead of JSON
	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return fmt.Errorf("failed to marshal contains relationship data: %w", err)
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: "Contains",
			Type: containsType,
		},
		Subject:   subject,
		Object:    object,
		Predicate: predicateAny,
	}

	if err := b.store.AddRelationships(relationship); err != nil {
		return fmt.Errorf("failed to add containment relationship: %w", err)
	}

	return nil
}

// createNUMAAffinityRelationship creates a NUMA affinity relationship
func (b *Builder) createNUMAAffinityRelationship(subject, object *resourcev1.ResourceRef, nodeID int32) error {
	// Create NUMA affinity relationship data using protobuf message
	predicate := &hardwarev1.NUMAAffinity{
		NodeId:   nodeID,
		Distance: 0, // Local affinity, no distance
	}

	// Use protobuf marshaling instead of JSON
	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return fmt.Errorf("failed to marshal NUMA affinity relationship data: %w", err)
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: "BelongsToNUMA",
			Type: fmt.Sprintf("numa-%d", nodeID),
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

// createSocketSharingRelationship creates a socket sharing relationship between CPU cores
func (b *Builder) createSocketSharingRelationship(core1, core2 *resourcev1.ResourceRef, physicalID int32) error {
	// Create socket sharing relationship data using protobuf message
	predicate := &hardwarev1.SocketSharing{
		PhysicalId: physicalID,
		SocketId:   physicalID, // Same as PhysicalID for consistency
	}

	// Use protobuf marshaling instead of JSON
	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return fmt.Errorf("failed to marshal socket sharing relationship data: %w", err)
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: "SharesSocket",
			Type: fmt.Sprintf("socket-%d", physicalID),
		},
		Subject:   core1,
		Object:    core2,
		Predicate: predicateAny,
	}

	if err := b.store.AddRelationships(relationship); err != nil {
		return fmt.Errorf("failed to add socket sharing relationship: %w", err)
	}

	return nil
}

// createNUMADistanceRelationship creates a NUMA distance relationship between NUMA nodes
func (b *Builder) createNUMADistanceRelationship(sourceNode, targetNode *resourcev1.ResourceRef, sourceNodeID, targetNodeID, distance int32) error {
	// Create NUMA distance relationship data using protobuf message
	predicate := &hardwarev1.NUMAAffinity{
		NodeId:   targetNodeID, // Target node ID
		Distance: distance,     // Distance metric
	}

	// Use protobuf marshaling instead of JSON
	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return fmt.Errorf("failed to marshal NUMA distance relationship data: %w", err)
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: "NUMADistance",
			Type: fmt.Sprintf("distance-%d", distance),
		},
		Subject:   sourceNode,
		Object:    targetNode,
		Predicate: predicateAny,
	}

	if err := b.store.AddRelationships(relationship); err != nil {
		return fmt.Errorf("failed to add NUMA distance relationship: %w", err)
	}

	return nil
}

// inferDiskBusType infers the bus type from the disk device name
func (b *Builder) inferDiskBusType(device string) string {
	switch {
	case strings.HasPrefix(device, "nvme"):
		return "nvme"
	case strings.HasPrefix(device, "sd"):
		return "sata" // Could be SAS, but SATA is more common
	case strings.HasPrefix(device, "hd"):
		return "ide"
	case strings.HasPrefix(device, "vd"):
		return "virtio"
	case strings.HasPrefix(device, "xvd"):
		return "virtio" // Xen virtual disk
	default:
		return "unknown"
	}
}

// inferNetworkBusType infers the bus type from the network interface type and driver
func (b *Builder) inferNetworkBusType(iface *performance.NetworkInfo) string {
	// For virtual interfaces, return appropriate type
	switch iface.Type {
	case "loopback", "bridge", "vlan", "bond":
		return "virtual"
	default:
		// For physical interfaces, try to infer from driver
		switch {
		case strings.Contains(iface.Driver, "virtio"):
			return "virtio"
		case strings.Contains(iface.Driver, "usb"):
			return "usb"
		default:
			return "pci" // Most physical NICs are PCI/PCIe
		}
	}
}

// createBusConnectionRelationship creates a bus connection relationship
func (b *Builder) createBusConnectionRelationship(device, system *resourcev1.ResourceRef, busType, busAddress string) error {
	// Map string bus type to protobuf enum
	var busTypeEnum hardwarev1.BusType
	switch busType {
	case "pci":
		busTypeEnum = hardwarev1.BusType_BUS_TYPE_PCI
	case "pcie":
		busTypeEnum = hardwarev1.BusType_BUS_TYPE_PCIE
	case "usb":
		busTypeEnum = hardwarev1.BusType_BUS_TYPE_USB
	case "sata":
		busTypeEnum = hardwarev1.BusType_BUS_TYPE_SATA
	case "nvme":
		busTypeEnum = hardwarev1.BusType_BUS_TYPE_NVME
	case "sas":
		busTypeEnum = hardwarev1.BusType_BUS_TYPE_SAS
	case "ide":
		busTypeEnum = hardwarev1.BusType_BUS_TYPE_IDE
	case "scsi":
		busTypeEnum = hardwarev1.BusType_BUS_TYPE_SCSI
	case "virtio":
		busTypeEnum = hardwarev1.BusType_BUS_TYPE_VIRTIO
	default:
		busTypeEnum = hardwarev1.BusType_BUS_TYPE_UNKNOWN
	}

	// Create bus connection relationship data using protobuf message
	predicate := &hardwarev1.ConnectedTo{
		BusType:    busTypeEnum,
		BusAddress: busAddress,
	}

	// Use protobuf marshaling instead of JSON
	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return fmt.Errorf("failed to marshal bus connection relationship data: %w", err)
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: "ConnectedTo",
			Type: busType,
		},
		Subject:   device,
		Object:    system,
		Predicate: predicateAny,
	}

	if err := b.store.AddRelationships(relationship); err != nil {
		return fmt.Errorf("failed to add bus connection relationship: %w", err)
	}

	return nil
}