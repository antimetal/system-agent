// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package containergraph

import (
	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// Define Kind and Type constants using proto descriptor full names
var (
	kindRelationship = string(proto.MessageName(&resourcev1.Relationship{}))
	typeContains     = string(proto.MessageName(&runtimev1.Contains{}))
	typeContainedBy  = string(proto.MessageName(&runtimev1.ContainedBy{}))
)

// Runtime relationship creation using runtime protobuf definitions

// createParentOfRelationship creates bidirectional parent-child process relationships
func (b *Builder) createParentOfRelationship(parentRef, childRef *resourcev1.ResourceRef) ([]*resourcev1.Relationship, error) {
	// Create forward relationship: parent Contains child
	contains := &runtimev1.Contains{
		Type: runtimev1.ContainmentType_CONTAINMENT_TYPE_RUNTIME,
	}

	containsPredicate, err := anypb.New(contains)
	if err != nil {
		return nil, err
	}

	containsRel := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: typeContains,
		},
		Subject:   parentRef,
		Predicate: containsPredicate,
		Object:    childRef,
	}

	// Create inverse relationship: child ContainedBy parent
	containedBy := &runtimev1.ContainedBy{
		Type: runtimev1.ContainmentType_CONTAINMENT_TYPE_RUNTIME,
	}

	containedByPredicate, err := anypb.New(containedBy)
	if err != nil {
		return nil, err
	}

	containedByRel := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: typeContainedBy,
		},
		Subject:   childRef,
		Predicate: containedByPredicate,
		Object:    parentRef,
	}

	b.logger.V(2).Info("Built bidirectional parent-child relationships",
		"parent", parentRef.Name,
		"child", childRef.Name)

	return []*resourcev1.Relationship{containsRel, containedByRel}, nil
}

// createContainerProcessRelationship creates bidirectional container-to-process relationships
func (b *Builder) createContainerProcessRelationship(containerRef, processRef *resourcev1.ResourceRef) ([]*resourcev1.Relationship, error) {
	// Create forward relationship: container Contains process
	contains := &runtimev1.Contains{
		Type: runtimev1.ContainmentType_CONTAINMENT_TYPE_RUNTIME,
	}

	containsPredicate, err := anypb.New(contains)
	if err != nil {
		return nil, err
	}

	containsRel := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: typeContains,
		},
		Subject:   containerRef,
		Predicate: containsPredicate,
		Object:    processRef,
	}

	// Create inverse relationship: process ContainedBy container
	containedBy := &runtimev1.ContainedBy{
		Type: runtimev1.ContainmentType_CONTAINMENT_TYPE_RUNTIME,
	}

	containedByPredicate, err := anypb.New(containedBy)
	if err != nil {
		return nil, err
	}

	containedByRel := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: typeContainedBy,
		},
		Subject:   processRef,
		Predicate: containedByPredicate,
		Object:    containerRef,
	}

	b.logger.V(2).Info("Built bidirectional container-process relationships",
		"container", containerRef.Name,
		"process", processRef.Name)

	return []*resourcev1.Relationship{containsRel, containedByRel}, nil
}

// createContainerHardwareRelationship creates bidirectional relationships between containers and hardware
func (b *Builder) createContainerHardwareRelationship(containerRef, hardwareRef *resourcev1.ResourceRef) ([]*resourcev1.Relationship, error) {
	// Create forward relationship: hardware Contains container
	contains := &runtimev1.Contains{
		Type: runtimev1.ContainmentType_CONTAINMENT_TYPE_RUNTIME,
	}

	containsPredicate, err := anypb.New(contains)
	if err != nil {
		return nil, err
	}

	containsRel := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: typeContains,
		},
		Subject:   hardwareRef, // Hardware contains the container
		Predicate: containsPredicate,
		Object:    containerRef,
	}

	// Create inverse relationship: container ContainedBy hardware
	containedBy := &runtimev1.ContainedBy{
		Type: runtimev1.ContainmentType_CONTAINMENT_TYPE_RUNTIME,
	}

	containedByPredicate, err := anypb.New(containedBy)
	if err != nil {
		return nil, err
	}

	containedByRel := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: typeContainedBy,
		},
		Subject:   containerRef, // Container is contained by hardware
		Predicate: containedByPredicate,
		Object:    hardwareRef,
	}

	b.logger.V(2).Info("Built bidirectional container-hardware relationships",
		"container", containerRef.Name,
		"hardware", hardwareRef.Name)

	return []*resourcev1.Relationship{containsRel, containedByRel}, nil
}
