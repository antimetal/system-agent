// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	badger "github.com/dgraph-io/badger/v4"
	"github.com/go-logr/logr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/antimetal/agent/pkg/errors"
	"github.com/antimetal/agent/internal/resource"
)

const (
	objKeySize = sha256.Size
)

type keyPart = []byte
type indexKey = []byte
type indexVal = []byte
type objKey = []byte

var (
	resourceKey     = keyPart("rsrc")
	relationshipKey = keyPart("rel")
	index           = keyPart("idx")
	subjectIdx      = keyPart("rel-subj")
	objectIdx       = keyPart("rel-obj")
	predicateIdx    = keyPart("rel-predicate")
)

type subscriber struct {
	ch        chan resource.Event
	kindMatch [][]string
	typeMatch [][]string
}

func (s *subscriber) match(typeDef *resourcev1.TypeDescriptor) bool {
	if len(s.kindMatch) == 0 && len(s.typeMatch) == 0 {
		return true
	}
	if typeDef == nil {
		return false
	}

	kindPath := strings.Split(typeDef.GetKind(), ".")
	typePath := strings.Split(typeDef.GetType(), ".")

	if len(s.kindMatch) > 0 {
		kindMatched := false
		for _, kindMatch := range s.kindMatch {
			if hasPrefix(kindPath, kindMatch) {
				kindMatched = true
				break
			}
		}
		if !kindMatched {
			return false
		}
	}

	if len(s.typeMatch) > 0 {
		for _, typeMatch := range s.typeMatch {
			if hasPrefix(typePath, typeMatch) {
				return true
			}
		}
	}

	return false
}

func hasPrefix(path, prefix []string) bool {
	if len(prefix) > len(path) {
		return false
	}
	for i, part := range prefix {
		if path[i] != part {
			return false
		}
	}
	return true
}

// Store is a simple store for resources and their relationships.
// Resources are objects that represent a type of workload running on the system
// or cloud resource (e.g. Kubernetes Pod, AWS EC2 instance, etc).
// Resources are identified by a unique name path.
//
// Resources can also have relationships with other resources. Relationships are
// represented by a RDF triplet with a subject, predicate, and object.
type store struct {
	mu     sync.RWMutex
	wg     sync.WaitGroup
	closed bool

	store           *badger.DB
	opGauge         *atomic.Int32
	eventRouter     chan resource.Event
	stopEventRouter chan struct{}
	subscribers     []*subscriber
}

// New creates a new Store.
func New(dataDir string, log logr.Logger) (*store, error) {
	// Create a debug-level logger for badger
	badgerLog := newBadgerLogger(log.V(1).WithName("badger"))

	db, err := badger.Open(
		badger.DefaultOptions(dataDir).
			WithInMemory(dataDir == "").
			WithNumMemtables(3).
			WithBlockCacheSize(128 << 20).
			WithIndexCacheSize(64 << 20).
			WithLogger(badgerLog), // Use debug-level logger for badger
	)
	if err != nil {
		return nil, err
	}
	s := &store{
		store:           db,
		opGauge:         &atomic.Int32{},
		eventRouter:     make(chan resource.Event),
		stopEventRouter: make(chan struct{}),
		subscribers:     make([]*subscriber, 0),
	}
	go s.startEventRouter()
	return s, nil
}

// AddResource adds rsrc to the inventory located by name and updates rsrc for
// created and updated timestamps.
// If a resource already exists with the same name and namespace, it will return an error.
func (s *store) AddResource(rsrc *resourcev1.Resource) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	s.opGauge.Add(1)
	defer s.opGauge.Add(-1)

	r, err := encodeResourceKey(ref(rsrc))
	if err != nil {
		return fmt.Errorf("failed to encode resource key: %w", err)
	}
	key := buildKey(resourceKey, []byte(r))

	var objAny *anypb.Any
	err = s.store.Update(func(txn *badger.Txn) error {
		_, err := txn.Get(key)
		if err == nil {
			return fmt.Errorf("resource already exists")
		}
		if !errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("failed to read resource: %w", err)
		}
		now := timestamppb.Now()
		rsrc.GetMetadata().CreatedAt = now
		rsrc.GetMetadata().UpdatedAt = now
		objAny, err = anypb.New(rsrc)
		if err != nil {
			return fmt.Errorf("failed to marshal resource: %w", err)
		}

		return txn.Set(key, objAny.GetValue())
	})
	if err != nil {
		return fmt.Errorf("failed to add resource: %w", err)
	}

	// Create a new copy of the Any object.
	// Set explicitly rather than proto.Clone to avoid using reflection.
	s.eventRouter <- resource.Event{
		Type: resource.EventTypeAdd,
		Objs: []*resourcev1.Object{{
			Type: rsrc.GetType(),
			Object: &anypb.Any{
				TypeUrl: objAny.GetTypeUrl(),
				Value:   bytes.Clone(objAny.GetValue()),
			},
		}},
	}
	return nil
}

