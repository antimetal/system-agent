// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	hardwarev1 "github.com/antimetal/agent/pkg/api/antimetal/hardware/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"google.golang.org/protobuf/types/known/anypb"
)

// Runtime relationship creation using hardware protobuf definitions

// createParentOfRelationship creates a parent-child process relationship
func (b *Builder) createParentOfRelationship(parentRef, childRef *resourcev1.ResourceRef) error {
	// Use Contains relationship with logical containment type for process hierarchy
	contains := &hardwarev1.Contains{
		Type: hardwarev1.ContainmentType_CONTAINMENT_TYPE_LOGICAL,
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
	// Use Contains relationship with logical containment type for container-process
	contains := &hardwarev1.Contains{
		Type: hardwarev1.ContainmentType_CONTAINMENT_TYPE_LOGICAL,
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
func (b *Builder) createContainerHardwareRelationship(containerRef, hardwareRef *resourcev1.ResourceRef) error {
	// Use Contains relationship with partition type for container-hardware affinity
	// This represents the container being assigned to specific hardware resources
	contains := &hardwarev1.Contains{
		Type: hardwarev1.ContainmentType_CONTAINMENT_TYPE_PARTITION,
	}

	predicateAny, err := anypb.New(contains)
	if err != nil {
		return err
	}

	relationship := &resourcev1.Relationship{
		Subject:   hardwareRef, // Hardware contains the container
		Predicate: predicateAny,
		Object:    containerRef, // Container is contained by hardware
	}

	if err := b.store.AddRelationships(relationship); err != nil {
		return err
	}

	b.logger.V(2).Info("Created container-hardware relationship",
		"container", containerRef.Name,
		"hardware", hardwareRef.Name)

	return nil
}
