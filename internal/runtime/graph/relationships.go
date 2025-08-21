// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"google.golang.org/protobuf/types/known/anypb"
)

// Simple relationship types for runtime topology
// TODO: Move to proper protobuf definitions when hardware package is available

// Contains represents hierarchical containment relationship
type Contains struct {
	ContainmentType string `json:"containment_type"`
}

// ConnectedTo represents a connection between runtime and hardware
type ConnectedTo struct {
	ConnectionType string `json:"connection_type"`
	BusType        string `json:"bus_type,omitempty"`
}

// NUMAAffinity represents NUMA affinity relationship
type NUMAAffinity struct {
	NodeId   int32 `json:"node_id"`
	Distance int32 `json:"distance,omitempty"`
}

// createParentOfRelationship creates a parent-child process relationship
func (b *Builder) createParentOfRelationship(parentRef, childRef *resourcev1.ResourceRef) error {
	// Use the Contains relationship for process hierarchy
	contains := &Contains{
		ContainmentType: "process",
	}

	predicate, err := anypb.New(contains)
	if err != nil {
		return err
	}

	relationship := &resourcev1.Relationship{
		Subject:   parentRef,
		Predicate: predicate,
		Object:    childRef,
	}

	if err := b.store.AddRelationships(relationship); err != nil {
		return err
	}

	b.logger.V(2).Info("Created parent-child relationship",
		"parent", parentRef.Name,
		"child", childRef.Name)

	return nil
}

// createContainerProcessRelationship creates a container-to-process relationship
func (b *Builder) createContainerProcessRelationship(containerRef, processRef *resourcev1.ResourceRef) error {
	// Use the Contains relationship for container containing processes
	contains := &Contains{
		ContainmentType: "process",
	}

	predicate, err := anypb.New(contains)
	if err != nil {
		return err
	}

	relationship := &resourcev1.Relationship{
		Subject:   containerRef,
		Predicate: predicate,
		Object:    processRef,
	}

	if err := b.store.AddRelationships(relationship); err != nil {
		return err
	}

	b.logger.V(2).Info("Created container-process relationship",
		"container", containerRef.Name,
		"process", processRef.Name)

	return nil
}

// createContainerHardwareRelationship creates relationships between containers and hardware
func (b *Builder) createContainerHardwareRelationship(containerRef, hardwareRef *resourcev1.ResourceRef, relationshipType string) error {
	var predicate any

	switch relationshipType {
	case "runs_on_cpu":
		// Container runs on specific CPU cores
		predicate = &ConnectedTo{
			ConnectionType: "cpu_affinity",
			BusType:       "",
		}
	case "allocated_memory":
		// Container allocated to specific memory/NUMA nodes
		predicate = &NUMAAffinity{
			NodeId:   0, // Will be filled in by caller
			Distance: 0,
		}
	default:
		// Generic connection
		predicate = &ConnectedTo{
			ConnectionType: relationshipType,
			BusType:       "",
		}
	}

	predicateAny, err := anypb.New(predicate)
	if err != nil {
		return err
	}

	relationship := &resourcev1.Relationship{
		Subject:   containerRef,
		Predicate: predicateAny,
		Object:    hardwareRef,
	}

	if err := b.store.AddRelationships(relationship); err != nil {
		return err
	}

	b.logger.V(2).Info("Created container-hardware relationship",
		"container", containerRef.Name,
		"hardware", hardwareRef.Name,
		"type", relationshipType)

	return nil
}