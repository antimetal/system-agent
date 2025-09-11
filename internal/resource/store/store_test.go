// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !integration

package store

import (
	"fmt"
	"testing"
	"time"

	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/errors"
	"github.com/antimetal/agent/internal/resource"
	"github.com/go-logr/logr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestStore_AddResource(t *testing.T) {
	inv, err := New("", logr.Discard())
	if err != nil {
		t.Fatalf("failed to create inventory: %v", err)
	}
	defer inv.Close()

	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Type: "foo",
		},
		Metadata: &resourcev1.ResourceMeta{
			Name: "test",
		},
	}

	if err := inv.AddResource(rsrc); err != nil {
		t.Fatalf("failed to add resource: %v", err)
	}

	r, err := inv.GetResource(ref(rsrc))
	if err != nil {
		t.Fatalf("failed to get resource: %v", err)
	}

	if r.Metadata.Name != rsrc.Metadata.Name {
		t.Fatalf("expected name %q, got %q", rsrc.Metadata.Name, r.Metadata.Name)
	}
	if r.Type.Type != rsrc.Type.Type {
		t.Fatalf("expected type %q, got %q", rsrc.Type.Type, r.Type.Type)
	}
	if r.Metadata.CreatedAt == nil {
		t.Fatalf("expected creation time to be set")
	}
	if r.Metadata.UpdatedAt == nil {
		t.Fatalf("expected update time to be set")
	}

	_, err = inv.GetResource(&resourcev1.ResourceRef{TypeUrl: "notexist", Name: "notexist"})
	if !errors.Is(err, resource.ErrResourceNotFound) {
		t.Fatalf("expected error %v, got %v", resource.ErrResourceNotFound, err)
	}
}

func TestStore_UpdateResourceNewResource(t *testing.T) {
	inv, err := New("", logr.Discard())
	if err != nil {
		t.Fatalf("failed to create inventory: %v", err)
	}
	defer inv.Close()

	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Type: "foo",
		},
		Metadata: &resourcev1.ResourceMeta{
			Name: "test",
		},
	}

	if err := inv.UpdateResource(rsrc); err != nil {
		t.Fatalf("failed to add resource: %v", err)
	}

	r, err := inv.GetResource(ref(rsrc))
	if err != nil {
		t.Fatalf("failed to get resource: %v", err)
	}

	if r.Metadata.Name != rsrc.Metadata.Name {
		t.Fatalf("expected name %q, got %q", rsrc.Metadata.Name, r.Metadata.Name)
	}
	if r.Type.Type != rsrc.Type.Type {
		t.Fatalf("expected type %q, got %q", rsrc.Type.Type, r.Type.Type)
	}
	if r.Metadata.CreatedAt == nil {
		t.Fatalf("expected creation time to be set")
	}
	if r.Metadata.UpdatedAt == nil {
		t.Fatalf("expected update time to be set")
	}

	_, err = inv.GetResource(&resourcev1.ResourceRef{TypeUrl: "notexist", Name: "notexist"})
	if !errors.Is(err, resource.ErrResourceNotFound) {
		t.Fatalf("expected error %v, got %v", resource.ErrResourceNotFound, err)
	}
}

