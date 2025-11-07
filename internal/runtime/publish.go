// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtime

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/go-logr/logr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/antimetal/agent/internal/resource"
	agentv1 "github.com/antimetal/agent/pkg/api/antimetal/agent/v1"
	hardwarev1 "github.com/antimetal/agent/pkg/api/antimetal/hardware/v1"
	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	k8sv1 "github.com/antimetal/agent/pkg/api/kubernetes/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/config/environment"
	"github.com/antimetal/agent/pkg/host"
)

var (
	kindResource     = string(proto.MessageName(&resourcev1.Resource{}))
	kindRelationship = string(proto.MessageName(&resourcev1.Relationship{}))
)

// PublishInstance publishes the runtime Instance to the resource store with relationships
// to Pod (if running in Kubernetes) and SystemNode.
func PublishInstance(ctx context.Context, store resource.Store, logger logr.Logger) error {
	if store == nil {
		return fmt.Errorf("store is required")
	}

	inst := GetInstance()
	if inst == nil {
		return fmt.Errorf("runtime instance not available")
	}

	// Get instance ID as base64 string for resource name
	instanceName := base64.URLEncoding.EncodeToString(inst.Id)

	// Create Instance resource
	instanceResource, instanceRef, err := createInstanceResource(inst, instanceName)
	if err != nil {
		return fmt.Errorf("failed to create instance resource: %w", err)
	}

	// Publish Instance resource
	if err := store.UpdateResource(instanceResource); err != nil {
		return fmt.Errorf("failed to publish instance resource: %w", err)
	}

	logger.Info("Published Instance resource", "instanceID", instanceName)

	// Create relationships
	relationships := make([]*resourcev1.Relationship, 0)

	// Create Pod → Instance relationship if running in Kubernetes
	podMetadata := environment.GetPodMetadata()
	if podMetadata != nil {
		podRels, err := createPodRelationships(instanceRef, podMetadata)
		if err != nil {
			logger.Error(err, "Failed to create Pod relationships", "pod", podMetadata.Name)
		} else {
			relationships = append(relationships, podRels...)
			logger.Info("Created Pod → Instance relationship", "pod", podMetadata.Name)
		}
	} else {
		logger.V(1).Info("Pod metadata not available, skipping Pod relationships (not running in Kubernetes)")
	}

	// Create SystemNode ↔ Instance relationship
	systemNodeID, err := getSystemNodeID()
	if err != nil {
		logger.Error(err, "Failed to get system node ID")
	} else {
		systemNodeRels, err := createSystemNodeRelationships(instanceRef, systemNodeID)
		if err != nil {
			logger.Error(err, "Failed to create SystemNode relationships")
		} else {
			relationships = append(relationships, systemNodeRels...)
			logger.Info("Created SystemNode ↔ Instance relationship", "systemNodeID", systemNodeID)
		}
	}

	// Publish all relationships
	if len(relationships) > 0 {
		if err := store.AddRelationships(relationships...); err != nil {
			return fmt.Errorf("failed to publish relationships: %w", err)
		}
		logger.V(1).Info("Published Instance relationships", "count", len(relationships))
	}

	return nil
}

func createInstanceResource(inst *agentv1.Instance, instanceName string) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Marshal Instance spec to Any
	specAny, err := anypb.New(inst)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal instance spec: %w", err)
	}

	// Get full type name
	typeName := string(inst.ProtoReflect().Descriptor().FullName())

	// Create tags
	tags := []*resourcev1.Tag{
		{Key: "version", Value: fmt.Sprintf("%d.%d.%d",
			inst.Build.Version.Major,
			inst.Build.Version.Minor,
			inst.Build.Version.Patch)},
		{Key: "revision", Value: inst.Build.Revision},
	}

	if inst.LinuxRuntime != nil {
		tags = append(tags, &resourcev1.Tag{
			Key:   "kernel_version",
			Value: inst.LinuxRuntime.KernelVersion,
		})
	}

	// Create resource
	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: typeName,
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_ANTIMETAL,
			ProviderId: instanceName,
			Name:       instanceName,
			Tags:       tags,
		},
		Spec: specAny,
	}

	// Create reference
	// Instance is host-scoped (not namespace-scoped), so no namespace is set
	ref := &resourcev1.ResourceRef{
		TypeUrl: typeName,
		Name:    instanceName,
	}

	return rsrc, ref, nil
}

