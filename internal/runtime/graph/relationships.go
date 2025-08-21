// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// Runtime relationship creation using hardware protobuf definitions

// createParentOfRelationship creates a parent-child process relationship
func (b *Builder) createParentOfRelationship(parentRef, childRef *resourcev1.ResourceRef) error {
	// Create contains relationship data
	containsData := map[string]interface{}{
		"containment_type": "process",
		"relationship":     "parent_of",
	}

	predicateStruct, err := structpb.NewStruct(containsData)
	if err != nil {
		return err
	}

	predicate, err := anypb.New(predicateStruct)
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

// TODO: createContainerProcessRelationship creates a container-to-process relationship
// Currently commented out to avoid unused function warnings - will be integrated when
// container-process relationships are implemented in buildProcessTopology
/*
func (b *Builder) createContainerProcessRelationship(containerRef, processRef *resourcev1.ResourceRef) error {
	// Create contains relationship data
	containsData := map[string]interface{}{
		"containment_type": "process",
		"relationship":     "contains",
	}

	predicateStruct, err := structpb.NewStruct(containsData)
	if err != nil {
		return err
	}

	predicate, err := anypb.New(predicateStruct)
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
*/

// TODO: createContainerHardwareRelationship creates relationships between containers and hardware
// Currently commented out to avoid unused function warnings - will be integrated when
// container-hardware relationships are implemented in buildContainerTopology
/*
func (b *Builder) createContainerHardwareRelationship(containerRef, hardwareRef *resourcev1.ResourceRef, relationshipType string) error {
	var relationshipData map[string]interface{}

	switch relationshipType {
	case "runs_on_cpu":
		// Container runs on specific CPU cores
		relationshipData = map[string]interface{}{
			"connection_type": "cpu_affinity",
			"relationship":    "runs_on",
		}
	case "allocated_memory":
		// Container allocated to specific memory/NUMA nodes
		relationshipData = map[string]interface{}{
			"connection_type": "numa_affinity",
			"relationship":    "allocated_to",
			"node_id":         0, // Will be filled in by caller
		}
	default:
		// Generic connection
		relationshipData = map[string]interface{}{
			"connection_type": relationshipType,
			"relationship":    "connected_to",
		}
	}

	predicateStruct, err := structpb.NewStruct(relationshipData)
	if err != nil {
		return err
	}

	predicateAny, err := anypb.New(predicateStruct)
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
*/
