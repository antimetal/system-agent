// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/antimetal/agent/internal/runtime/graph"
	"github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/resource/store"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

// BenchmarkRuntimeDiscovery benchmarks the full discovery cycle
func BenchmarkRuntimeDiscovery(b *testing.B) {
	// Create test logger
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	// Setup test directories with mock cgroup structures
	tmpDir := b.TempDir()
	cgroupPath := filepath.Join(tmpDir, "cgroup")

	// Create mock performance manager with correct config
	perfOpts := performance.ManagerOptions{
		Config: performance.CollectionConfig{
			HostProcPath: "/proc",
			HostSysPath:  "/sys",
		},
		Logger: logger,
	}
	perfManager, err := performance.NewManager(perfOpts)
	if err != nil {
		b.Fatal(err)
	}

	// Create in-memory store using correct constructor (empty string = in-memory)
	rsrcStore, err := store.New("")
	if err != nil {
		b.Fatal(err)
	}

	benchmarks := []struct {
		name           string
		containerCount int
		processCount   int
	}{
		{"small", 10, 100},
		{"medium", 50, 500},
		{"large", 100, 1000},
		{"xlarge", 500, 5000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Setup mock cgroup structure for containers
			setupMockCgroups(b, cgroupPath, bm.containerCount)

			manager, err := NewManager(logger, ManagerConfig{
				Store:              rsrcStore,
				PerformanceManager: perfManager,
				CgroupPath:         cgroupPath,
				UpdateInterval:     30 * time.Second,
			})
			if err != nil {
				b.Fatal(err)
			}

			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Benchmark the complete discovery cycle
				if err := manager.ForceUpdate(ctx); err != nil {
					b.Fatal(err)
				}
			}

			// Report metrics
			metrics := manager.GetMetrics()
			b.ReportMetric(float64(metrics.LastContainerCount), "containers")
			b.ReportMetric(float64(metrics.LastProcessCount), "processes")
			b.ReportMetric(float64(metrics.LastDiscoveryDuration.Milliseconds()), "ms")
		})
	}
}

// BenchmarkContainerDiscovery benchmarks just the container discovery phase
func BenchmarkContainerDiscovery(b *testing.B) {
	tmpDir := b.TempDir()
	cgroupPath := filepath.Join(tmpDir, "cgroup")

	benchmarks := []struct {
		name  string
		count int
	}{
		{"10_containers", 10},
		{"100_containers", 100},
		{"1000_containers", 1000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			setupMockCgroups(b, cgroupPath, bm.count)
			discovery := containers.NewDiscovery(cgroupPath)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				containers, err := discovery.DiscoverAllContainers()
				if err != nil {
					b.Fatal(err)
				}
				_ = containers
			}
		})
	}
}

// BenchmarkGraphBuilding benchmarks the graph building phase
func BenchmarkGraphBuilding(b *testing.B) {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	benchmarks := []struct {
		name           string
		containerCount int
		processCount   int
	}{
		{"small_graph", 10, 100},
		{"medium_graph", 50, 500},
		{"large_graph", 100, 1000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Create in-memory store for each benchmark
			rsrcStore, err := store.New("")
			if err != nil {
				b.Fatal(err)
			}

			builder := graph.NewBuilder(logger, rsrcStore)
			snapshot := createMockSnapshot(bm.containerCount, bm.processCount)
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := builder.BuildFromSnapshot(ctx, snapshot); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// setupMockCgroups creates a mock cgroup directory structure for benchmarking
func setupMockCgroups(b *testing.B, cgroupPath string, containerCount int) {
	b.Helper()

	// Create cgroup v2 structure
	for i := 0; i < containerCount; i++ {
		// Docker-style paths
		dockerPath := filepath.Join(cgroupPath, "system.slice",
			"docker.service", "docker",
			"abc123def456789"+string(rune('0'+i)))
		if err := os.MkdirAll(dockerPath, 0755); err != nil {
			b.Fatal(err)
		}

		// Create cgroup.controllers file (marker for v2)
		controllersFile := filepath.Join(dockerPath, "cgroup.controllers")
		if err := os.WriteFile(controllersFile, []byte("cpu memory io\n"), 0644); err != nil {
			b.Fatal(err)
		}

		// Add cpuset files for CPU affinity testing
		cpusetFile := filepath.Join(dockerPath, "cpuset.cpus")
		if err := os.WriteFile(cpusetFile, []byte("0-3\n"), 0644); err != nil {
			b.Fatal(err)
		}
	}
}

// createMockSnapshot creates a mock RuntimeSnapshot for benchmarking
func createMockSnapshot(containerCount, processCount int) graph.RuntimeSnapshot {
	mockContainers := make([]graph.ContainerInfo, containerCount)
	for i := 0; i < containerCount; i++ {
		mockContainers[i] = graph.ContainerInfo{
			ID:            "container" + string(rune('0'+i)),
			CgroupVersion: 2,
			CgroupPath:    "/sys/fs/cgroup/docker/" + string(rune('0'+i)),
			CpusetCpus:    "0-3",
		}
	}

	mockProcesses := make([]graph.ProcessInfo, processCount)
	for i := 0; i < processCount; i++ {
		mockProcesses[i] = graph.ProcessInfo{
			PID:     int32(i + 1000),
			PPID:    int32(1),
			Command: "test-process",
			State:   "R",
		}
	}

	return &mockSnapshot{
		containers: mockContainers,
		processes:  mockProcesses,
	}
}

// mockSnapshot implements graph.RuntimeSnapshot for benchmarking
type mockSnapshot struct {
	containers []graph.ContainerInfo
	processes  []graph.ProcessInfo
}

func (m *mockSnapshot) GetContainers() []graph.ContainerInfo {
	return m.containers
}

func (m *mockSnapshot) GetProcesses() []graph.ProcessInfo {
	return m.processes
}
