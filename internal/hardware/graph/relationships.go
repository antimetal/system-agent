// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package hardwaregraph

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/types/known/anypb"

	hardwarev1 "github.com/antimetal/agent/pkg/api/antimetal/hardware/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/performance"
)

var kindRelationship = string((&resourcev1.Relationship{}).ProtoReflect().Descriptor().FullName())

// createContainsRelationship creates a containment relationship between two resources
func (b *Builder) createContainsRelationship(subject, object *resourcev1.ResourceRef, containsType string) (*resourcev1.Relationship, error) {
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

	predicate := &hardwarev1.Contains{
		Type: containmentType,
	}

	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal contains relationship data: %w", err)
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: string(predicate.ProtoReflect().Descriptor().FullName()),
		},
		Subject:   subject,
		Object:    object,
		Predicate: predicateAny,
	}

	return relationship, nil
}

// createNUMAAffinityRelationship creates a NUMA affinity relationship
func (b *Builder) createNUMAAffinityRelationship(subject, object *resourcev1.ResourceRef, nodeID int32) (*resourcev1.Relationship, error) {
	predicate := &hardwarev1.NUMAAffinity{
		NodeId:   nodeID,
		Distance: 0,
	}

	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal NUMA affinity relationship data: %w", err)
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: string(predicate.ProtoReflect().Descriptor().FullName()),
		},
		Subject:   subject,
		Object:    object,
		Predicate: predicateAny,
	}

	return relationship, nil
}

// createSocketSharingRelationship creates a socket sharing relationship between CPU cores
func (b *Builder) createSocketSharingRelationship(core1, core2 *resourcev1.ResourceRef, physicalID int32) (*resourcev1.Relationship, error) {
	predicate := &hardwarev1.SocketSharing{
		PhysicalId: physicalID,
		SocketId:   physicalID,
	}

	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal socket sharing relationship data: %w", err)
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: string(predicate.ProtoReflect().Descriptor().FullName()),
		},
		Subject:   core1,
		Object:    core2,
		Predicate: predicateAny,
	}

	return relationship, nil
}

// createNUMADistanceRelationship creates a NUMA distance relationship between NUMA nodes
func (b *Builder) createNUMADistanceRelationship(sourceNode, targetNode *resourcev1.ResourceRef, sourceNodeID, targetNodeID, distance int32) (*resourcev1.Relationship, error) {
	predicate := &hardwarev1.NUMAAffinity{
		NodeId:   targetNodeID,
		Distance: distance,
	}

	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal NUMA distance relationship data: %w", err)
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: string(predicate.ProtoReflect().Descriptor().FullName()),
		},
		Subject:   sourceNode,
		Object:    targetNode,
		Predicate: predicateAny,
	}

	return relationship, nil
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
func (b *Builder) createBusConnectionRelationship(device, system *resourcev1.ResourceRef, busType, busAddress string) (*resourcev1.Relationship, error) {
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

	predicate := &hardwarev1.ConnectedTo{
		BusType:    busTypeEnum,
		BusAddress: busAddress,
	}

	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bus connection relationship data: %w", err)
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: string(predicate.ProtoReflect().Descriptor().FullName()),
		},
		Subject:   device,
		Object:    system,
		Predicate: predicateAny,
	}

	return relationship, nil
}