// UpdateResource updates a resource located by name with rsrc.
// If a resource already exists with the same namespace/name, it will be replaced
// with rsrc and updates rsrc with updated at timestamp. The created at timestamp from the
// originally added resource is preserved. Otherwise a new resource
// will be added and rsrc will be updated for created and updated timestamps.
func (s *store) UpdateResource(rsrc *resourcev1.Resource) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	s.opGauge.Add(1)
	defer s.opGauge.Add(-1)

	r, err := encodeResourceKey(ref(rsrc))
	if err != nil {
		return fmt.Errorf("failed to encode resource key: %w", err)
	}
	key := buildKey(resourceKey, []byte(r))

	var objAny *anypb.Any
	err = s.store.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		// If the resource does not exist, create it
		if errors.Is(err, badger.ErrKeyNotFound) {
			now := timestamppb.Now()
			rsrc.GetMetadata().CreatedAt = now
			rsrc.GetMetadata().UpdatedAt = now
			objAny, err = anypb.New(rsrc)
			if err != nil {
				return fmt.Errorf("failed to marshal resource: %w", err)
			}
			return txn.Set(key, objAny.GetValue())
		}
		if err != nil {
			return fmt.Errorf("failed to read resource: %w", err)
		}
		err = item.Value(func(val []byte) error {
			r := &resourcev1.Resource{}
			err := proto.Unmarshal(val, r)
			if err != nil {
				return fmt.Errorf("failed to unmarshal resource: %w", err)
			}
			rsrc.GetMetadata().CreatedAt = r.Metadata.GetCreatedAt()
			rsrc.GetMetadata().UpdatedAt = timestamppb.Now()
			objAny, err = anypb.New(rsrc)
			if err != nil {
				return fmt.Errorf("failed to marshal resource: %w", err)
			}
			return txn.Set(key, objAny.GetValue())
		})
		if err != nil {
			return fmt.Errorf("failed to update resource: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update resource: %w", err)
	}

	// Create a new copy of the Any object.
	// Set explicitly rather than proto.Clone to avoid using reflection.
	s.eventRouter <- resource.Event{
		Type: resource.EventTypeUpdate,
		Objs: []*resourcev1.Object{{
			Type: rsrc.GetType(),
			Object: &anypb.Any{
				TypeUrl: objAny.GetTypeUrl(),
				Value:   bytes.Clone(objAny.GetValue()),
			},
		}},
	}
	return nil
}

// GetResource returns the resource identified by ref.
// If the resource does not exist, it will return ErrResourceNotFound.
func (s *store) GetResource(ref *resourcev1.ResourceRef) (*resourcev1.Resource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, fmt.Errorf("store is closed")
	}

	s.opGauge.Add(1)
	defer s.opGauge.Add(-1)

	r, err := encodeResourceKey(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to encode resource key: %w", err)
	}
	key := buildKey(resourceKey, []byte(r))
	var val []byte
	err = s.store.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		val, err = item.ValueCopy(val)
		return err
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, resource.ErrResourceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find resource: %w", err)
	}
	rsrc := &resourcev1.Resource{}
	err = proto.Unmarshal(val, rsrc)
	return rsrc, err
}

