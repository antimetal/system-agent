// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package hardwaregraph

import (
	"testing"

	hardwarev1 "github.com/antimetal/agent/pkg/api/antimetal/hardware/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Phase 1: Basic Functionality Tests

func TestCreateNUMADistanceRelationship_BasicCreation(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	sourceNode := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "test-system-numa-node-0",
	}

	targetNode := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "test-system-numa-node-1",
	}

	rel, err := builder.createNUMADistanceRelationship(sourceNode, targetNode, 0, 1, 20)

	require.NoError(t, err, "Should create NUMA distance relationship successfully")
	require.NotNil(t, rel)

	// Verify relationship structure
	assert.Equal(t, sourceNode, rel.Subject, "Subject should be source node")
	assert.Equal(t, targetNode, rel.Object, "Object should be target node")
	assert.NotNil(t, rel.Type, "Type descriptor should be set")
	assert.NotNil(t, rel.Predicate, "Predicate should be set")
}

func TestCreateNUMADistanceRelationship_PredicateMarshaling(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	sourceNode := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "test-system-numa-node-0",
	}

	targetNode := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "test-system-numa-node-1",
	}

	distance := int32(20)
	targetNodeID := int32(1)

	rel, err := builder.createNUMADistanceRelationship(sourceNode, targetNode, 0, targetNodeID, distance)
	require.NoError(t, err)

	// Unmarshal and verify predicate
	var numaAffinity hardwarev1.NUMAAffinity
	err = rel.Predicate.UnmarshalTo(&numaAffinity)
	require.NoError(t, err, "Should unmarshal predicate successfully")

	assert.Equal(t, targetNodeID, numaAffinity.NodeId, "NodeId should match target node ID")
	assert.Equal(t, distance, numaAffinity.Distance, "Distance should be preserved")
}

func TestCreateNUMADistanceRelationship_TypeDescriptor(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	sourceNode := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "test-system-numa-node-0",
	}

	targetNode := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "test-system-numa-node-1",
	}

	rel, err := builder.createNUMADistanceRelationship(sourceNode, targetNode, 0, 1, 20)
	require.NoError(t, err)

	// Verify type descriptor
	assert.Equal(t, kindRelationship, rel.Type.Kind, "Kind should be relationship")
	assert.Contains(t, rel.Type.Type, "NUMAAffinity", "Type should reference NUMAAffinity")
}

// Phase 2: Distance Value Tests

func TestCreateNUMADistanceRelationship_DistanceValues(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name        string
		distance    int32
		description string
	}{
		{"local node", 10, "Typical local/self distance"},
		{"remote node same socket", 20, "Typical remote node on same socket"},
		{"remote node different socket", 40, "Remote node on different socket"},
		{"zero distance", 0, "Edge case: zero distance"},
		{"maximum typical distance", 255, "Edge case: very large distance"},
		{"negative distance", -1, "Edge case: negative distance (invalid but should not panic)"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sourceNode := &resourcev1.ResourceRef{
				TypeUrl: "antimetal.hardware.v1.NUMANode",
				Name:    "numa-0",
			}

			targetNode := &resourcev1.ResourceRef{
				TypeUrl: "antimetal.hardware.v1.NUMANode",
				Name:    "numa-1",
			}

			rel, err := builder.createNUMADistanceRelationship(sourceNode, targetNode, 0, 1, tc.distance)

			require.NoError(t, err, "Should create relationship for distance %d", tc.distance)

			// Verify distance is preserved
			var numaAffinity hardwarev1.NUMAAffinity
			err = rel.Predicate.UnmarshalTo(&numaAffinity)
			require.NoError(t, err)
			assert.Equal(t, tc.distance, numaAffinity.Distance, tc.description)
		})
	}
}

// Phase 2: Validation & Error Handling

func TestCreateNUMADistanceRelationship_NilReferences(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	validRef := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "numa-0",
	}

	t.Run("nil source node", func(t *testing.T) {
		rel, err := builder.createNUMADistanceRelationship(nil, validRef, 0, 1, 20)
		// Should not panic, may return relationship with nil subject
		assert.NotPanics(t, func() {
			_, _ = builder.createNUMADistanceRelationship(nil, validRef, 0, 1, 20)
		})
		_ = rel
		_ = err
	})

	t.Run("nil target node", func(t *testing.T) {
		rel, err := builder.createNUMADistanceRelationship(validRef, nil, 0, 1, 20)
		// Should not panic, may return relationship with nil object
		assert.NotPanics(t, func() {
			_, _ = builder.createNUMADistanceRelationship(validRef, nil, 0, 1, 20)
		})
		_ = rel
		_ = err
	})

	t.Run("both nil", func(t *testing.T) {
		assert.NotPanics(t, func() {
			_, _ = builder.createNUMADistanceRelationship(nil, nil, 0, 1, 20)
		})
	})
}

