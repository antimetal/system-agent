// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package resource

import (
	"errors"

	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"google.golang.org/protobuf/proto"
)

var (
	ErrResourceNotFound      = errors.New("resource not found")
	ErrRelationshipsNotFound = errors.New("relationships not found")
)

// Store persists Resources and their Relationships. Resources are objects that represent a type
// of workload running on the system or cloud resource (e.g. Kubernetes Pod, AWS EC2 instance, etc).
// Resources are identified by a unique name path.
//
// Resources can also have relationships with other resources.
// Relationships are represented by a RDF triplet with a subject, predicate, and object.
type Store interface {
	// GetResource returns a resource.
	// If the resource does not exist, it will return ErrResourceNotFound.
	GetResource(ref *resourcev1.ResourceRef) (*resourcev1.Resource, error)

	// AddResource adds rsrc to the inventory located by name and updates rsrc for
	// created and updated timestamps.
	// If a resource already exists with the same name and namespace, it will return an error.
	AddResource(rsrc *resourcev1.Resource) error

	// UpdateResource updates a resource located by name with rsrc.
	// If a resource already exists with the same namespace/name, it will be replaced
	// with rsrc and updates rsrc with updated at timestamp. The created at timestamp from the
	// originally added resource is preserved. Otherwise a new resource
	// will be added and rsrc will be updated for created and updated timestamps.
	UpdateResource(rsrc *resourcev1.Resource) error

	// DeleteResource deletes the resource located by name.
	// It also cascade deletes all relationships where the resource is the subject
	// or object.
	DeleteResource(ref *resourcev1.ResourceRef) error

	// GetRelationships returns all relationships that match the combination subject, object,
	// and predicate with the following invariants:
	//
	// subject == nil matches any subject
	//
	// object == nil matches any object
	//
	// predicateT == nil matches any predicate
	//
	// If there are no matching relationships then it will return ErrRelationshipsNotFound.
	//
	// Examples:
	//
	//   - GetRelationships(&resourcev1.ResourceRef{TypeUrl: "type", Name: "foo"}, nil, nil)
	// 		 returns all relationships where subject is "foo".
	//
	//   - GetRelationships(nil, nil, &ConnectedTo{}) returns all relationships where predicate
	//     has a protobuf message type of ConnectedTo between any subject and object.
	//
	//   - GetRelationships(
	// 			&resourcev1.ResourceRef{TypeUrl: "type", Name: "foo"},
	// 			&resourcev1.ResourceRef{TypeUrl: "type", Name: "bar"},
	// 			&ConnectedTo{})
	// 		 returns all ConnectedTo relationships between subject "foo" and object "bar".
	GetRelationships(subject, object *resourcev1.ResourceRef, predicateT proto.Message) ([]*resourcev1.Relationship, error)

	// AddRelationships adds rels to the inventory.
	AddRelationships(rels ...*resourcev1.Relationship) error

	// Subscribe returns a channel that will emit events on resource changes. An Event contains both
	// the event type (add, update delete) etc. and a list of Objects. The Object values are protobuf
	// clones of the original so they can be modified without modifiying the underlying resource.
	//
	// typeDef can filter for object kinds and types. both typeDef.Kind and typeDef.Type filters
	// for longest dot delimitted package name match. e.g. typeDef.Type == antimetal.foo.v1 will
	// match any object in the antimetal.foo.v1 package; antimetal.v1 matches resources in any
	// package in the antimetal.v1 package hierarchy; antimetal.foo.v1.Bar filters for only the
	// Bar resource. Empty strings match any kind or type.
	//
	// The returned channel will be closed when Close() is called. If Close()
	// has already been called, then it will return a closed channel.
	Subscribe(typeDefs ...*resourcev1.TypeDescriptor) <-chan Event

	// Close closes the inventory store.
	// It should be idempotent - calling Close multiple times will close only once.
	Close() error
}

type EventType string

const (
	EventTypeAdd    EventType = "ADD"
	EventTypeUpdate EventType = "UPDATE"
	EventTypeDelete EventType = "DELETE"
)

type Event struct {
	Type EventType
	Objs []*resourcev1.Object
}