// DeleteResource deletes the resource identfied by ref.
// It also cascade deletes all relationships where the resource is the subject
// or object.
func (s *store) DeleteResource(ref *resourcev1.ResourceRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	s.opGauge.Add(1)
	defer s.opGauge.Add(-1)

	r, err := encodeResourceKey(ref)
	if err != nil {
		return fmt.Errorf("failed to encode resource key: %w", err)
	}

	err = s.store.Update(func(txn *badger.Txn) error {
		delObjs := make([]objKey, 0)

		// 1. Delete all relationships where resource is the subject
		subjectIdxLookup := buildKey(index, subjectIdx, keyPart(r))
		delSubjectObjs, err := deleteIndexedObjects(txn, subjectIdxLookup)
		if err != nil {
			return fmt.Errorf("failed to delete subject relationships: %w", err)
		}
		delObjs = append(delObjs, delSubjectObjs...)

		// 2. Delete all relationships where resource is the object
		objectIdxLookup := buildKey(index, objectIdx, keyPart(r))
		delObjectObjs, err := deleteIndexedObjects(txn, objectIdxLookup)
		if err != nil {
			return fmt.Errorf("failed to delete object relationships: %w", err)
		}
		delObjs = append(delObjs, delObjectObjs...)

		// 3. Update relationship indexes
		if err := txn.Delete(subjectIdxLookup); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("failed to delete subject relationship index: %w", err)
		}
		if err := txn.Delete(objectIdxLookup); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("failed to delete object relationship index: %w", err)
		}
		// TODO: This is pretty expensive - O(delObjs*numPredicateIndexes)
		// An optimization would be to use bloom filters to check whether the index
		// contains the object. That we can only read the index if we know there's an
		// object there saving us KV lookups
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(buildKey(index, predicateIdx)); it.ValidForPrefix(buildKey(index, predicateIdx)); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				for _, obj := range delObjs {
					if err := deleteObjKeyFromIndex(txn, item.Key(), obj); err != nil {
						return fmt.Errorf("failed to update index: %w", err)
					}
				}
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to update predicate index value: %w", err)
			}
		}
		// 4. Finally delete the actual resource
		return txn.Delete(buildKey(resourceKey, []byte(r)))
	})
	if err != nil {
		return fmt.Errorf("failed to delete resource: %w", err)
	}
	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: string((&resourcev1.Resource{}).ProtoReflect().Descriptor().Name()),
			Type: ref.TypeUrl,
		},
		Metadata: &resourcev1.ResourceMeta{
			Name:      ref.Name,
			Namespace: ref.Namespace,
			DeletedAt: timestamppb.Now(),
		},
	}
	objAny, err := anypb.New(rsrc)
	if err != nil {
		return fmt.Errorf("failed to marshal resource: %w", err)
	}

	// Create a new copy of the Any object.
	// Set explicitly rather than proto.Clone to avoid using reflection.
	s.eventRouter <- resource.Event{
		Type: resource.EventTypeDelete,
		Objs: []*resourcev1.Object{{
			Type: rsrc.GetType(),
			Object: &anypb.Any{
				TypeUrl: objAny.GetTypeUrl(),
				Value:   bytes.Clone(objAny.GetValue()),
			},
		}},
	}
	return nil
}