func TestCreateNUMADistanceRelationship_SelfReferential(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	node := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "numa-0",
	}

	// Self-referential relationship (same node, typically distance=10)
	rel, err := builder.createNUMADistanceRelationship(node, node, 0, 0, 10)

	require.NoError(t, err, "Should create self-referential relationship")
	assert.Equal(t, node, rel.Subject)
	assert.Equal(t, node, rel.Object)

	var numaAffinity hardwarev1.NUMAAffinity
	err = rel.Predicate.UnmarshalTo(&numaAffinity)
	require.NoError(t, err)
	assert.Equal(t, int32(10), numaAffinity.Distance, "Local distance typically 10")
}

// Phase 3: Real-World NUMA Topology Scenarios

func TestCreateNUMADistanceRelationship_DualSocketTopology(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	// Typical dual-socket server NUMA distance matrix:
	// Node  0   1
	// 0     10  20
	// 1     20  10

	nodes := []*resourcev1.ResourceRef{
		{TypeUrl: "antimetal.hardware.v1.NUMANode", Name: "numa-0"},
		{TypeUrl: "antimetal.hardware.v1.NUMANode", Name: "numa-1"},
	}

	testCases := []struct {
		fromNodeID int32
		toNodeID   int32
		distance   int32
	}{
		{0, 0, 10}, // Node 0 to itself
		{0, 1, 20}, // Node 0 to Node 1
		{1, 0, 20}, // Node 1 to Node 0
		{1, 1, 10}, // Node 1 to itself
	}

	for _, tc := range testCases {
		t.Run(string(rune('0'+tc.fromNodeID))+"->"+string(rune('0'+tc.toNodeID)), func(t *testing.T) {
			rel, err := builder.createNUMADistanceRelationship(
				nodes[tc.fromNodeID],
				nodes[tc.toNodeID],
				tc.fromNodeID,
				tc.toNodeID,
				tc.distance,
			)

			require.NoError(t, err)

			var numaAffinity hardwarev1.NUMAAffinity
			err = rel.Predicate.UnmarshalTo(&numaAffinity)
			require.NoError(t, err)

			assert.Equal(t, tc.toNodeID, numaAffinity.NodeId)
			assert.Equal(t, tc.distance, numaAffinity.Distance)
		})
	}
}

func TestCreateNUMADistanceRelationship_QuadSocketTopology(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	// Typical quad-socket server NUMA distance matrix:
	// Node  0   1   2   3
	// 0     10  20  20  40
	// 1     20  10  40  20
	// 2     20  40  10  20
	// 3     40  20  20  10

	nodes := []*resourcev1.ResourceRef{
		{TypeUrl: "antimetal.hardware.v1.NUMANode", Name: "numa-0"},
		{TypeUrl: "antimetal.hardware.v1.NUMANode", Name: "numa-1"},
		{TypeUrl: "antimetal.hardware.v1.NUMANode", Name: "numa-2"},
		{TypeUrl: "antimetal.hardware.v1.NUMANode", Name: "numa-3"},
	}

	// Distance matrix for quad-socket topology
	distanceMatrix := [][]int32{
		{10, 20, 20, 40}, // Node 0 distances
		{20, 10, 40, 20}, // Node 1 distances
		{20, 40, 10, 20}, // Node 2 distances
		{40, 20, 20, 10}, // Node 3 distances
	}

	for fromID := int32(0); fromID < 4; fromID++ {
		for toID := int32(0); toID < 4; toID++ {
			t.Run(string(rune('0'+fromID))+"->"+string(rune('0'+toID)), func(t *testing.T) {
				rel, err := builder.createNUMADistanceRelationship(
					nodes[fromID],
					nodes[toID],
					fromID,
					toID,
					distanceMatrix[fromID][toID],
				)

				require.NoError(t, err)

				var numaAffinity hardwarev1.NUMAAffinity
				err = rel.Predicate.UnmarshalTo(&numaAffinity)
				require.NoError(t, err)

				assert.Equal(t, toID, numaAffinity.NodeId)
				assert.Equal(t, distanceMatrix[fromID][toID], numaAffinity.Distance)
			})
		}
	}
}

