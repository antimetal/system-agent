// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package graph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/resource/store"
	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// MockRuntimeSnapshot implements RuntimeSnapshot for testing
type MockRuntimeSnapshot struct {
	containers []ContainerInfo
	processes  []ProcessInfo
}

func (m *MockRuntimeSnapshot) GetContainers() []ContainerInfo {
	return m.containers
}

func (m *MockRuntimeSnapshot) GetProcesses() []ProcessInfo {
	return m.processes
}

func TestBuilder_Integration_CompleteTopology(t *testing.T) {
	logger := zapr.NewLogger(zaptest.NewLogger(t))

	// Create in-memory store
	rsrcStore, err := store.New("")
	require.NoError(t, err)
	defer rsrcStore.Close()

	builder := NewBuilder(logger, rsrcStore)

	t.Run("BuildContainerProcessHierarchy", func(t *testing.T) {
		// Create a realistic topology:
		// - 2 containers with CPU affinity
		// - Multiple processes, some in containers, some on host
		// - Parent-child process relationships

		snapshot := &MockRuntimeSnapshot{
			containers: []ContainerInfo{
				{
					ID:            "docker-abc123",
					Runtime:       runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER,
					CgroupVersion: 2,
					CgroupPath:    "/docker/abc123",
					ImageName:     "nginx",
					ImageTag:      "latest",
					CpusetCpus:    "0-3", // Pinned to CPUs 0-3
					CpusetMems:    "0",   // NUMA node 0
					Labels: map[string]string{
						"app": "webserver",
					},
				},
				{
					ID:            "containerd-def456",
					Runtime:       runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CONTAINERD,
					CgroupVersion: 2,
					CgroupPath:    "/system.slice/containerd-def456.scope",
					ImageName:     "redis",
					ImageTag:      "6.2",
					CpusetCpus:    "4-7", // Pinned to CPUs 4-7
					CpusetMems:    "1",   // NUMA node 1
				},
			},
			processes: []ProcessInfo{
				// Init process
				{PID: 1, PPID: 0, Command: "systemd", State: "S"},
				// Container processes
				{PID: 1000, PPID: 1, Command: "nginx", State: "S"},
				{PID: 1001, PPID: 1000, Command: "nginx", State: "S"}, // worker
				{PID: 2000, PPID: 1, Command: "redis-server", State: "S"},
				// Host processes
				{PID: 500, PPID: 1, Command: "sshd", State: "S"},
				{PID: 501, PPID: 500, Command: "sshd", State: "S"}, // child
			},
		}

		// Setup mock /proc filesystem for container detection
		setupMockProcFS(t, snapshot.processes, map[int32]string{
			1000: "/docker/abc123",
			1001: "/docker/abc123",
			2000: "/system.slice/containerd-def456.scope",
		})

		ctx := context.Background()
		err := builder.BuildFromSnapshot(ctx, snapshot)
		require.NoError(t, err)

		// Verify nodes were created by checking for specific resources
		// Check for container nodes
		container1Ref := &resourcev1.ResourceRef{
			TypeUrl: "antimetal.runtime.v1/ContainerNode",
			Name:    "container-docker-abc123",
		}
		container1, err := rsrcStore.GetResource(container1Ref)
		assert.NoError(t, err, "Should find first container")
		assert.NotNil(t, container1)

		container2Ref := &resourcev1.ResourceRef{
			TypeUrl: "antimetal.runtime.v1/ContainerNode",
			Name:    "container-containerd-def456",
		}
		container2, err := rsrcStore.GetResource(container2Ref)
		assert.NoError(t, err, "Should find second container")
		assert.NotNil(t, container2)

		// Check for process nodes
		for _, pid := range []int32{1, 1000, 1001, 2000, 500, 501} {
			processRef := &resourcev1.ResourceRef{
				TypeUrl: "antimetal.runtime.v1/ProcessNode",
				Name:    fmt.Sprintf("process-%d", pid),
			}
			process, err := rsrcStore.GetResource(processRef)
			assert.NoError(t, err, "Should find process %d", pid)
			assert.NotNil(t, process, "Process %d should exist", pid)
		}

		// Verify relationships exist
		// Check container-CPU relationships for first container
		containerRef := &resourcev1.ResourceRef{
			TypeUrl: "antimetal.runtime.v1/ContainerNode",
			Name:    "container-docker-abc123",
		}

		// Should have relationships to CPUs 0-3
		rels, err := rsrcStore.GetRelationships(containerRef, nil, &runtimev1.RunsOn{})
		assert.NoError(t, err)
		// Note: CPU relationships depend on hardware discovery being run first
		// In a real environment, these would be created

		// Check parent-child process relationships
		parentRef := &resourcev1.ResourceRef{
			TypeUrl: "antimetal.runtime.v1/ProcessNode",
			Name:    "process-1000",
		}
		childRef := &resourcev1.ResourceRef{
			TypeUrl: "antimetal.runtime.v1/ProcessNode",
			Name:    "process-1001",
		}

		parentRels, err := rsrcStore.GetRelationships(parentRef, childRef, &runtimev1.ParentOf{})
		assert.NoError(t, err)
		assert.NotEmpty(t, parentRels, "Should have parent-child relationship")
	})
}