func createPodRelationships(instanceRef *resourcev1.ResourceRef, podMeta *environment.PodMetadata) ([]*resourcev1.Relationship, error) {
	// Kubernetes API types use their Go package path as TypeUrl
	// See: https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.31/#pod-v1-core
	const podTypeUrl = "k8s.io.api.core.v1.Pod"

	// Create Pod reference
	podRef := &resourcev1.ResourceRef{
		TypeUrl: podTypeUrl,
		Name:    podMeta.Name,
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Kube{
				Kube: &resourcev1.KubernetesNamespace{
					Namespace: podMeta.Namespace,
				},
			},
		},
	}

	// Create forward relationship: Pod --RegisteredAs--> Instance
	registeredAs := &k8sv1.RegisteredAs{}
	registeredAsPredicate, err := anypb.New(registeredAs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RegisteredAs predicate: %w", err)
	}

	registeredAsType := string(proto.MessageName(registeredAs))

	registeredAsRel := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: registeredAsType,
		},
		Subject:   podRef,
		Predicate: registeredAsPredicate,
		Object:    instanceRef,
	}

	// Create inverse relationship: Instance --Underlying--> Pod
	underlying := &k8sv1.Underlying{}
	underlyingPredicate, err := anypb.New(underlying)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Underlying predicate: %w", err)
	}

	underlyingType := string(proto.MessageName(underlying))

	underlyingRel := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: underlyingType,
		},
		Subject:   instanceRef,
		Predicate: underlyingPredicate,
		Object:    podRef,
	}

	return []*resourcev1.Relationship{registeredAsRel, underlyingRel}, nil
}

func createSystemNodeRelationships(instanceRef *resourcev1.ResourceRef, systemNodeID string) ([]*resourcev1.Relationship, error) {
	// Create SystemNode reference
	systemNodeType := string(proto.MessageName(&hardwarev1.SystemNode{}))
	systemNodeRef := &resourcev1.ResourceRef{
		TypeUrl: systemNodeType,
		Name:    systemNodeID,
	}

	// Create forward relationship: SystemNode --Contains--> Instance
	contains := &runtimev1.Contains{
		Type: runtimev1.ContainmentType_CONTAINMENT_TYPE_RUNTIME,
	}
	containsPredicate, err := anypb.New(contains)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Contains predicate: %w", err)
	}

	containsType := string(proto.MessageName(contains))

	containsRel := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: containsType,
		},
		Subject:   systemNodeRef,
		Predicate: containsPredicate,
		Object:    instanceRef,
	}

	// Create inverse relationship: Instance --ContainedBy--> SystemNode
	containedBy := &runtimev1.ContainedBy{
		Type: runtimev1.ContainmentType_CONTAINMENT_TYPE_RUNTIME,
	}
	containedByPredicate, err := anypb.New(containedBy)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ContainedBy predicate: %w", err)
	}

	containedByType := string(proto.MessageName(containedBy))

	containedByRel := &resourcev1.Relationship{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindRelationship,
			Type: containedByType,
		},
		Subject:   instanceRef,
		Predicate: containedByPredicate,
		Object:    systemNodeRef,
	}

	return []*resourcev1.Relationship{containsRel, containedByRel}, nil
}

func getSystemNodeID() (string, error) {
	// Use canonical name for system node identification
	// This provides a robust fallback chain: MachineInfo → CloudProviderID → FQDN → MachineID
	name, err := host.CanonicalName()
	if err != nil {
		return "", fmt.Errorf("could not determine system node ID: %w", err)
	}
	return name, nil
}