func TestCreateNUMADistanceRelationship_AMDEPYCTopology(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	// AMD EPYC systems often have unique NUMA distances due to chiplet architecture
	// Example: EPYC 7742 with 8 NUMA nodes (4 per socket)

	nodes := make([]*resourcev1.ResourceRef, 4)
	for i := 0; i < 4; i++ {
		nodes[i] = &resourcev1.ResourceRef{
			TypeUrl: "antimetal.hardware.v1.NUMANode",
			Name:    "numa-" + string(rune('0'+i)),
		}
	}

	// AMD EPYC distance pattern (simplified 4-node example)
	// Intra-socket: 10-12, Inter-socket: 32
	testCases := []struct {
		fromID   int32
		toID     int32
		distance int32
	}{
		{0, 0, 10}, // Self
		{0, 1, 12}, // Same socket, different chiplet
		{0, 2, 32}, // Different socket
		{0, 3, 32}, // Different socket
		{1, 2, 32}, // Different socket
		{2, 3, 12}, // Same socket, different chiplet
	}

	for _, tc := range testCases {
		t.Run(string(rune('0'+tc.fromID))+"->"+string(rune('0'+tc.toID)), func(t *testing.T) {
			rel, err := builder.createNUMADistanceRelationship(
				nodes[tc.fromID],
				nodes[tc.toID],
				tc.fromID,
				tc.toID,
				tc.distance,
			)

			require.NoError(t, err)

			var numaAffinity hardwarev1.NUMAAffinity
			err = rel.Predicate.UnmarshalTo(&numaAffinity)
			require.NoError(t, err)

			assert.Equal(t, tc.distance, numaAffinity.Distance,
				"AMD EPYC distance pattern for %d->%d", tc.fromID, tc.toID)
		})
	}
}

func TestCreateNUMADistanceRelationship_IntelXeonTopology(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	// Intel Xeon systems typically have simpler NUMA patterns
	// Dual-socket with uniform cross-socket distances

	nodes := []*resourcev1.ResourceRef{
		{TypeUrl: "antimetal.hardware.v1.NUMANode", Name: "numa-0"},
		{TypeUrl: "antimetal.hardware.v1.NUMANode", Name: "numa-1"},
	}

	testCases := []struct {
		name     string
		fromID   int32
		toID     int32
		distance int32
	}{
		{"node 0 local", 0, 0, 10},
		{"node 0 to node 1", 0, 1, 21},
		{"node 1 to node 0", 1, 0, 21},
		{"node 1 local", 1, 1, 10},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rel, err := builder.createNUMADistanceRelationship(
				nodes[tc.fromID],
				nodes[tc.toID],
				tc.fromID,
				tc.toID,
				tc.distance,
			)

			require.NoError(t, err)

			var numaAffinity hardwarev1.NUMAAffinity
			err = rel.Predicate.UnmarshalTo(&numaAffinity)
			require.NoError(t, err)

			assert.Equal(t, tc.distance, numaAffinity.Distance,
				"Intel Xeon distance for %s", tc.name)
		})
	}
}

// Edge Cases

func TestCreateNUMADistanceRelationship_BidirectionalSymmetry(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	node0 := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "numa-0",
	}

	node1 := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "numa-1",
	}

	// Create both directions
	rel01, err := builder.createNUMADistanceRelationship(node0, node1, 0, 1, 20)
	require.NoError(t, err)

	rel10, err := builder.createNUMADistanceRelationship(node1, node0, 1, 0, 20)
	require.NoError(t, err)

	// Verify both relationships have same distance (symmetric)
	var affinity01, affinity10 hardwarev1.NUMAAffinity

	err = rel01.Predicate.UnmarshalTo(&affinity01)
	require.NoError(t, err)

	err = rel10.Predicate.UnmarshalTo(&affinity10)
	require.NoError(t, err)

	assert.Equal(t, affinity01.Distance, affinity10.Distance,
		"Bidirectional distances should be symmetric")
}

func TestCreateNUMADistanceRelationship_AsymmetricDistances(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	// Some systems may have asymmetric NUMA distances (rare but possible)
	node0 := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "numa-0",
	}

	node1 := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "numa-1",
	}

	// Different distances in each direction
	rel01, err := builder.createNUMADistanceRelationship(node0, node1, 0, 1, 20)
	require.NoError(t, err)

	rel10, err := builder.createNUMADistanceRelationship(node1, node0, 1, 0, 25)
	require.NoError(t, err)

	var affinity01, affinity10 hardwarev1.NUMAAffinity

	err = rel01.Predicate.UnmarshalTo(&affinity01)
	require.NoError(t, err)

	err = rel10.Predicate.UnmarshalTo(&affinity10)
	require.NoError(t, err)

	assert.NotEqual(t, affinity01.Distance, affinity10.Distance,
		"Should support asymmetric distances")
	assert.Equal(t, int32(20), affinity01.Distance)
	assert.Equal(t, int32(25), affinity10.Distance)
}

func TestCreateNUMADistanceRelationship_LargeNodeIDs(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	// Test with large node IDs (some systems have many NUMA nodes)
	node0 := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "numa-0",
	}

	node255 := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "numa-255",
	}

	rel, err := builder.createNUMADistanceRelationship(node0, node255, 0, 255, 100)

	require.NoError(t, err)

	var numaAffinity hardwarev1.NUMAAffinity
	err = rel.Predicate.UnmarshalTo(&numaAffinity)
	require.NoError(t, err)

	assert.Equal(t, int32(255), numaAffinity.NodeId)
	assert.Equal(t, int32(100), numaAffinity.Distance)
}