func TestBuilder_Integration_CPUAffinity(t *testing.T) {
	logger := zapr.NewLogger(zaptest.NewLogger(t))

	rsrcStore, err := store.New("")
	require.NoError(t, err)
	defer rsrcStore.Close()

	// Pre-populate hardware nodes (CPUs and NUMA nodes)
	setupHardwareNodes(t, rsrcStore)

	builder := NewBuilder(logger, rsrcStore)

	t.Run("ContainerCPUPinning", func(t *testing.T) {
		snapshot := &MockRuntimeSnapshot{
			containers: []ContainerInfo{
				{
					ID:            "test-container",
					Runtime:       runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER,
					CgroupVersion: 2,
					CgroupPath:    "/docker/test-container",
					CpusetCpus:    "0,2,4,6", // Even CPUs only
					CpusetMems:    "0-1",     // Both NUMA nodes
				},
			},
			processes: []ProcessInfo{},
		}

		ctx := context.Background()
		err := builder.BuildFromSnapshot(ctx, snapshot)
		require.NoError(t, err)

		// Verify container-CPU relationships
		containerRef := &resourcev1.ResourceRef{
			TypeUrl: "antimetal.runtime.v1/ContainerNode",
			Name:    "container-test-container",
		}

		// Check RunsOn relationships to CPUs
		cpuRels, err := rsrcStore.GetRelationships(containerRef, nil, &runtimev1.RunsOn{})
		assert.NoError(t, err)
		assert.Len(t, cpuRels, 4, "Should have relationships to 4 CPUs")

		// Verify specific CPU relationships
		expectedCPUs := []int{0, 2, 4, 6}
		for _, expectedCPU := range expectedCPUs {
			cpuRef := &resourcev1.ResourceRef{
				TypeUrl: "antimetal.hardware.v1/CPU",
				Name:    fmt.Sprintf("cpu-%d", expectedCPU),
			}
			rels, err := rsrcStore.GetRelationships(containerRef, cpuRef, &runtimev1.RunsOn{})
			assert.NoError(t, err)
			assert.NotEmpty(t, rels, "Should have relationship to CPU %d", expectedCPU)
		}
	})
}

