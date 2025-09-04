// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"google.golang.org/protobuf/types/known/anypb"
)

// Define Kind and Type constants using proto descriptor full names
var (
	kindRelationship = string((&resourcev1.Relationship{}).ProtoReflect().Descriptor().FullName())
	typeContains     = string((&runtimev1.Contains{}).ProtoReflect().Descriptor().FullName())
	typeContainedBy  = string((&runtimev1.ContainedBy{}).ProtoReflect().Descriptor().FullName())
)

// Runtime relationship creation using runtime protobuf definitions

// createParentOfRelationship creates a parent-child process relationship
func (b *Builder) createParentOfRelationship(parentRef, childRef *resourcev1.ResourceRef) error {
	// Use Contains relationship with runtime containment type for process hierarchy
	contains := &runtimev1.Contains{
		Type: runtimev1.ContainmentType_CONTAINMENT_TYPE_RUNTIME,
	}

	predicate, err := anypb.New(contains)
	if err != nil {
		return err
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: typeContains,
		},
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
	// Use Contains relationship with runtime containment type for container-process
	contains := &runtimev1.Contains{
		Type: runtimev1.ContainmentType_CONTAINMENT_TYPE_RUNTIME,
	}

	predicate, err := anypb.New(contains)
	if err != nil {
		return err
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: typeContains,
		},
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
	// Use ContainedBy relationship for container-hardware affinity
	// This represents the container being contained by specific hardware resources
	containedBy := &runtimev1.ContainedBy{
		Type: runtimev1.ContainmentType_CONTAINMENT_TYPE_RUNTIME,
	}

	predicateAny, err := anypb.New(containedBy)
	if err != nil {
		return err
	}

	relationship := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: typeContainedBy,
		},
		Subject:   containerRef, // Container is contained by hardware (subject is what is contained)
		Predicate: predicateAny,
		Object:    hardwareRef, // Hardware is what contains
	}

	if err := b.store.AddRelationships(relationship); err != nil {
		return err
	}

	b.logger.V(2).Info("Created container-hardware relationship",
		"container", containerRef.Name,
		"hardware", hardwareRef.Name)

	return nil
}