// AddRelationships adds rels to the inventory.
func (s *store) AddRelationships(rels ...*resourcev1.Relationship) error {
	for _, rel := range rels {
		if rel.GetPredicate() == nil {
			return fmt.Errorf("predicate cannot be nil")
		}

		if reflect.DeepEqual(rel.GetSubject(), rel.GetObject()) {
			return fmt.Errorf(
				"[%s;%s;%s]: subject and object cannot be equal",
				rel.GetSubject(),
				rel.GetPredicate().GetTypeUrl(),
				rel.GetObject(),
			)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	s.opGauge.Add(1)
	defer s.opGauge.Add(-1)

	objs := make([]*resourcev1.Object, len(rels))
	err := s.store.Update(func(txn *badger.Txn) error {
		for i, rel := range rels {
			// 1. Write the relationship object
			objAny, err := anypb.New(rel)
			if err != nil {
				return fmt.Errorf("failed to marshal relationship: %w", err)
			}
			h := sha256.Sum256(objAny.GetValue())
			if err := txn.Set(buildKey(relationshipKey, h[:]), objAny.GetValue()); err != nil {
				return fmt.Errorf("failed to write relationship: %w", err)
			}

			// 2. Update the indexes
			predicate := keyPart(strings.TrimPrefix(rel.Predicate.GetTypeUrl(), "type.googleapis.com/"))
			predicateIdxKey := buildKey(index, predicateIdx, predicate)
			if err := addObjKeyToIndex(txn, predicateIdxKey, h[:]); err != nil {
				return fmt.Errorf("failed to update predicate index: %w", err)
			}

			objectKey, err := encodeResourceKey(rel.GetObject())
			if err != nil {
				return fmt.Errorf("failed to encode object key: %w", err)
			}
			objectIdxKey := buildKey(index, objectIdx, []byte(objectKey))
			if err := addObjKeyToIndex(txn, objectIdxKey, h[:]); err != nil {
				return fmt.Errorf("failed to update object index: %w", err)
			}

			subjectKey, err := encodeResourceKey(rel.GetSubject())
			if err != nil {
				return fmt.Errorf("failed to encode subject key: %w", err)
			}
			subjectIdxKey := buildKey(index, subjectIdx, []byte(subjectKey))
			if err := addObjKeyToIndex(txn, subjectIdxKey, h[:]); err != nil {
				return fmt.Errorf("failed to update subject index: %w", err)
			}

			// Create a new copy of the Any object.
			// Set explicitly rather than proto.Clone to avoid using reflection.
			objs[i] = &resourcev1.Object{
				Type: rel.GetType(),
				Object: &anypb.Any{
					TypeUrl: objAny.GetTypeUrl(),
					Value:   bytes.Clone(objAny.GetValue()),
				},
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to add relationships: %w", err)
	}

	// send objects individually so that it can be filtered downstream
	for _, obj := range objs {
		s.eventRouter <- resource.Event{
			Type: resource.EventTypeAdd,
			Objs: []*resourcev1.Object{obj},
		}
	}
	return nil
}

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
//     returns all relationships where subject is "foo".
//
//   - GetRelationships(nil, nil, &ConnectedTo{}) returns all relationships where predicate
//     has a protobuf message type of ConnectedTo between any subject and object.
//
//   - GetRelationships(
//     &resourcev1.ResourceRef{TypeUrl: "type", Name: "foo"},
//     &resourcev1.ResourceRef{TypeUrl: "type", Name: "bar"},
//     &ConnectedTo{})
//     returns all ConnectedTo relationships between subject "foo" and object "bar".
func (s *store) GetRelationships(subject, object *resourcev1.ResourceRef, predicateT proto.Message) ([]*resourcev1.Relationship, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, fmt.Errorf("store is closed")
	}

	s.opGauge.Add(1)
	defer s.opGauge.Add(-1)

	var rels []*resourcev1.Relationship

	err := s.store.View(func(txn *badger.Txn) error {
		// 1. Decide which indexes to use
		indexes := make([]indexKey, 0)
		if subject != nil {
			subjectKey, err := encodeResourceKey(subject)
			if err != nil {
				return fmt.Errorf("failed to encode subject key: %w", err)
			}
			indexes = append(indexes, buildKey(index, subjectIdx, keyPart(subjectKey)))
		}
		if object != nil {
			objectKey, err := encodeResourceKey(object)
			if err != nil {
				return fmt.Errorf("failed to encode object key: %w", err)
			}
			indexes = append(indexes, buildKey(index, objectIdx, keyPart(objectKey)))
		}
		if predicateT != nil {
			predicate := []byte(predicateT.ProtoReflect().Descriptor().FullName())
			indexes = append(indexes, buildKey(index, predicateIdx, predicate))
		}
		if len(indexes) == 0 {
			return resource.ErrRelationshipsNotFound
		}

		// 2. Read the objects keys from the index
		objs, err := readObjKeysFromIndexes(txn, indexes...)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return resource.ErrRelationshipsNotFound
			}
			return fmt.Errorf("failed to read indexed objects: %w", err)
		}

		// 3. Get the relationships objects
		for _, obj := range objs {
			item, err := txn.Get(buildKey(relationshipKey, obj[:]))
			if err != nil {
				return fmt.Errorf("failed to get relationship %x: %w", obj, err)
			}
			rel := &resourcev1.Relationship{}
			err = item.Value(func(val []byte) error {
				return proto.Unmarshal(val, rel)
			})
			if err != nil {
				return fmt.Errorf("failed to unmarshal relationship %x: %w", obj, err)
			}
			rels = append(rels, rel)
		}
		return nil
	})

	if len(rels) == 0 {
		return nil, resource.ErrRelationshipsNotFound
	}

	return rels, err
}

// Subscribe returns a channel that will emit events on resource changes. An Event contains both
// the event type (add, update delete) etc. and a list of Objects. The Object values are protobuf
// clones of the original so they can be modified without modifiying the underlying resource.
//
// The returned channel will be closed when Close() is called. If Close() has already been called,
// then it will return a closed channel.
func (s *store) Subscribe(typeDefs ...*resourcev1.TypeDescriptor) <-chan resource.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan resource.Event, 1000)
	if s.closed {
		close(ch)
		return ch
	}

	var typeMatch [][]string
	var kindMatch [][]string
	for _, typeDef := range typeDefs {
		if typeDef != nil && typeDef.GetKind() != "" {
			kindMatch = append(kindMatch, strings.Split(typeDef.GetKind(), "."))
		}
		if typeDef != nil && typeDef.GetType() != "" {
			typeMatch = append(typeMatch, strings.Split(typeDef.GetType(), "."))
		}
	}

	subscriber := &subscriber{
		ch:        ch,
		typeMatch: typeMatch,
		kindMatch: kindMatch,
	}
	s.subscribers = append(s.subscribers, subscriber)
	go s.sendInitialObjects(subscriber)
	return ch
}