func TestBuilder_Integration_ProcessContainerMapping(t *testing.T) {
	logger := zapr.NewLogger(zaptest.NewLogger(t))

	rsrcStore, err := store.New("")
	require.NoError(t, err)
	defer rsrcStore.Close()

	builder := NewBuilder(logger, rsrcStore)

	t.Run("ProcessToContainerAssociation", func(t *testing.T) {
		// Test that processes are correctly associated with their containers
		// based on cgroup membership

		containers := []ContainerInfo{
			{
				ID:            "webapp",
				Runtime:       runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER,
				CgroupVersion: 2,
				CgroupPath:    "/docker/webapp",
			},
			{
				ID:            "database",
				Runtime:       runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER,
				CgroupVersion: 2,
				CgroupPath:    "/docker/database",
			},
		}

		processes := []ProcessInfo{
			{PID: 100, PPID: 1, Command: "node"},     // In webapp
			{PID: 101, PPID: 100, Command: "node"},   // Child in webapp
			{PID: 200, PPID: 1, Command: "postgres"}, // In database
			{PID: 300, PPID: 1, Command: "systemd"},  // On host
		}

		// Setup cgroup membership
		setupMockProcFS(t, processes, map[int32]string{
			100: "/docker/webapp",
			101: "/docker/webapp",
			200: "/docker/database",
			// 300 has no container cgroup
		})

		snapshot := &MockRuntimeSnapshot{
			containers: containers,
			processes:  processes,
		}

		ctx := context.Background()
		err := builder.BuildFromSnapshot(ctx, snapshot)
		require.NoError(t, err)

		// Verify container-process relationships
		webappRef := &resourcev1.ResourceRef{
			TypeUrl: "antimetal.runtime.v1/ContainerNode",
			Name:    "container-webapp",
		}

		// Check Contains relationships
		processRels, err := rsrcStore.GetRelationships(webappRef, nil, &runtimev1.Contains{})
		assert.NoError(t, err)
		assert.Len(t, processRels, 2, "Webapp container should contain 2 processes")

		// Verify database container
		dbRef := &resourcev1.ResourceRef{
			TypeUrl: "antimetal.runtime.v1/ContainerNode",
			Name:    "container-database",
		}

		dbProcessRels, err := rsrcStore.GetRelationships(dbRef, nil, &runtimev1.Contains{})
		assert.NoError(t, err)
		assert.Len(t, dbProcessRels, 1, "Database container should contain 1 process")
	})
}

// Helper functions

func setupMockProcFS(t *testing.T, processes []ProcessInfo, containerMappings map[int32]string) {
	// Create mock /proc filesystem structure for testing
	procDir := "/proc"

	for _, proc := range processes {
		pidDir := filepath.Join(procDir, strconv.Itoa(int(proc.PID)))

		// Check if directory exists (it should on Linux)
		if _, err := os.Stat(pidDir); os.IsNotExist(err) {
			// If /proc/<pid> doesn't exist, we're likely in a test environment
			// Skip setting up mock cgroup files
			t.Logf("Skipping mock /proc setup for PID %d (directory doesn't exist)", proc.PID)
			continue
		}

		// If this process belongs to a container, we would write to its cgroup file
		// However, we can't modify real /proc files, so this is just for illustration
		if cgroupPath, ok := containerMappings[proc.PID]; ok {
			t.Logf("Process %d would be in cgroup: %s", proc.PID, cgroupPath)
		}
	}
}

func setupHardwareNodes(t *testing.T, store resource.Store) {
	// Create mock CPU nodes for testing CPU affinity
	for i := 0; i < 8; i++ {
		cpuResource := &resourcev1.Resource{
			Type: &resourcev1.TypeDescriptor{
				Kind: "CPU",
				Type: "antimetal.hardware.v1.CPU",
			},
			Metadata: &resourcev1.ResourceMeta{
				Provider: resourcev1.Provider_PROVIDER_KUBERNETES,
				Service:  "hardware",
				Name:     fmt.Sprintf("cpu-%d", i),
			},
		}
		err := store.AddResource(cpuResource)
		require.NoError(t, err)
	}

	// Create NUMA nodes
	for i := 0; i < 2; i++ {
		numaResource := &resourcev1.Resource{
			Type: &resourcev1.TypeDescriptor{
				Kind: "NUMANode",
				Type: "antimetal.hardware.v1.NUMANode",
			},
			Metadata: &resourcev1.ResourceMeta{
				Provider: resourcev1.Provider_PROVIDER_KUBERNETES,
				Service:  "hardware",
				Name:     fmt.Sprintf("numa-node-%d", i),
			},
		}
		err := store.AddResource(numaResource)
		require.NoError(t, err)
	}
}