func TestStore_UpdateResource(t *testing.T) {
	inv, err := New("", logr.Discard())
	if err != nil {
		t.Fatalf("failed to create inventory: %v", err)
	}
	defer inv.Close()

	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Type: "foo",
		},
		Metadata: &resourcev1.ResourceMeta{
			Name: "test",
		},
	}

	if err := inv.AddResource(rsrc); err != nil {
		t.Fatalf("failed to add resource: %v", err)
	}

	r, err := inv.GetResource(ref(rsrc))
	if err != nil {
		t.Fatalf("failed to get resource: %v", err)
	}

	r2 := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Type: "foo",
		},
		Metadata: &resourcev1.ResourceMeta{
			Name:   "test",
			Region: "us-east-1",
		},
	}
	if err := inv.UpdateResource(r2); err != nil {
		t.Fatalf("failed to update resource: %v", err)
	}

	if !r2.Metadata.UpdatedAt.AsTime().After(r.Metadata.UpdatedAt.AsTime()) {
		t.Fatalf("expected update time to be updated: r: %v, r2: %v",
			r.Metadata.UpdatedAt.AsTime(), r2.Metadata.UpdatedAt.AsTime(),
		)
	}

	r3, err := inv.GetResource(ref(rsrc))
	if err != nil {
		t.Fatalf("failed to get resource after update: %v", err)
	}
	if r3.Metadata.Name != rsrc.Metadata.Name {
		t.Fatalf("expected name %q, got %q", rsrc.Metadata.Name, r3.Metadata.Name)
	}
	if r3.Type.Type != rsrc.Type.Type {
		t.Fatalf("expected type %q, got %q", rsrc.Type.Type, r3.Type.Type)
	}
	if r3.Metadata.Region != "us-east-1" {
		t.Fatalf("expected region %q, got %q", "us-east-1", r3.Metadata.Region)
	}
	if r3.Metadata.UpdatedAt.AsTime().After(r2.Metadata.UpdatedAt.AsTime()) {
		t.Fatalf("expected update time to be updated: r3: %v, r2: %v",
			r3.Metadata.UpdatedAt.AsTime(), r2.Metadata.UpdatedAt.AsTime(),
		)
	}
}

