// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package hardwaregraph_test

import (
	"context"
	"testing"
	"time"

	hardwaregraph "github.com/antimetal/agent/internal/hardware/graph"
	"github.com/antimetal/agent/internal/hardware/types"
	"github.com/antimetal/agent/internal/resource/store"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
)

// BenchmarkHardwareGraph_SmallVM benchmarks a small VM (2 vCPU, 4GB RAM)
func BenchmarkHardwareGraph_SmallVM(b *testing.B) {
	snapshot := &types.Snapshot{
		Timestamp: time.Now(),
		CPUInfo: &performance.CPUInfo{
			VendorID:      "GenuineIntel",
			ModelName:     "Intel Xeon",
			PhysicalCores: 1,
			LogicalCores:  2,
			Cores:         generateSingleSocketCPUCores(1, true),
		},
		MemoryInfo: &performance.MemoryInfo{
			TotalBytes:  4294967296,
			NUMAEnabled: false,
		},
		DiskInfo: []*performance.DiskInfo{
			{Device: "sda", SizeBytes: 53687091200},
		},
		NetworkInfo: []*performance.NetworkInfo{
			{Interface: "eth0", Speed: 1000},
		},
	}

	benchmarkBuildFromSnapshot(b, snapshot)
}

// BenchmarkHardwareGraph_StandardServer benchmarks a standard server (8 CPU, 32GB RAM)
func BenchmarkHardwareGraph_StandardServer(b *testing.B) {
	snapshot := &types.Snapshot{
		Timestamp: time.Now(),
		CPUInfo: &performance.CPUInfo{
			VendorID:      "GenuineIntel",
			ModelName:     "Intel Xeon",
			PhysicalCores: 4,
			LogicalCores:  8,
			Cores:         generateSingleSocketCPUCores(4, true),
		},
		MemoryInfo: &performance.MemoryInfo{
			TotalBytes:  34359738368,
			NUMAEnabled: true,
			NUMANodes: []performance.NUMANode{
				{
					NodeID:     0,
					TotalBytes: 34359738368,
					CPUs:       []int32{0, 1, 2, 3, 4, 5, 6, 7},
					Distances:  []int32{10},
				},
			},
		},
		DiskInfo: []*performance.DiskInfo{
			{Device: "nvme0n1", SizeBytes: 1000204886016},
			{Device: "nvme1n1", SizeBytes: 1000204886016},
			{Device: "sda", SizeBytes: 4000787030016},
			{Device: "sdb", SizeBytes: 4000787030016},
		},
		NetworkInfo: []*performance.NetworkInfo{
			{Interface: "eth0", Speed: 10000},
			{Interface: "eth1", Speed: 10000},
		},
	}

	benchmarkBuildFromSnapshot(b, snapshot)
}

// BenchmarkHardwareGraph_LargeServer benchmarks a large server (32 CPU, 256GB RAM)
func BenchmarkHardwareGraph_LargeServer(b *testing.B) {
	snapshot := &types.Snapshot{
		Timestamp: time.Now(),
		CPUInfo: &performance.CPUInfo{
			VendorID:      "GenuineIntel",
			ModelName:     "Intel Xeon Gold",
			PhysicalCores: 16,
			LogicalCores:  32,
			Cores:         generateSingleSocketCPUCores(16, true),
		},
		MemoryInfo: &performance.MemoryInfo{
			TotalBytes:  274877906944,
			NUMAEnabled: true,
			NUMANodes: []performance.NUMANode{
				{
					NodeID:     0,
					TotalBytes: 137438953472,
					CPUs:       []int32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
					Distances:  []int32{10, 21},
				},
				{
					NodeID:     1,
					TotalBytes: 137438953472,
					CPUs:       []int32{16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31},
					Distances:  []int32{21, 10},
				},
			},
		},
		DiskInfo: generateLargeServerDisks(8),
		NetworkInfo: []*performance.NetworkInfo{
			{Interface: "eth0", Speed: 25000},
			{Interface: "eth1", Speed: 25000},
			{Interface: "eth2", Speed: 10000},
			{Interface: "eth3", Speed: 10000},
		},
	}

	benchmarkBuildFromSnapshot(b, snapshot)
}

// BenchmarkHardwareGraph_NUMAServer benchmarks a multi-socket NUMA server
func BenchmarkHardwareGraph_NUMAServer(b *testing.B) {
	snapshot := &types.Snapshot{
		Timestamp: time.Now(),
		CPUInfo: &performance.CPUInfo{
			VendorID:      "GenuineIntel",
			ModelName:     "Intel Xeon Platinum",
			PhysicalCores: 40,
			LogicalCores:  80,
			Cores:         generateMultiSocketCPUCores(2, 20, true),
		},
		MemoryInfo: &performance.MemoryInfo{
			TotalBytes:  536870912000,
			NUMAEnabled: true,
			NUMANodes:   generateNUMANodes(2, 20),
		},
		DiskInfo:    generateServerDiskConfig(),
		NetworkInfo: generateServerNetworkConfig(),
	}

	benchmarkBuildFromSnapshot(b, snapshot)
}

// BenchmarkHardwareGraph_ManyDisks benchmarks a storage server with many disks
func BenchmarkHardwareGraph_ManyDisks(b *testing.B) {
	snapshot := &types.Snapshot{
		Timestamp: time.Now(),
		CPUInfo: &performance.CPUInfo{
			VendorID:      "GenuineIntel",
			ModelName:     "Intel Xeon",
			PhysicalCores: 8,
			LogicalCores:  16,
			Cores:         generateSingleSocketCPUCores(8, true),
		},
		MemoryInfo: &performance.MemoryInfo{
			TotalBytes:  68719476736,
			NUMAEnabled: false,
		},
		DiskInfo:    generateLargeServerDisks(24),
		NetworkInfo: generateServerNetworkConfig(),
	}

	benchmarkBuildFromSnapshot(b, snapshot)
}

// BenchmarkHardwareGraph_ManyNetworkInterfaces benchmarks many network interfaces
func BenchmarkHardwareGraph_ManyNetworkInterfaces(b *testing.B) {
	snapshot := &types.Snapshot{
		Timestamp: time.Now(),
		CPUInfo: &performance.CPUInfo{
			VendorID:      "GenuineIntel",
			ModelName:     "Intel Xeon",
			PhysicalCores: 8,
			LogicalCores:  16,
			Cores:         generateSingleSocketCPUCores(8, true),
		},
		MemoryInfo: &performance.MemoryInfo{
			TotalBytes:  68719476736,
			NUMAEnabled: false,
		},
		DiskInfo:    generateServerDiskConfig(),
		NetworkInfo: generateManyNetworkInterfaces(50),
	}

	benchmarkBuildFromSnapshot(b, snapshot)
}

// Helper function to run the benchmark
func benchmarkBuildFromSnapshot(b *testing.B, snapshot *types.Snapshot) {
	ctx := context.Background()
	logger := logr.Discard()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		testStore, err := store.New(store.WithDataDir(""))
		require.NoError(b, err)
		builder := hardwaregraph.NewBuilder(logger, testStore)
		b.StartTimer()

		err = builder.BuildFromSnapshot(ctx, snapshot)
		require.NoError(b, err)

		b.StopTimer()
		testStore.Close()
		b.StartTimer()
	}
}
