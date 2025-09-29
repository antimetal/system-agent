// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package containergraph

import (
	"context"
	"sync"
	"testing"

	"github.com/antimetal/agent/internal/resource"
	"github.com/antimetal/agent/internal/resource/store"
	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

// mockSnapshot implements RuntimeSnapshot for testing
type mockSnapshot struct {
	containers []ContainerInfo
	processes  []ProcessInfo
}

func (m *mockSnapshot) GetContainers() []ContainerInfo {
	return m.containers
}

func (m *mockSnapshot) GetProcesses() []ProcessInfo {
	return m.processes
}

// mockStore implements a simple in-memory store for testing
type mockStore struct {
	mu            sync.Mutex
	resources     []*resourcev1.Resource
	relationships []*resourcev1.Relationship
}

func newMockStore() *mockStore {
	return &mockStore{
		resources:     make([]*resourcev1.Resource, 0),
		relationships: make([]*resourcev1.Relationship, 0),
	}
}

func (m *mockStore) AddResource(rsrc *resourcev1.Resource) error {
	m.resources = append(m.resources, rsrc)
	return nil
}

func (m *mockStore) AddRelationships(rels ...*resourcev1.Relationship) error {
	m.relationships = append(m.relationships, rels...)
	return nil
}

// Implement other required Store interface methods as no-ops for testing
func (m *mockStore) UpdateResource(rsrc *resourcev1.Resource) error   {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources = append(m.resources, rsrc)
	return nil
}
func (m *mockStore) DeleteResource(ref *resourcev1.ResourceRef) error { return nil }
func (m *mockStore) GetResource(ref *resourcev1.ResourceRef) (*resourcev1.Resource, error) {
	return nil, nil
}
func (m *mockStore) GetRelationships(subject, object *resourcev1.ResourceRef, predicate proto.Message) ([]*resourcev1.Relationship, error) {
	return nil, resource.ErrRelationshipsNotFound
}
func (m *mockStore) Subscribe(typeDefs ...*resourcev1.TypeDescriptor) <-chan resource.Event {
	return nil
}
func (m *mockStore) Close() error { return nil }

func TestBuilder_BuildFromSnapshot_Empty(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	mockStore := newMockStore()
	builder := NewBuilder(logger, mockStore)

	// Empty snapshot
	snapshot := &mockSnapshot{
		containers: []ContainerInfo{},
		processes:  []ProcessInfo{},
	}

	ctx := context.Background()
	err := builder.BuildFromSnapshot(ctx, snapshot)

	require.NoError(t, err)
	assert.Empty(t, mockStore.resources)
	assert.Empty(t, mockStore.relationships)
}

func TestBuilder_BuildFromSnapshot_Containers(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	mockStore := newMockStore()
	builder := NewBuilder(logger, mockStore)

	// Snapshot with containers
	snapshot := &mockSnapshot{
		containers: []ContainerInfo{
			{
				ID:            "container1",
				Runtime:       runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER,
				CgroupVersion: 2,
				CgroupPath:    "/sys/fs/cgroup/docker/container1",
				ImageName:     "nginx",
				ImageTag:      "latest",
				CpusetCpus:    "0-3",
				CpusetMems:    "0",
			},
			{
				ID:            "container2",
				Runtime:       runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CONTAINERD,
				CgroupVersion: 2,
				CgroupPath:    "/sys/fs/cgroup/containerd/container2",
				ImageName:     "redis",
				ImageTag:      "6.2",
				CpusetCpus:    "4-7",
			},
		},
		processes: []ProcessInfo{},
	}

	ctx := context.Background()
	err := builder.BuildFromSnapshot(ctx, snapshot)

	require.NoError(t, err)
	assert.Equal(t, 2, len(mockStore.resources), "Should create 2 container nodes")

	// Verify container nodes were created correctly
	for i, rsrc := range mockStore.resources {
		assert.Equal(t, "resource.v1.Resource", rsrc.Type.Kind)
		assert.Equal(t, "antimetal.runtime.v1.ContainerNode", rsrc.Type.Type)
		// Service field was removed as containers are provider-agnostic
		assert.Contains(t, rsrc.Metadata.Name, snapshot.containers[i].ID)
	}
}

func TestBuilder_BuildFromSnapshot_Processes(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	mockStore := newMockStore()
	builder := NewBuilder(logger, mockStore)

	// Snapshot with processes
	snapshot := &mockSnapshot{
		containers: []ContainerInfo{},
		processes: []ProcessInfo{
			{
				PID:     100,
				PPID:    1,
				PGID:    100,
				SID:     100,
				Command: "nginx",
				Cmdline: "nginx -g daemon off;",
				State:   "R",
			},
			{
				PID:     101,
				PPID:    100, // Child of process 100
				PGID:    100,
				SID:     100,
				Command: "nginx",
				Cmdline: "nginx: worker process",
				State:   "S",
			},
		},
	}

	ctx := context.Background()
	err := builder.BuildFromSnapshot(ctx, snapshot)

	require.NoError(t, err)
	assert.Equal(t, 2, len(mockStore.resources), "Should create 2 process nodes")

	// Verify process nodes were created
	for _, rsrc := range mockStore.resources {
		assert.Equal(t, "resource.v1.Resource", rsrc.Type.Kind)
		assert.Equal(t, "antimetal.runtime.v1.ProcessNode", rsrc.Type.Type)
	}

	// Should have created bidirectional parent-child relationships (2 relationships)
	assert.Equal(t, 2, len(mockStore.relationships), "Should create 2 relationships (bidirectional)")

	// Check that we have both Contains and ContainedBy relationships
	hasContains := false
	hasContainedBy := false
	for _, rel := range mockStore.relationships {
		if rel.Type.Type == string(proto.MessageName(&runtimev1.Contains{})) {
			hasContains = true
			assert.Contains(t, rel.Subject.Name, "100") // Parent PID
			assert.Contains(t, rel.Object.Name, "101")  // Child PID
		}
		if rel.Type.Type == string(proto.MessageName(&runtimev1.ContainedBy{})) {
			hasContainedBy = true
			assert.Contains(t, rel.Subject.Name, "101") // Child PID
			assert.Contains(t, rel.Object.Name, "100")  // Parent PID
		}
	}
	assert.True(t, hasContains, "Should have Contains relationship")
	assert.True(t, hasContainedBy, "Should have ContainedBy relationship")
}

func TestBuilder_BuildFromSnapshot_CompleteTopology(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	// Use real in-memory store for more complete testing
	realStore, err := store.New("", logr.Discard())
	require.NoError(t, err)

	builder := NewBuilder(logger, realStore)

	// Complete snapshot with containers and processes
	snapshot := &mockSnapshot{
		containers: []ContainerInfo{
			{
				ID:            "container1",
				Runtime:       runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER,
				CgroupVersion: 2,
				CgroupPath:    "/sys/fs/cgroup/docker/container1",
				CpusetCpus:    "0-3",
				CpusetMems:    "0",
			},
		},
		processes: []ProcessInfo{
			{
				PID:     1000,
				PPID:    1,
				Command: "init",
				State:   "S",
			},
			{
				PID:     1001,
				PPID:    1000,
				Command: "nginx",
				State:   "S",
			},
			{
				PID:     1002,
				PPID:    1001,
				Command: "nginx",
				Cmdline: "nginx: worker process",
				State:   "S",
			},
		},
	}

	ctx := context.Background()
	err = builder.BuildFromSnapshot(ctx, snapshot)

	require.NoError(t, err)
	// With a real store, we can't easily inspect internal state,
	// but we've verified no errors occurred during building
}

func TestBuilder_BuildFromSnapshot_CPUAffinity(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	mockStore := newMockStore()
	builder := NewBuilder(logger, mockStore)

	// Container with CPU affinity
	snapshot := &mockSnapshot{
		containers: []ContainerInfo{
			{
				ID:         "affinity-test",
				Runtime:    runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER,
				CpusetCpus: "0,2,4,6", // Specific CPU affinity
				CpusetMems: "0-1",     // NUMA nodes 0 and 1
			},
		},
		processes: []ProcessInfo{},
	}

	ctx := context.Background()
	err := builder.BuildFromSnapshot(ctx, snapshot)

	require.NoError(t, err)

	// Should create container node
	assert.Equal(t, 1, len(mockStore.resources))

	// The relationships to CPUs and NUMA nodes would be created
	// but since we're using a mock store and the builder tries to
	// look up CPU/NUMA nodes that don't exist, relationships won't be created
	// This is expected behavior - graceful degradation when hardware nodes don't exist
}

func TestBuilder_BuildFromSnapshot_LargeScale(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	mockStore := newMockStore()
	builder := NewBuilder(logger, mockStore)

	// Create a large snapshot to test performance
	containers := make([]ContainerInfo, 100)
	for i := 0; i < 100; i++ {
		containers[i] = ContainerInfo{
			ID:            string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Runtime:       runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER,
			CgroupVersion: 2,
		}
	}

	processes := make([]ProcessInfo, 1000)
	for i := 0; i < 1000; i++ {
		processes[i] = ProcessInfo{
			PID:     int32(1000 + i),
			PPID:    int32(1), // All children of init for simplicity
			Command: "test-process",
			State:   "R",
		}
	}

	snapshot := &mockSnapshot{
		containers: containers,
		processes:  processes,
	}

	ctx := context.Background()
	err := builder.BuildFromSnapshot(ctx, snapshot)

	require.NoError(t, err)
	assert.Equal(t, 1100, len(mockStore.resources), "Should create 100 containers + 1000 processes")
}

func TestBuilder_ParseCgroupVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  int
		expected runtimev1.CgroupVersion
	}{
		{
			name:     "cgroup v1",
			version:  1,
			expected: runtimev1.CgroupVersion_CGROUP_VERSION_V1,
		},
		{
			name:     "cgroup v2",
			version:  2,
			expected: runtimev1.CgroupVersion_CGROUP_VERSION_V2,
		},
		{
			name:     "unknown version defaults to v1",
			version:  3,
			expected: runtimev1.CgroupVersion_CGROUP_VERSION_V1,
		},
		{
			name:     "zero version defaults to v1",
			version:  0,
			expected: runtimev1.CgroupVersion_CGROUP_VERSION_V1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCgroupVersion(tt.version)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuilder_BuildFromSnapshot_ProcessHierarchy(t *testing.T) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	mockStore := newMockStore()
	builder := NewBuilder(logger, mockStore)

	// Complex process hierarchy
	snapshot := &mockSnapshot{
		containers: []ContainerInfo{},
		processes: []ProcessInfo{
			{PID: 1, PPID: 0, Command: "init"},
			{PID: 100, PPID: 1, Command: "systemd"},
			{PID: 200, PPID: 100, Command: "dockerd"},
			{PID: 300, PPID: 200, Command: "containerd"},
			{PID: 400, PPID: 300, Command: "container-shim"},
			{PID: 500, PPID: 400, Command: "nginx"},
			{PID: 501, PPID: 500, Command: "nginx-worker"},
			{PID: 502, PPID: 500, Command: "nginx-worker"},
		},
	}

	ctx := context.Background()
	err := builder.BuildFromSnapshot(ctx, snapshot)

	require.NoError(t, err)
	assert.Equal(t, 8, len(mockStore.resources), "Should create 8 process nodes")

	// Should create bidirectional relationships for all parent-child pairs (7 pairs = 14 relationships)
	// Note: PID 1 has PPID 0, which doesn't exist, so no relationship for that
	assert.Equal(t, 14, len(mockStore.relationships), "Should create 14 relationships (7 bidirectional pairs)")
}