func TestStore_GetRelationships(t *testing.T) {
	type testCase struct {
		name              string
		subject           *resourcev1.ResourceRef
		object            *resourcev1.ResourceRef
		predicate         proto.Message
		expectedNumResult int
	}

	inv, err := New("", logr.Discard())
	if err != nil {
		t.Fatalf("failed to create inventory: %v", err)
	}
	defer inv.Close()

	rsrcs := []*resourcev1.Resource{
		{
			Type: &resourcev1.TypeDescriptor{
				Type: "foo",
			},
			Metadata: &resourcev1.ResourceMeta{
				Name: "test",
			},
		},
		{
			Type: &resourcev1.TypeDescriptor{
				Type: "bar",
			},
			Metadata: &resourcev1.ResourceMeta{
				Name: "test2",
			},
		},
	}
	for _, rsrc := range rsrcs {
		if err := inv.AddResource(rsrc); err != nil {
			t.Fatalf("failed to add resource: %v", err)
		}
	}

	predicate, err := anypb.New(&resourcev1.Resource{})
	if err != nil {
		t.Fatalf("failed to create predicate: %v", err)
	}
	predicate2, err := anypb.New(&resourcev1.Relationship{})
	if err != nil {
		t.Fatalf("failed to create predicate 2: %v", err)
	}

	rels := []*resourcev1.Relationship{
		{
			Subject:   &resourcev1.ResourceRef{TypeUrl: "bar", Name: "test"},
			Object:    &resourcev1.ResourceRef{TypeUrl: "baz", Name: "test2"},
			Predicate: predicate,
		},
		{
			Subject:   &resourcev1.ResourceRef{TypeUrl: "baz", Name: "test2"},
			Object:    &resourcev1.ResourceRef{TypeUrl: "bar", Name: "test"},
			Predicate: predicate2,
		},
		{
			Subject:   &resourcev1.ResourceRef{TypeUrl: "bar", Name: "test"},
			Object:    &resourcev1.ResourceRef{TypeUrl: "baz", Name: "test2"},
			Predicate: predicate2,
		},
		{
			Subject:   &resourcev1.ResourceRef{TypeUrl: "baz", Name: "test2"},
			Object:    &resourcev1.ResourceRef{TypeUrl: "qux", Name: "test3"},
			Predicate: predicate,
		},
	}
	if err := inv.AddRelationships(rels...); err != nil {
		t.Fatalf("failed to add relationships: %v", err)
	}

	testCases := []testCase{
		{
			name: "empty",
			subject: &resourcev1.ResourceRef{
				TypeUrl: "notexist",
				Name:    "notexist",
			},
			object: &resourcev1.ResourceRef{
				TypeUrl: "bar",
				Name:    "test",
			},
			predicate:         &resourcev1.Resource{},
			expectedNumResult: 0,
		},
		{
			name: "subject",
			subject: &resourcev1.ResourceRef{
				TypeUrl: "bar",
				Name:    "test",
			},
			expectedNumResult: 2,
		},
		{
			name: "subject-2",
			subject: &resourcev1.ResourceRef{
				TypeUrl: "baz",
				Name:    "test2",
			},
			expectedNumResult: 2,
		},
		{
			name: "subject-3",
			subject: &resourcev1.ResourceRef{
				TypeUrl: "qux",
				Name:    "test3",
			},
			expectedNumResult: 0,
		},
		{
			name: "object",
			object: &resourcev1.ResourceRef{
				TypeUrl: "baz",
				Name:    "test2",
			},
			expectedNumResult: 2,
		},
		{
			name: "object-2",
			object: &resourcev1.ResourceRef{
				TypeUrl: "qux",
				Name:    "test3",
			},
			expectedNumResult: 1,
		},
		{
			name:              "predicate",
			predicate:         &resourcev1.Resource{},
			expectedNumResult: 2,
		},
		{
			name:              "predicate-2",
			predicate:         &resourcev1.Relationship{},
			expectedNumResult: 2,
		},
		{
			name: "subject-object-predicate",
			subject: &resourcev1.ResourceRef{
				TypeUrl: "bar",
				Name:    "test",
			},
			object: &resourcev1.ResourceRef{
				TypeUrl: "baz",
				Name:    "test2",
			},
			predicate:         &resourcev1.Resource{},
			expectedNumResult: 1,
		},
		{
			name: "subject-object",
			subject: &resourcev1.ResourceRef{
				TypeUrl: "bar",
				Name:    "test",
			},
			object: &resourcev1.ResourceRef{
				TypeUrl: "baz",
				Name:    "test2",
			},
			expectedNumResult: 2,
		},
		{
			name: "subject-object-2",
			subject: &resourcev1.ResourceRef{
				TypeUrl: "baz",
				Name:    "test2",
			},
			object: &resourcev1.ResourceRef{
				TypeUrl: "qux",
				Name:    "test3",
			},
			expectedNumResult: 1,
		},
		{
			name: "subject-predicate",
			subject: &resourcev1.ResourceRef{
				TypeUrl: "baz",
				Name:    "test2",
			},
			predicate:         &resourcev1.Relationship{},
			expectedNumResult: 1,
		},
		{
			name: "object-predicate",
			object: &resourcev1.ResourceRef{
				TypeUrl: "bar",
				Name:    "test",
			},
			predicate:         &resourcev1.Relationship{},
			expectedNumResult: 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rels, err := inv.GetRelationships(tc.subject, tc.object, tc.predicate)
			if err != nil && !errors.Is(err, resource.ErrRelationshipsNotFound) {
				t.Fatalf("failed to get relationships: %v\n", err)
			}

			if tc.expectedNumResult == 0 && !errors.Is(err, resource.ErrRelationshipsNotFound) {
				t.Fatalf("expected error %v, got %v\n", resource.ErrRelationshipsNotFound, err)
			}

			if len(rels) != tc.expectedNumResult {
				t.Fatalf("expected %d relationships, got %d\n%+v", tc.expectedNumResult, len(rels), rels)
			}
		})
	}
}