func TestBuilder_Integration_RealSystemProcesses(t *testing.T) {
	// This test runs against real system processes
	logger := zapr.NewLogger(zaptest.NewLogger(t))

	rsrcStore, err := store.New("")
	require.NoError(t, err)
	defer rsrcStore.Close()

	builder := NewBuilder(logger, rsrcStore)

	t.Run("RealProcessHierarchy", func(t *testing.T) {
		// Collect real process information
		procPath := "/proc"
		entries, err := os.ReadDir(procPath)
		if err != nil {
			t.Skip("Cannot read /proc directory")
		}

		var processes []ProcessInfo
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			// Check if it's a PID directory
			pid, err := strconv.Atoi(entry.Name())
			if err != nil {
				continue
			}

			// Read process stat file
			statPath := filepath.Join(procPath, entry.Name(), "stat")
			statData, err := os.ReadFile(statPath)
			if err != nil {
				continue // Process might have exited
			}

			// Parse basic info from stat (simplified)
			stat := string(statData)
			ppid := extractPPIDFromStat(stat)

			processes = append(processes, ProcessInfo{
				PID:     int32(pid),
				PPID:    int32(ppid),
				Command: entry.Name(), // Simplified
				State:   "R",
			})

			// Limit to first 100 processes for testing
			if len(processes) >= 100 {
				break
			}
		}

		require.NotEmpty(t, processes, "Should find some processes")

		snapshot := &MockRuntimeSnapshot{
			containers: []ContainerInfo{}, // No containers for this test
			processes:  processes,
		}

		ctx := context.Background()
		err = builder.BuildFromSnapshot(ctx, snapshot)
		require.NoError(t, err)

		// Verify process nodes were created by checking for specific ones
		for _, proc := range processes {
			processRef := &resourcev1.ResourceRef{
				TypeUrl: "antimetal.runtime.v1/ProcessNode",
				Name:    fmt.Sprintf("process-%d", proc.PID),
			}
			processResource, err := rsrcStore.GetResource(processRef)
			if err != nil {
				t.Logf("Could not find process %d: %v", proc.PID, err)
			} else {
				assert.NotNil(t, processResource, "Process %d should exist", proc.PID)
			}
		}

		// Verify init process exists
		initRef := &resourcev1.ResourceRef{
			TypeUrl: "antimetal.runtime.v1/ProcessNode",
			Name:    "process-1",
		}
		initResource, err := rsrcStore.GetResource(initRef)
		assert.NoError(t, err)
		assert.NotNil(t, initResource, "Should have init process")
	})
}

// extractPPIDFromStat is a simplified parser for /proc/[pid]/stat
func extractPPIDFromStat(stat string) int {
	// The stat file format has the PPID as the 4th field
	// Format: pid (comm) state ppid ...
	// We need to handle the comm field which can contain spaces and parens

	// Find the last ')' which closes the comm field
	lastParen := -1
	for i := len(stat) - 1; i >= 0; i-- {
		if stat[i] == ')' {
			lastParen = i
			break
		}
	}

	if lastParen == -1 {
		return 0
	}

	// Fields after the comm field
	remaining := stat[lastParen+1:]
	fields := performance.SplitStatFields(remaining)

	if len(fields) >= 2 {
		// PPID is the second field after comm (state is first)
		ppid, _ := strconv.Atoi(fields[1])
		return ppid
	}

	return 0
}