// Complex Topology Tests

func TestCreateNUMADistanceRelationship_EightSocketTopology(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	// Large 8-socket server (e.g., SGI UV systems, large Intel/AMD servers)
	nodes := make([]*resourcev1.ResourceRef, 8)
	for i := 0; i < 8; i++ {
		nodes[i] = &resourcev1.ResourceRef{
			TypeUrl: "antimetal.hardware.v1.NUMANode",
			Name:    "numa-" + string(rune('0'+i)),
		}
	}

	// Test a subset of relationships from node 0
	testCases := []struct {
		toID     int32
		distance int32
		desc     string
	}{
		{0, 10, "local"},
		{1, 20, "adjacent socket"},
		{2, 20, "adjacent socket"},
		{3, 40, "two hops away"},
		{7, 80, "far socket"},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			rel, err := builder.createNUMADistanceRelationship(
				nodes[0],
				nodes[tc.toID],
				0,
				tc.toID,
				tc.distance,
			)

			require.NoError(t, err)

			var numaAffinity hardwarev1.NUMAAffinity
			err = rel.Predicate.UnmarshalTo(&numaAffinity)
			require.NoError(t, err)

			assert.Equal(t, tc.distance, numaAffinity.Distance, tc.desc)
		})
	}
}

func TestCreateNUMADistanceRelationship_SingleNodeSystem(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	// Single NUMA node system (many cloud instances)
	node0 := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "numa-0",
	}

	// Only self-referential relationship
	rel, err := builder.createNUMADistanceRelationship(node0, node0, 0, 0, 10)

	require.NoError(t, err)

	var numaAffinity hardwarev1.NUMAAffinity
	err = rel.Predicate.UnmarshalTo(&numaAffinity)
	require.NoError(t, err)

	assert.Equal(t, int32(0), numaAffinity.NodeId)
	assert.Equal(t, int32(10), numaAffinity.Distance)
}

func TestCreateNUMADistanceRelationship_CloudInstancePatterns(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name        string
		numNodes    int
		description string
	}{
		{
			name:        "AWS c5.metal (2 NUMA nodes)",
			numNodes:    2,
			description: "AWS bare metal instances with NUMA",
		},
		{
			name:        "GCP n2 (1 NUMA node)",
			numNodes:    1,
			description: "Most GCP instances have single NUMA node",
		},
		{
			name:        "Azure L-series (4 NUMA nodes)",
			numNodes:    4,
			description: "Azure memory-optimized with NUMA",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nodes := make([]*resourcev1.ResourceRef, tc.numNodes)
			for i := 0; i < tc.numNodes; i++ {
				nodes[i] = &resourcev1.ResourceRef{
					TypeUrl: "antimetal.hardware.v1.NUMANode",
					Name:    "numa-" + string(rune('0'+i)),
				}
			}

			// Test local distance for first node
			rel, err := builder.createNUMADistanceRelationship(
				nodes[0],
				nodes[0],
				0,
				0,
				10,
			)

			require.NoError(t, err, "Cloud instance NUMA topology: %s", tc.description)

			var numaAffinity hardwarev1.NUMAAffinity
			err = rel.Predicate.UnmarshalTo(&numaAffinity)
			require.NoError(t, err)
			assert.Equal(t, int32(10), numaAffinity.Distance)
		})
	}
}

// Relationship Property Tests

func TestCreateNUMADistanceRelationship_BidirectionalRelationships(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	node0 := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "numa-0",
	}

	node1 := &resourcev1.ResourceRef{
		TypeUrl: "antimetal.hardware.v1.NUMANode",
		Name:    "numa-1",
	}

	// Create forward relationship
	relForward, err := builder.createNUMADistanceRelationship(node0, node1, 0, 1, 20)
	require.NoError(t, err)

	// Create reverse relationship
	relReverse, err := builder.createNUMADistanceRelationship(node1, node0, 1, 0, 20)
	require.NoError(t, err)

	// Verify they are distinct relationships
	assert.NotEqual(t, relForward.Subject.Name, relReverse.Subject.Name)
	assert.NotEqual(t, relForward.Object.Name, relReverse.Object.Name)

	// But have same distance
	var affinityForward, affinityReverse hardwarev1.NUMAAffinity

	err = relForward.Predicate.UnmarshalTo(&affinityForward)
	require.NoError(t, err)

	err = relReverse.Predicate.UnmarshalTo(&affinityReverse)
	require.NoError(t, err)

	assert.Equal(t, affinityForward.Distance, affinityReverse.Distance)
}
