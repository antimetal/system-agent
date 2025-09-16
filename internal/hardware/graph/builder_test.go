// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph_test

import (
	"context"
	"testing"
	"time"

	"github.com/antimetal/agent/internal/hardware/graph"
	"github.com/antimetal/agent/internal/hardware/types"
	"github.com/antimetal/agent/internal/resource"
	"github.com/antimetal/agent/internal/resource/store"
	hardwarev1 "github.com/antimetal/agent/pkg/api/antimetal/hardware/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// testStore wraps the real store and tracks operations for testing
type testStore struct {
	realStore     resource.Store
	resources     []*resourcev1.Resource
	relationships []*resourcev1.Relationship
}

func newTestStore(t *testing.T) *testStore {
	// Create an in-memory store (passing empty string for dataDir makes it in-memory)
	s, err := store.New("", logr.Discard())
	require.NoError(t, err, "Failed to create in-memory store")

	return &testStore{
		realStore:     s,
		resources:     make([]*resourcev1.Resource, 0),
		relationships: make([]*resourcev1.Relationship, 0),
	}
}

func (ts *testStore) AddResource(r *resourcev1.Resource) error {
	ts.resources = append(ts.resources, r)
	return ts.realStore.AddResource(r)
}

func (ts *testStore) AddRelationships(rels ...*resourcev1.Relationship) error {
	ts.relationships = append(ts.relationships, rels...)
	return ts.realStore.AddRelationships(rels...)
}

func (ts *testStore) UpdateResource(r *resourcev1.Resource) error {
	ts.resources = append(ts.resources, r)
	return ts.realStore.UpdateResource(r)
}

func (ts *testStore) DeleteResource(ref *resourcev1.ResourceRef) error {
	return ts.realStore.DeleteResource(ref)
}

func (ts *testStore) GetResource(ref *resourcev1.ResourceRef) (*resourcev1.Resource, error) {
	return ts.realStore.GetResource(ref)
}

func (ts *testStore) GetRelationships(subject, object *resourcev1.ResourceRef, predicate proto.Message) ([]*resourcev1.Relationship, error) {
	return ts.realStore.GetRelationships(subject, object, predicate)
}

func (ts *testStore) Subscribe(typeDefs ...*resourcev1.TypeDescriptor) <-chan resource.Event {
	return ts.realStore.Subscribe(typeDefs...)
}

func (ts *testStore) Close() error {
	return ts.realStore.Close()
}

func TestBuilder_BuildFromSnapshot(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()
	testStore := newTestStore(t)
	defer testStore.Close()

	builder := graph.NewBuilder(logger, testStore)

	// Create a test snapshot with sample data
	snapshot := &types.Snapshot{
		Timestamp: time.Now(),
		CPUInfo: &performance.CPUInfo{
			VendorID:      "GenuineIntel",
			CPUFamily:     6,
			Model:         85,
			ModelName:     "Intel(R) Xeon(R) Platinum 8275CL CPU @ 3.00GHz",
			Stepping:      7,
			Microcode:     "0x500320a",
			CPUMHz:        3599.998,
			CacheSize:     "36608 KB",
			PhysicalCores: 4,
			LogicalCores:  8,
			Cores: []performance.CPUCore{
				{
					Processor:  0,
					PhysicalID: 0,
					CoreID:     0,
					Siblings:   4,
					CPUMHz:     3599.998,
				},
				{
					Processor:  1,
					PhysicalID: 0,
					CoreID:     1,
					Siblings:   4,
					CPUMHz:     3599.998,
				},
			},
		},
		MemoryInfo: &performance.MemoryInfo{
			TotalBytes:             16777216000, // ~16GB
			NUMAEnabled:            true,
			NUMABalancingAvailable: true,
			NUMANodes: []performance.NUMANode{
				{
					NodeID:     0,
					TotalBytes: 16777216000,
					CPUs:       []int32{0, 1, 2, 3, 4, 5, 6, 7},
					Distances:  []int32{10},
				},
			},
		},
		DiskInfo: []*performance.DiskInfo{
			&performance.DiskInfo{
				Device:            "nvme0n1",
				Model:             "Amazon Elastic Block Store",
				Vendor:            "NVMe",
				SizeBytes:         107374182400, // 100GB
				Rotational:        false,
				BlockSize:         512,
				PhysicalBlockSize: 512,
				Scheduler:         "none",
				QueueDepth:        1024,
				Partitions: []performance.PartitionInfo{
					{
						Name:        "nvme0n1p1",
						SizeBytes:   107373133824,
						StartSector: 2048,
					},
				},
			},
		},
		NetworkInfo: []*performance.NetworkInfo{
			&performance.NetworkInfo{
				Interface:  "eth0",
				MACAddress: "02:42:ac:11:00:02",
				Speed:      10000, // 10Gbps
				Duplex:     "full",
				MTU:        1500,
				Driver:     "ena",
				Type:       "ether",
				OperState:  "up",
				Carrier:    true,
			},
		},
	}

	// Build the hardware graph
	err := builder.BuildFromSnapshot(ctx, snapshot)
	require.NoError(t, err)

	// Verify resources were created
	assert.Greater(t, len(testStore.resources), 0, "Should have created resources")
	assert.Greater(t, len(testStore.relationships), 0, "Should have created relationships")

	// Count resource types
	resourceCounts := make(map[string]int)
	for _, r := range testStore.resources {
		resourceCounts[r.Type.Type]++
	}

	// Verify expected resource types
	assert.Equal(t, 1, resourceCounts["antimetal.hardware.v1.SystemNode"], "Should have 1 system node")
	assert.Equal(t, 1, resourceCounts["antimetal.hardware.v1.CPUPackageNode"], "Should have 1 CPU package")
	assert.Equal(t, 2, resourceCounts["antimetal.hardware.v1.CPUCoreNode"], "Should have 2 CPU cores")
	assert.Equal(t, 1, resourceCounts["antimetal.hardware.v1.MemoryModuleNode"], "Should have 1 memory module")
	assert.Equal(t, 1, resourceCounts["antimetal.hardware.v1.NUMANode"], "Should have 1 NUMA node")
	assert.Equal(t, 1, resourceCounts["antimetal.hardware.v1.DiskDeviceNode"], "Should have 1 disk device")
	assert.Equal(t, 1, resourceCounts["antimetal.hardware.v1.DiskPartitionNode"], "Should have 1 disk partition")
	assert.Equal(t, 1, resourceCounts["antimetal.hardware.v1.NetworkInterfaceNode"], "Should have 1 network interface")

	// Verify relationships were created
	relationshipCounts := make(map[string]int)
	for _, r := range testStore.relationships {
		relationshipCounts[r.Type.Type]++
	}

	assert.Greater(t, relationshipCounts["antimetal.hardware.v1.Contains"], 0, "Should have containment relationships")
	assert.Greater(t, relationshipCounts["antimetal.hardware.v1.NUMAAffinity"], 0, "Should have NUMA affinity relationships")

	// Verify specific resource details
	for _, r := range testStore.resources {
		assert.Equal(t, resourcev1.Provider_PROVIDER_ANTIMETAL, r.Metadata.Provider)
		assert.NotEmpty(t, r.Metadata.Name)
		assert.NotNil(t, r.Spec)

		// Verify the spec can be unmarshaled based on type
		switch r.Type.Type {
		case "antimetal.hardware.v1.CPUPackageNode":
			var spec hardwarev1.CPUPackageNode
			err := anypb.UnmarshalTo(r.Spec, &spec, proto.UnmarshalOptions{})
			require.NoError(t, err)
			assert.Equal(t, "GenuineIntel", spec.VendorId)
			assert.Equal(t, int32(2), spec.PhysicalCores)
			assert.Equal(t, int32(2), spec.LogicalCores)
		case "antimetal.hardware.v1.DiskDeviceNode":
			var spec hardwarev1.DiskDeviceNode
			err := anypb.UnmarshalTo(r.Spec, &spec, proto.UnmarshalOptions{})
			require.NoError(t, err)
			assert.Equal(t, "nvme0n1", spec.Device)
			assert.Equal(t, uint64(107374182400), spec.SizeBytes)
			assert.False(t, spec.Rotational)
		case "antimetal.hardware.v1.NetworkInterfaceNode":
			var spec hardwarev1.NetworkInterfaceNode
			err := anypb.UnmarshalTo(r.Spec, &spec, proto.UnmarshalOptions{})
			require.NoError(t, err)
			assert.Equal(t, "eth0", spec.Interface)
			assert.Equal(t, "02:42:ac:11:00:02", spec.MacAddress)
			assert.Equal(t, uint64(10000), spec.Speed)
		}
	}
}

func TestBuilder_BuildFromSnapshot_EmptySnapshot(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()
	testStore := newTestStore(t)
	defer testStore.Close()

	builder := graph.NewBuilder(logger, testStore)

	// Create an empty snapshot
	snapshot := &types.Snapshot{
		Timestamp: time.Now(),
	}

	// Build should succeed even with empty data
	err := builder.BuildFromSnapshot(ctx, snapshot)
	require.NoError(t, err)

	// Should have at least the system node
	assert.Equal(t, 1, len(testStore.resources), "Should have created system node")
	assert.Equal(t, "resource.v1.Resource", testStore.resources[0].Type.Kind)
}

func TestBuilder_BuildFromSnapshot_PartialData(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()
	testStore := newTestStore(t)
	defer testStore.Close()

	builder := graph.NewBuilder(logger, testStore)

	// Create a snapshot with only CPU data
	snapshot := &types.Snapshot{
		Timestamp: time.Now(),
		CPUInfo: &performance.CPUInfo{
			VendorID:      "AuthenticAMD",
			ModelName:     "AMD EPYC 7R32",
			PhysicalCores: 2,
			LogicalCores:  4,
			Cores: []performance.CPUCore{
				{
					Processor:  0,
					PhysicalID: 0,
					CoreID:     0,
					CPUMHz:     2799.998,
				},
				{
					Processor:  1,
					PhysicalID: 0,
					CoreID:     1,
					CPUMHz:     2799.998,
				},
			},
		},
	}

	// Build the hardware graph
	err := builder.BuildFromSnapshot(ctx, snapshot)
	require.NoError(t, err)

	// Verify resources were created
	resourceCounts := make(map[string]int)
	for _, r := range testStore.resources {
		resourceCounts[r.Type.Type]++
	}

	assert.Equal(t, 1, resourceCounts["antimetal.hardware.v1.SystemNode"], "Should have 1 system node")
	assert.Equal(t, 1, resourceCounts["antimetal.hardware.v1.CPUPackageNode"], "Should have 1 CPU package")
	assert.Equal(t, 2, resourceCounts["antimetal.hardware.v1.CPUCoreNode"], "Should have 2 CPU cores")
	assert.Equal(t, 0, resourceCounts["antimetal.hardware.v1.MemoryModuleNode"], "Should have no memory module")
	assert.Equal(t, 0, resourceCounts["antimetal.hardware.v1.DiskDeviceNode"], "Should have no disk devices")
}
