// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuilder_EmptyAndNilFields tests handling of empty and nil fields
func TestBuilder_EmptyAndNilFields(t *testing.T) {
	tests := []struct {
		name     string
		snapshot *types.Snapshot
		wantErr  bool
	}{
		{
			name: "Completely empty snapshot",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
			},
			wantErr: false,
		},
		{
			name: "Nil CPU cores slice",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				CPUInfo: &performance.CPUInfo{
					VendorID:      "GenuineIntel",
					ModelName:     "Test CPU",
					PhysicalCores: 2,
					LogicalCores:  4,
					Cores:         nil,
				},
			},
			wantErr: false,
		},
		{
			name: "Empty CPU cores slice",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				CPUInfo: &performance.CPUInfo{
					VendorID:      "GenuineIntel",
					ModelName:     "Test CPU",
					PhysicalCores: 2,
					LogicalCores:  4,
					Cores:         []performance.CPUCore{},
				},
			},
			wantErr: false,
		},
		{
			name: "Empty NUMA nodes",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				MemoryInfo: &performance.MemoryInfo{
					TotalBytes:  8589934592,
					NUMAEnabled: true,
					NUMANodes:   []performance.NUMANode{},
				},
			},
			wantErr: false,
		},
		{
			name: "Empty disk info array",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				DiskInfo:  []*performance.DiskInfo{},
			},
			wantErr: false,
		},
		{
			name: "Empty network info array",
			snapshot: &types.Snapshot{
				Timestamp:   time.Now(),
				NetworkInfo: []*performance.NetworkInfo{},
			},
			wantErr: false,
		},
		{
			name: "Zero values in CPU info",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				CPUInfo: &performance.CPUInfo{
					VendorID:      "",
					ModelName:     "",
					PhysicalCores: 0,
					LogicalCores:  0,
					Cores:         []performance.CPUCore{},
				},
			},
			wantErr: false,
		},
		{
			name: "Zero memory size",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				MemoryInfo: &performance.MemoryInfo{
					TotalBytes:  0,
					NUMAEnabled: false,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger := logr.Discard()

			testStore, err := store.New(store.WithDataDir(""))
			require.NoError(t, err)
			defer testStore.Close()

			builder := hardwaregraph.NewBuilder(logger, testStore)
			err = builder.BuildFromSnapshot(ctx, tt.snapshot)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestBuilder_InconsistentData tests handling of inconsistent hardware data
func TestBuilder_InconsistentData(t *testing.T) {
	tests := []struct {
		name     string
		snapshot *types.Snapshot
		wantErr  bool
	}{
		{
			name: "CPU cores count mismatch",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				CPUInfo: &performance.CPUInfo{
					VendorID:      "GenuineIntel",
					ModelName:     "Test CPU",
					PhysicalCores: 4,
					LogicalCores:  8,
					Cores:         generateSingleSocketCPUCores(2, true), // Only 4 cores instead of 8
				},
			},
			wantErr: false,
		},
		{
			name: "NUMA enabled but no nodes",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				MemoryInfo: &performance.MemoryInfo{
					TotalBytes:  8589934592,
					NUMAEnabled: true,
					NUMANodes:   []performance.NUMANode{},
				},
			},
			wantErr: false,
		},
		{
			name: "NUMA disabled but has nodes",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				MemoryInfo: &performance.MemoryInfo{
					TotalBytes:  8589934592,
					NUMAEnabled: false,
					NUMANodes: []performance.NUMANode{
						{
							NodeID:     0,
							TotalBytes: 8589934592,
							CPUs:       []int32{0, 1, 2, 3},
							Distances:  []int32{10},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Disk with no partitions",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				DiskInfo: []*performance.DiskInfo{
					{
						Device:     "sda",
						SizeBytes:  107374182400,
						Partitions: []performance.PartitionInfo{},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Network interface with missing fields",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				NetworkInfo: []*performance.NetworkInfo{
					{
						Interface:  "eth0",
						MACAddress: "",
						Speed:      0,
						Driver:     "",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Negative NUMA node ID",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				MemoryInfo: &performance.MemoryInfo{
					TotalBytes:  8589934592,
					NUMAEnabled: true,
					NUMANodes: []performance.NUMANode{
						{
							NodeID:     -1,
							TotalBytes: 8589934592,
							CPUs:       []int32{0, 1, 2, 3},
							Distances:  []int32{10},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger := logr.Discard()

			testStore, err := store.New(store.WithDataDir(""))
			require.NoError(t, err)
			defer testStore.Close()

			builder := hardwaregraph.NewBuilder(logger, testStore)
			err = builder.BuildFromSnapshot(ctx, tt.snapshot)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestBuilder_ExtremeValues tests handling of extreme values
func TestBuilder_ExtremeValues(t *testing.T) {
	tests := []struct {
		name     string
		snapshot *types.Snapshot
		wantErr  bool
	}{
		{
			name: "Very large core count",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				CPUInfo: &performance.CPUInfo{
					VendorID:      "GenuineIntel",
					ModelName:     "Test CPU",
					PhysicalCores: 256,
					LogicalCores:  512,
					Cores:         generateSingleSocketCPUCores(256, true),
				},
			},
			wantErr: false,
		},
		{
			name: "Very large memory size",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				MemoryInfo: &performance.MemoryInfo{
					TotalBytes:  1099511627776, // 1TB
					NUMAEnabled: false,
				},
			},
			wantErr: false,
		},
		{
			name: "Very large disk size",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				DiskInfo: []*performance.DiskInfo{
					{
						Device:    "sda",
						SizeBytes: 18446744073709551615, // Max uint64
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Very high network speed",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				NetworkInfo: []*performance.NetworkInfo{
					{
						Interface: "eth0",
						Speed:     400000, // 400 Gbps
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Many NUMA nodes",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				MemoryInfo: &performance.MemoryInfo{
					TotalBytes:  1099511627776,
					NUMAEnabled: true,
					NUMANodes:   generateNUMANodes(16, 8),
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger := logr.Discard()

			testStore, err := store.New(store.WithDataDir(""))
			require.NoError(t, err)
			defer testStore.Close()

			builder := hardwaregraph.NewBuilder(logger, testStore)
			err = builder.BuildFromSnapshot(ctx, tt.snapshot)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestBuilder_DuplicateData tests handling of duplicate entries
func TestBuilder_DuplicateData(t *testing.T) {
	tests := []struct {
		name     string
		snapshot *types.Snapshot
		wantErr  bool
	}{
		{
			name: "Duplicate CPU cores with same processor ID",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				CPUInfo: &performance.CPUInfo{
					VendorID:      "GenuineIntel",
					ModelName:     "Test CPU",
					PhysicalCores: 2,
					LogicalCores:  4,
					Cores: []performance.CPUCore{
						{Processor: 0, PhysicalID: 0, CoreID: 0},
						{Processor: 0, PhysicalID: 0, CoreID: 0}, // Duplicate
						{Processor: 1, PhysicalID: 0, CoreID: 1},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Duplicate disk devices",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				DiskInfo: []*performance.DiskInfo{
					{Device: "sda", SizeBytes: 107374182400},
					{Device: "sda", SizeBytes: 107374182400}, // Duplicate
				},
			},
			wantErr: false,
		},
		{
			name: "Duplicate network interfaces",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				NetworkInfo: []*performance.NetworkInfo{
					{Interface: "eth0", Speed: 1000},
					{Interface: "eth0", Speed: 1000}, // Duplicate
				},
			},
			wantErr: false,
		},
		{
			name: "Duplicate NUMA node IDs",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				MemoryInfo: &performance.MemoryInfo{
					TotalBytes:  8589934592,
					NUMAEnabled: true,
					NUMANodes: []performance.NUMANode{
						{NodeID: 0, TotalBytes: 4294967296, CPUs: []int32{0, 1}, Distances: []int32{10, 21}},
						{NodeID: 0, TotalBytes: 4294967296, CPUs: []int32{2, 3}, Distances: []int32{21, 10}}, // Duplicate ID
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger := logr.Discard()

			testStore, err := store.New(store.WithDataDir(""))
			require.NoError(t, err)
			defer testStore.Close()

			builder := hardwaregraph.NewBuilder(logger, testStore)
			err = builder.BuildFromSnapshot(ctx, tt.snapshot)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestBuilder_SpecialCharactersAndEncoding tests special characters in device names
func TestBuilder_SpecialCharactersAndEncoding(t *testing.T) {
	tests := []struct {
		name     string
		snapshot *types.Snapshot
		wantErr  bool
	}{
		{
			name: "Disk device with special characters",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				DiskInfo: []*performance.DiskInfo{
					{Device: "nvme0n1p1-foo_bar", SizeBytes: 107374182400},
					{Device: "dm-0", SizeBytes: 107374182400},
					{Device: "loop0", SizeBytes: 1073741824},
				},
			},
			wantErr: false,
		},
		{
			name: "Network interface with special naming",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				NetworkInfo: []*performance.NetworkInfo{
					{Interface: "eth0:1", Speed: 1000},      // Virtual interface
					{Interface: "eth0.100", Speed: 1000},    // VLAN
					{Interface: "br-abcd1234", Speed: 0},    // Docker bridge
					{Interface: "veth@if123", Speed: 10000}, // veth pair
					{Interface: "wlan0", Speed: 1000},       // Wireless
					{Interface: "enp0s3", Speed: 1000},      // Predictable naming
					{Interface: "ens192", Speed: 10000},     // Another predictable name
				},
			},
			wantErr: false,
		},
		{
			name: "CPU model with unicode characters",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				CPUInfo: &performance.CPUInfo{
					VendorID:      "GenuineIntel",
					ModelName:     "Intel® Xeon® Platinum 8275CL CPU @ 3.00GHz",
					PhysicalCores: 2,
					LogicalCores:  4,
					Cores:         generateSingleSocketCPUCores(2, true),
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger := logr.Discard()

			testStore, err := store.New(store.WithDataDir(""))
			require.NoError(t, err)
			defer testStore.Close()

			builder := hardwaregraph.NewBuilder(logger, testStore)
			err = builder.BuildFromSnapshot(ctx, tt.snapshot)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestBuilder_RapidUpdates tests handling of rapid sequential updates
func TestBuilder_RapidUpdates(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	testStore, err := store.New(store.WithDataDir(""))
	require.NoError(t, err)
	defer testStore.Close()

	builder := hardwaregraph.NewBuilder(logger, testStore)

	// Build the same snapshot multiple times rapidly
	for i := 0; i < 10; i++ {
		snapshot := &types.Snapshot{
			Timestamp: time.Now(),
			CPUInfo: &performance.CPUInfo{
				VendorID:      "GenuineIntel",
				ModelName:     "Test CPU",
				PhysicalCores: 2,
				LogicalCores:  4,
				Cores:         generateSingleSocketCPUCores(2, true),
			},
			MemoryInfo: &performance.MemoryInfo{
				TotalBytes: 8589934592,
			},
		}

		err := builder.BuildFromSnapshot(ctx, snapshot)
		require.NoError(t, err, "Failed on iteration %d", i)
	}

	t.Log("Successfully handled 10 rapid updates")
}

// TestBuilder_ConcurrentBuilds tests concurrent graph building (if supported)
func TestBuilder_ConcurrentBuilds(t *testing.T) {
	// Note: This tests whether concurrent builds cause race conditions or data corruption
	// The actual implementation may not support true concurrent builds
	t.Skip("Concurrent builds may not be supported - requires synchronization analysis")
}