func (s *store) sendInitialObjects(subscriber *subscriber) {
	objs := make([]*resourcev1.Object, 0)
	_ = s.store.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(buildKey(resourceKey)); it.ValidForPrefix(buildKey(resourceKey)); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				r := &resourcev1.Resource{}
				err := proto.Unmarshal(val, r)
				if err != nil {
					return fmt.Errorf("failed to unmarshal resource: %w", err)
				}
				if subscriber.match(r.GetType()) {
					objs = append(objs, &resourcev1.Object{
						Type: r.GetType(),
						Object: &anypb.Any{
							TypeUrl: fmt.Sprintf("%s/%s", "type.googleapis.com", r.GetType().GetType()),
							Value:   val,
						},
					})
				}
				return nil
			})
			if err != nil {
				continue
			}
		}
		for it.Seek(buildKey(relationshipKey)); it.ValidForPrefix(buildKey(relationshipKey)); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				rel := &resourcev1.Relationship{}
				err := proto.Unmarshal(val, rel)
				if err != nil {
					return fmt.Errorf("failed to unmarshal relationship: %w", err)
				}
				if subscriber.match(rel.GetType()) {
					objs = append(objs, &resourcev1.Object{
						Type:   rel.GetType(),
						Object: &anypb.Any{Value: val},
					})
				}
				return nil
			})
			if err != nil {
				continue
			}
		}
		return nil
	})
	if len(objs) > 0 {
		subscriber.ch <- resource.Event{
			Type: resource.EventTypeAdd,
			Objs: objs,
		}
	}
}

// Close closes the inventory store.
// It is idempotent - calling Close multiple times will close only once.
func (s *store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	close(s.stopEventRouter)
	s.wg.Wait()
	err := s.store.Close()
	s.closed = true
	return err
}

// Start implements the controller-runtime.Manager Runnable interface.
// It blocks until ctx is done, at which point it will close the store in order
// to clean up subscriptions.
func (s *store) Start(ctx context.Context) error {
	<-ctx.Done()
	return s.Close()
}

func (s *store) startEventRouter() {
	s.wg.Add(1)
	defer s.wg.Done()

	for {
		select {
		case e := <-s.eventRouter:
			if len(e.Objs) == 0 {
				continue
			}
			for _, subscriber := range s.subscribers {
				if subscriber.match(e.Objs[0].GetType()) {
					subscriber.ch <- e
				}
			}
		case <-s.stopEventRouter:
			for {
				if s.opGauge.Load() == 0 {
					close(s.eventRouter)
					break
				}
			}
			for _, subscriber := range s.subscribers {
				close(subscriber.ch)
			}
			return
		}
	}
}