func TestStore_DeleteResource_CascadeDelete(t *testing.T) {
	inv, err := New("", logr.Discard())
	if err != nil {
		t.Fatalf("failed to create inventory: %v", err)
	}
	defer inv.Close()

	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Type: "test",
		},
		Metadata: &resourcev1.ResourceMeta{
			Name: "foo",
		},
	}
	if err := inv.AddResource(rsrc); err != nil {
		t.Fatalf("failed to add resource: %v", err)
	}

	rels := []*resourcev1.Relationship{
		{
			Subject: &resourcev1.ResourceRef{
				TypeUrl: "test",
				Name:    "foo",
			},
			Object: &resourcev1.ResourceRef{
				TypeUrl: "test",
				Name:    "bar",
			},
			Predicate: &anypb.Any{
				TypeUrl: "foo",
			},
		},
		{
			Subject: &resourcev1.ResourceRef{
				TypeUrl: "test",
				Name:    "bar",
			},
			Object: &resourcev1.ResourceRef{
				TypeUrl: "test",
				Name:    "foo",
			},
			Predicate: &anypb.Any{
				TypeUrl: "bar",
			},
		},
		{
			Subject: &resourcev1.ResourceRef{
				TypeUrl: "test",
				Name:    "bar",
			},
			Object: &resourcev1.ResourceRef{
				TypeUrl: "test",
				Name:    "baz",
			},
			Predicate: &anypb.Any{
				TypeUrl: "baz",
			},
		},
	}
	if err := inv.AddRelationships(rels...); err != nil {
		t.Fatalf("failed to add relationships: %v", err)
	}

	if err := inv.DeleteResource(ref(rsrc)); err != nil {
		t.Fatalf("failed to delete resource: %v", err)
	}

	if rsrc, err := inv.GetResource(ref(rsrc)); !errors.Is(err, resource.ErrResourceNotFound) {
		t.Fatalf("expected error %v, got %v; rsrc: %+v", resource.ErrResourceNotFound, err, rsrc)
	}
	rel, err := inv.GetRelationships(
		&resourcev1.ResourceRef{TypeUrl: "test", Name: "foo"},
		&resourcev1.ResourceRef{TypeUrl: "test", Name: "bar"},
		nil,
	)
	if !errors.Is(err, resource.ErrRelationshipsNotFound) {
		t.Fatalf("expected error %v, got %v; rel: %+v", resource.ErrRelationshipsNotFound, err, rel)
	}
	rel, err = inv.GetRelationships(
		&resourcev1.ResourceRef{TypeUrl: "test", Name: "bar"},
		&resourcev1.ResourceRef{TypeUrl: "test", Name: "foo"},
		nil,
	)
	if !errors.Is(err, resource.ErrRelationshipsNotFound) {
		t.Fatalf("expected error %v, got %v; rel: %+v", resource.ErrRelationshipsNotFound, err, rel)
	}
	_, err = inv.GetRelationships(
		&resourcev1.ResourceRef{TypeUrl: "test", Name: "bar"},
		&resourcev1.ResourceRef{TypeUrl: "test", Name: "baz"},
		nil,
	)
	if err != nil {
		t.Fatalf("expected bar->baz relationship to exist, got %v", err)
	}
}

func TestStore_DeleteResource_NoRelationships(t *testing.T) {
	inv, err := New("", logr.Discard())
	if err != nil {
		t.Fatalf("failed to create inventory: %v", err)
	}
	defer inv.Close()

	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Type: "foo",
		},
		Metadata: &resourcev1.ResourceMeta{
			Name: "foo",
		},
	}
	if err := inv.AddResource(rsrc); err != nil {
		t.Fatalf("failed to add resource: %v", err)
	}

	if err := inv.DeleteResource(ref(rsrc)); err != nil {
		t.Fatalf("failed to delete resource: %v", err)
	}

	if rsrc, err := inv.GetResource(ref(rsrc)); !errors.Is(err, resource.ErrResourceNotFound) {
		t.Fatalf("expected error %v, got %v; rsrc: %+v", resource.ErrResourceNotFound, err, rsrc)
	}
}

