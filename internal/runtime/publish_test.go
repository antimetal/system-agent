// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtime

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/antimetal/agent/internal/resource"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/config/environment"
)

func TestCreateInstanceResource(t *testing.T) {
	instance := GetInstance()
	require.NotNil(t, instance, "Runtime instance should be available")

	instanceName := "test-instance"

	rsrc, ref, err := createInstanceResource(instance, instanceName)
	require.NoError(t, err)
	require.NotNil(t, rsrc)
	require.NotNil(t, ref)

	// Verify resource structure
	assert.Equal(t, kindResource, rsrc.Type.Kind)
	assert.Equal(t, "antimetal.agent.v1.Instance", rsrc.Type.Type)
	assert.Equal(t, resourcev1.Provider_PROVIDER_ANTIMETAL, rsrc.Metadata.Provider)
	assert.Equal(t, instanceName, rsrc.Metadata.Name)
	assert.NotNil(t, rsrc.Spec)

	// Verify tags
	foundVersion := false
	foundRevision := false
	for _, tag := range rsrc.Metadata.Tags {
		if tag.Key == "version" {
			foundVersion = true
		}
		if tag.Key == "revision" {
			foundRevision = true
		}
	}
	assert.True(t, foundVersion, "Should have version tag")
	assert.True(t, foundRevision, "Should have revision tag")

	// Verify reference
	assert.Equal(t, "antimetal.agent.v1.Instance", ref.TypeUrl)
	assert.Equal(t, instanceName, ref.Name)
	assert.Nil(t, ref.Namespace, "Instance should be host-scoped, not namespace-scoped")
}

func TestCreatePodRelationships(t *testing.T) {
	instanceRef := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.agent.v1.Instance",
		Name:    "test-instance",
	}

	podMeta := &environment.PodMetadata{
		Name:      "test-pod",
		Namespace: "test-namespace",
		UID:       "test-uid",
	}

	rels, err := createPodRelationships(instanceRef, podMeta)
	require.NoError(t, err)
	require.Len(t, rels, 1, "Should create one Pod → Instance relationship")

	rel := rels[0]

	// Verify relationship structure
	assert.Equal(t, kindRelationship, rel.Type.Kind)
	assert.Equal(t, "kubernetes.v1.RegisteredAs", rel.Type.Type)

	// Verify subject (Pod)
	assert.Equal(t, "k8s.io.api.core.v1.Pod", rel.Subject.TypeUrl)
	assert.Equal(t, "test-pod", rel.Subject.Name)
	assert.NotNil(t, rel.Subject.Namespace)

	// Verify object (Instance)
	assert.Equal(t, instanceRef.TypeUrl, rel.Object.TypeUrl)
	assert.Equal(t, instanceRef.Name, rel.Object.Name)

	// Verify predicate
	assert.NotNil(t, rel.Predicate)
}

func TestCreateSystemNodeRelationships(t *testing.T) {
	instanceRef := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.agent.v1.Instance",
		Name:    "test-instance",
	}

	systemNodeID := "test-machine-id"

	rels, err := createSystemNodeRelationships(instanceRef, systemNodeID)
	require.NoError(t, err)
	require.Len(t, rels, 2, "Should create bidirectional relationships")

	// Find forward and inverse relationships
	var containsRel, containedByRel *resourcev1.Relationship
	for _, rel := range rels {
		if rel.Type.Type == "antimetal.runtime.v1.Contains" {
			containsRel = rel
		} else if rel.Type.Type == "antimetal.runtime.v1.ContainedBy" {
			containedByRel = rel
		}
	}

	require.NotNil(t, containsRel, "Should have Contains relationship")
	require.NotNil(t, containedByRel, "Should have ContainedBy relationship")

	// Verify Contains: SystemNode → Instance
	assert.Equal(t, "antimetal.hardware.v1.SystemNode", containsRel.Subject.TypeUrl)
	assert.Equal(t, systemNodeID, containsRel.Subject.Name)
	assert.Equal(t, instanceRef.TypeUrl, containsRel.Object.TypeUrl)
	assert.Equal(t, instanceRef.Name, containsRel.Object.Name)

	// Verify ContainedBy: Instance → SystemNode
	assert.Equal(t, instanceRef.TypeUrl, containedByRel.Subject.TypeUrl)
	assert.Equal(t, instanceRef.Name, containedByRel.Subject.Name)
	assert.Equal(t, "antimetal.hardware.v1.SystemNode", containedByRel.Object.TypeUrl)
	assert.Equal(t, systemNodeID, containedByRel.Object.Name)
}

func TestGetSystemNodeID(t *testing.T) {
	// This test depends on the host having a machine ID
	// On systems without one, it should return an error
	id, err := getSystemNodeID()

	if err != nil {
		t.Skip("System does not have a machine ID available")
	}

	assert.NotEmpty(t, id, "Machine ID should not be empty when available")
}

// mockStore is a minimal resource store implementation for testing
type mockStore struct {
	resources     []*resourcev1.Resource
	relationships []*resourcev1.Relationship
}

func (m *mockStore) GetResource(ref *resourcev1.ResourceRef) (*resourcev1.Resource, error) {
	return nil, nil
}

func (m *mockStore) AddResource(rsrc *resourcev1.Resource) error {
	m.resources = append(m.resources, rsrc)
	return nil
}

func (m *mockStore) UpdateResource(rsrc *resourcev1.Resource) error {
	m.resources = append(m.resources, rsrc)
	return nil
}

func (m *mockStore) DeleteResource(ref *resourcev1.ResourceRef) error {
	return nil
}

func (m *mockStore) GetRelationships(subject, object *resourcev1.ResourceRef, predicateT proto.Message) ([]*resourcev1.Relationship, error) {
	return nil, nil
}

func (m *mockStore) AddRelationships(rels ...*resourcev1.Relationship) error {
	m.relationships = append(m.relationships, rels...)
	return nil
}

func (m *mockStore) DeleteRelationships(rels ...*resourcev1.Relationship) error {
	return nil
}

func (m *mockStore) Subscribe(typeDefs ...*resourcev1.TypeDescriptor) <-chan resource.Event {
	ch := make(chan resource.Event)
	close(ch)
	return ch
}

func (m *mockStore) Close() error {
	return nil
}

func TestPublishInstance(t *testing.T) {
	store := &mockStore{}
	logger := logr.Discard()
	ctx := context.Background()

	err := PublishInstance(ctx, store, logger)
	require.NoError(t, err)

	// Verify resource was published
	require.Len(t, store.resources, 1, "Should publish one Instance resource")

	// Verify resource type
	rsrc := store.resources[0]
	assert.Equal(t, "antimetal.agent.v1.Instance", rsrc.Type.Type)

	// Relationships depend on environment (K8s vs non-K8s, machine ID availability)
	// Just verify they're valid if present
	for _, rel := range store.relationships {
		assert.NotNil(t, rel.Type)
		assert.NotNil(t, rel.Subject)
		assert.NotNil(t, rel.Object)
		assert.NotNil(t, rel.Predicate)
	}
}

func TestPublishInstance_NilStore(t *testing.T) {
	logger := logr.Discard()
	ctx := context.Background()

	err := PublishInstance(ctx, nil, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "store is required")
}