func buildKey(parts ...keyPart) []byte {
	b := bytes.Buffer{}
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		b.WriteByte('/')
		b.Write(p)
	}
	return b.Bytes()
}

func splitObjects(b indexVal) []objKey {
	if len(b) == 0 {
		return nil
	}

	// This assumes that len(b) % chunkSize == 0
	numChunks := len(b) / objKeySize
	chunks := make([]objKey, 0, numChunks)

	for i := 0; i < len(b); i += objKeySize {
		chunks = append(chunks, objKey(b[i:i+objKeySize]))
	}
	return chunks
}

func numObjects(b []byte) int {
	return len(b) / objKeySize
}

func addObjKeyToIndex(txn *badger.Txn, key indexKey, value objKey) error {
	item, err := txn.Get(key)
	if err != nil {
		if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		return txn.Set(key, value[:])
	}
	return item.Value(func(val []byte) error {
		val = append(val, value[:]...)
		objs := splitObjects(val)
		slices.SortFunc(objs, func(a, b objKey) int {
			return bytes.Compare(a[:], b[:])
		})
		val = bytes.Join(objs, []byte(""))
		return txn.Set(key, val)
	})
}

func deleteObjKeyFromIndex(txn *badger.Txn, key indexKey, value []byte) error {
	item, err := txn.Get(key)
	if err != nil {
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		return err
	}
	return item.Value(func(val []byte) error {
		// use binary search to find and remove the objKey
		n := numObjects(val)
		l, h := 0, n
		for l < h {
			m := (l + h) >> 1 // avoids overflow
			r := bytes.Compare(val[m*objKeySize:(m+1)*objKeySize], value)
			if r < 0 {
				l = m + 1
			} else if r > 0 {
				h = m - 1
			} else {
				// found the objKey, remove it
				newVal := append(val[:m*objKeySize], val[(m+1)*objKeySize:]...)
				return txn.Set(key, newVal)
			}
		}
		return nil
	})
}

func readObjKeysFromIndexes(txn *badger.Txn, indexes ...indexKey) ([]objKey, error) {
	if len(indexes) == 0 {
		return nil, nil
	}

	// fast path if there is only one index
	if len(indexes) == 1 {
		item, err := txn.Get(indexes[0])
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		var objs [][]byte
		err = item.Value(func(val []byte) error {
			objs = splitObjects(val)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return objs, nil
	}

	// read the objects from the indexes
	indexValues := make([][]byte, len(indexes))
	for i, idx := range indexes {
		item, err := txn.Get(idx)
		if err != nil {
			return nil, err
		}
		err = item.Value(func(val []byte) error {
			indexValues[i] = val
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// find the intersection of the objects from the indexes
	return intersectIndexes(indexValues...), nil
}

func deleteIndexedObjects(txn *badger.Txn, idxPrefix []byte) ([]objKey, error) {
	objs, err := readObjKeysFromIndexes(txn, idxPrefix)
	if err != nil {
		return nil, err
	}
	for _, obj := range objs {
		if err := txn.Delete(buildKey(relationshipKey, obj[:])); err != nil {
			return nil, fmt.Errorf("failed to delete relationship %x: %w", obj, err)
		}
	}
	return objs, nil
}

func intersectIndexes(indexVals ...indexVal) []objKey {
	if len(indexVals) == 0 {
		return nil
	}

	type e = [objKeySize]byte
	type set = map[e]struct{}
	convertToSet := func(indexval []byte) set {
		s := make(set)
		for i := 0; i < len(indexval); i += objKeySize {
			s[e(indexval[i:i+objKeySize])] = struct{}{}
		}
		return s
	}
	cmp := convertToSet(indexVals[0])

	for _, idxVal := range indexVals[1:] {
		s := convertToSet(idxVal)
		for k := range cmp {
			if _, ok := s[k]; !ok {
				delete(cmp, k)
			}
		}
	}

	idxObjs := make([]objKey, 0, len(cmp))
	for k := range cmp {
		idxObjs = append(idxObjs, k[:])
	}
	return idxObjs
}