func TestStore_Subscribe_PackageLongestPathMatch(t *testing.T) {
	testCases := []struct {
		name               string
		subscribeType      string
		resourceTypes      []string
		expectedMatches    []string
		expectedNonMatches []string
	}{
		{
			name:          "exact match",
			subscribeType: "com.example.services.UserService",
			resourceTypes: []string{
				"com.example.services.UserService",
				"com.example.services.AuthService",
				"com.other.services.UserService",
			},
			expectedMatches:    []string{"com.example.services.UserService"},
			expectedNonMatches: []string{"com.example.services.AuthService", "com.other.services.UserService"},
		},
		{
			name:          "package prefix match",
			subscribeType: "com.example.services",
			resourceTypes: []string{
				"com.example.services.UserService",
				"com.example.services.AuthService",
				"com.example.services.billing.PaymentService",
				"com.example.models.User",
				"com.other.services.SomeService",
			},
			expectedMatches: []string{
				"com.example.services.UserService",
				"com.example.services.AuthService",
				"com.example.services.billing.PaymentService",
			},
			expectedNonMatches: []string{
				"com.example.models.User",
				"com.other.services.SomeService",
			},
		},
		{
			name:          "root package match",
			subscribeType: "com.example",
			resourceTypes: []string{
				"com.example.services.UserService",
				"com.example.models.User",
				"com.example.utils.Helper",
				"com.other.services.SomeService",
				"org.example.Something",
			},
			expectedMatches: []string{
				"com.example.services.UserService",
				"com.example.models.User",
				"com.example.utils.Helper",
			},
			expectedNonMatches: []string{
				"com.other.services.SomeService",
				"org.example.Something",
			},
		},
		{
			name:          "no package separator",
			subscribeType: "SimpleType",
			resourceTypes: []string{
				"SimpleType",
				"SimpleTypeExtended",
				"AnotherType",
			},
			expectedMatches:    []string{"SimpleType"},
			expectedNonMatches: []string{"SimpleTypeExtended", "AnotherType"},
		},
		{
			name:          "empty subscription type matches all",
			subscribeType: "",
			resourceTypes: []string{
				"com.example.services.UserService",
				"SimpleType",
				"org.other.Type",
			},
			expectedMatches:    []string{"com.example.services.UserService", "SimpleType", "org.other.Type"},
			expectedNonMatches: []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := New("", logr.Discard())
			if err != nil {
				t.Fatalf("failed to create store: %v", err)
			}
			defer store.Close()

			resources := make([]*resourcev1.Resource, len(tc.resourceTypes))
			for i, resourceType := range tc.resourceTypes {
				resources[i] = &resourcev1.Resource{
					Type: &resourcev1.TypeDescriptor{
						Kind: "Resource",
						Type: resourceType,
					},
					Metadata: &resourcev1.ResourceMeta{
						Name: fmt.Sprintf("resource-%d", i),
					},
				}
				if err := store.AddResource(resources[i]); err != nil {
					t.Fatalf("failed to add resource %s: %v", resourceType, err)
				}
			}

			var subscribeTypeDef *resourcev1.TypeDescriptor
			if tc.subscribeType != "" {
				subscribeTypeDef = &resourcev1.TypeDescriptor{
					Kind: "Resource",
					Type: tc.subscribeType,
				}
			}

			receivedTypes := make(map[string]struct{})
			eventCh := store.Subscribe(subscribeTypeDef)

			timeout := time.NewTimer(100 * time.Millisecond)
			defer timeout.Stop()

			done := false
			for !done {
				select {
				case event := <-eventCh:
					for _, obj := range event.Objs {
						receivedTypes[obj.GetType().GetType()] = struct{}{}
					}
				case <-timeout.C:
					done = true
				}
			}

			for _, expectedType := range tc.expectedMatches {
				if _, ok := receivedTypes[expectedType]; !ok {
					t.Errorf("expected to receive event for type %s but didn't", expectedType)
				}
			}

			for _, nonExpectedType := range tc.expectedNonMatches {
				if _, ok := receivedTypes[nonExpectedType]; ok {
					t.Errorf("did not expect to receive event for type %s but did", nonExpectedType)
				}
			}
		})
	}
}
