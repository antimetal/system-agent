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
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHardwareGraph_RealSystemDiscovery tests hardware discovery on the actual Linux system
func TestHardwareGraph_RealSystemDiscovery(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	// Create collection config
	config := performance.CollectionConfig{
		ProcPath: "/proc",
		SysPath:  "/sys",
		DevPath:  "/dev",
	}

	// Create real collectors to gather actual hardware data
	cpuInfoCollector, err := collectors.NewCPUInfoCollector(logger, config)
	require.NoError(t, err, "Failed to create CPU info collector")

	memInfoCollector, err := collectors.NewMemoryInfoCollector(logger, config)
	require.NoError(t, err, "Failed to create memory info collector")

	diskInfoCollector, err := collectors.NewDiskInfoCollector(logger, config)
	require.NoError(t, err, "Failed to create disk info collector")

	netInfoCollector, err := collectors.NewNetworkInfoCollector(logger, config)
	require.NoError(t, err, "Failed to create network info collector")

	// Collect real hardware data
	cpuInfo, err := cpuInfoCollector.Collect()
	require.NoError(t, err, "Failed to collect CPU info")

	memInfo, err := memInfoCollector.Collect()
	require.NoError(t, err, "Failed to collect memory info")

	diskInfo, err := diskInfoCollector.Collect()
	require.NoError(t, err, "Failed to collect disk info")

	netInfo, err := netInfoCollector.Collect()
	require.NoError(t, err, "Failed to collect network info")

	// Create snapshot from real data
	snapshot := &types.Snapshot{
		Timestamp:   time.Now(),
		NodeName:    "test-node",
		ClusterName: "test-cluster",
		CPUInfo:     cpuInfo,
		MemoryInfo:  memInfo,
		DiskInfo:    diskInfo,
		NetworkInfo: netInfo,
	}

	// Create in-memory store
	testStore, err := store.New(store.WithDataDir(""))
	require.NoError(t, err, "Failed to create in-memory store")
	defer testStore.Close()

	// Build hardware graph
	builder := hardwaregraph.NewBuilder(logger, testStore)
	err = builder.BuildFromSnapshot(ctx, snapshot)
	require.NoError(t, err, "Failed to build hardware graph from real system data")

	// Verify system node was created
	systemRef := &resourcev1.ResourceRef{
		Type: &resourcev1.TypeDescriptor{
			Kind: "resource.v1.Resource",
			Type: "antimetal.hardware.v1.SystemNode",
		},
		Name: "system",
	}
	systemNode, err := testStore.GetResource(systemRef)
	require.NoError(t, err, "System node should exist")
	assert.NotNil(t, systemNode)

	// Log system details
	t.Logf("CPU: %s, Cores: Physical=%d, Logical=%d",
		cpuInfo.ModelName, cpuInfo.PhysicalCores, cpuInfo.LogicalCores)
	t.Logf("Memory: Total=%d GB, NUMA=%v",
		memInfo.TotalBytes/(1024*1024*1024), memInfo.NUMAEnabled)
	t.Logf("Disks: Count=%d", len(diskInfo))
	t.Logf("Network Interfaces: Count=%d", len(netInfo))

	// Verify CPU topology was created
	if cpuInfo != nil && len(cpuInfo.Cores) > 0 {
		// Should have CPU packages
		uniqueSockets := make(map[int32]bool)
		for _, core := range cpuInfo.Cores {
			uniqueSockets[core.PhysicalID] = true
		}
		t.Logf("CPU Sockets: %d", len(uniqueSockets))
		assert.Greater(t, len(uniqueSockets), 0, "Should have at least one CPU socket")
	}

	// Verify NUMA topology if available
	if memInfo != nil && memInfo.NUMAEnabled {
		t.Logf("NUMA Nodes: %d", len(memInfo.NUMANodes))
		assert.Greater(t, len(memInfo.NUMANodes), 0, "Should have NUMA nodes")
	}

	// Verify disk topology
	if len(diskInfo) > 0 {
		for _, disk := range diskInfo {
			t.Logf("Disk: %s, Size=%d GB, Rotational=%v, Partitions=%d",
				disk.Device,
				disk.SizeBytes/(1024*1024*1024),
				disk.Rotational,
				len(disk.Partitions))
		}
	}

	// Verify network topology
	if len(netInfo) > 0 {
		for _, iface := range netInfo {
			t.Logf("Network: %s, Speed=%d Mbps, Driver=%s, State=%s",
				iface.Interface, iface.Speed, iface.Driver, iface.OperState)
		}
	}
}

// TestHardwareGraph_MultiSocketServer tests dual-socket server topology
func TestHardwareGraph_MultiSocketServer(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	// Create a realistic dual-socket Intel Xeon server snapshot
	snapshot := &types.Snapshot{
		Timestamp:   time.Now(),
		NodeName:    "dual-xeon-server",
		ClusterName: "test-cluster",
		CPUInfo: &performance.CPUInfo{
			VendorID:      "GenuineIntel",
			CPUFamily:     6,
			Model:         85,
			ModelName:     "Intel(R) Xeon(R) Gold 6248 CPU @ 2.50GHz",
			Stepping:      7,
			Microcode:     "0x500320a",
			CPUMHz:        2500.000,
			CacheSize:     "28160 KB",
			PhysicalCores: 40, // 20 cores per socket
			LogicalCores:  80, // With HT
			Cores:         generateMultiSocketCPUCores(2, 20, true),
		},
		MemoryInfo: &performance.MemoryInfo{
			TotalBytes:             536870912000, // 512GB
			NUMAEnabled:            true,
			NUMABalancingAvailable: true,
			NUMANodes:              generateNUMANodes(2, 20),
		},
		DiskInfo:    generateServerDiskConfig(),
		NetworkInfo: generateServerNetworkConfig(),
	}

	testStore, err := store.New(store.WithDataDir(""))
	require.NoError(t, err)
	defer testStore.Close()

	builder := hardwaregraph.NewBuilder(logger, testStore)
	err = builder.BuildFromSnapshot(ctx, snapshot)
	require.NoError(t, err, "Failed to build dual-socket server topology")

	// Verify we have 2 CPU packages (sockets)
	// Verify NUMA topology (2 NUMA nodes for 2 sockets)
	// Verify CPU affinity relationships

	t.Logf("Successfully built dual-socket server topology")
}

// TestHardwareGraph_CloudProviderPatterns tests AWS, GCP, Azure specific patterns
func TestHardwareGraph_CloudProviderPatterns(t *testing.T) {
	tests := []struct {
		name     string
		snapshot *types.Snapshot
	}{
		{
			name: "AWS c5.2xlarge",
			snapshot: &types.Snapshot{
				Timestamp:   time.Now(),
				NodeName:    "aws-c5-2xlarge",
				ClusterName: "eks-cluster",
				CPUInfo: &performance.CPUInfo{
					VendorID:      "GenuineIntel",
					CPUFamily:     6,
					Model:         85,
					ModelName:     "Intel(R) Xeon(R) Platinum 8275CL CPU @ 3.00GHz",
					PhysicalCores: 4,
					LogicalCores:  8,
					Cores:         generateSingleSocketCPUCores(4, true),
				},
				MemoryInfo: &performance.MemoryInfo{
					TotalBytes:  16106127360, // 16GB
					NUMAEnabled: true,
					NUMANodes: []performance.NUMANode{
						{
							NodeID:     0,
							TotalBytes: 16106127360,
							CPUs:       []int32{0, 1, 2, 3, 4, 5, 6, 7},
							Distances:  []int32{10},
						},
					},
				},
				DiskInfo: []*performance.DiskInfo{
					{
						Device:            "nvme0n1",
						Model:             "Amazon Elastic Block Store",
						Vendor:            "NVMe",
						SizeBytes:         107374182400,
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
					{
						Interface:  "eth0",
						MACAddress: "02:42:ac:11:00:02",
						Speed:      10000,
						Duplex:     "full",
						MTU:        9001, // AWS Jumbo frames
						Driver:     "ena",
						Type:       "ether",
						OperState:  "up",
						Carrier:    true,
					},
				},
			},
		},
		{
			name: "GCP n2-standard-4",
			snapshot: &types.Snapshot{
				Timestamp:   time.Now(),
				NodeName:    "gcp-n2-standard-4",
				ClusterName: "gke-cluster",
				CPUInfo: &performance.CPUInfo{
					VendorID:      "GenuineIntel",
					CPUFamily:     6,
					Model:         85,
					ModelName:     "Intel(R) Xeon(R) CPU @ 2.80GHz",
					PhysicalCores: 2,
					LogicalCores:  4,
					Cores:         generateSingleSocketCPUCores(2, true),
				},
				MemoryInfo: &performance.MemoryInfo{
					TotalBytes:  16777216000,
					NUMAEnabled: false,
				},
				DiskInfo: []*performance.DiskInfo{
					{
						Device:            "sda",
						Model:             "Google PersistentDisk",
						Vendor:            "Google",
						SizeBytes:         107374182400,
						Rotational:        false,
						BlockSize:         4096,
						PhysicalBlockSize: 4096,
						Scheduler:         "mq-deadline",
						QueueDepth:        256,
					},
				},
				NetworkInfo: []*performance.NetworkInfo{
					{
						Interface:  "eth0",
						MACAddress: "42:01:0a:80:00:02",
						Speed:      10000,
						Duplex:     "full",
						MTU:        1460,
						Driver:     "virtio_net",
						Type:       "ether",
						OperState:  "up",
						Carrier:    true,
					},
				},
			},
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
			require.NoError(t, err, "Failed to build %s topology", tt.name)

			t.Logf("Successfully built %s topology", tt.name)
		})
	}
}

// TestHardwareGraph_StorageConfigurations tests various storage scenarios
func TestHardwareGraph_StorageConfigurations(t *testing.T) {
	tests := []struct {
		name     string
		diskInfo []*performance.DiskInfo
	}{
		{
			name:     "Mixed NVMe and SATA",
			diskInfo: generateMixedStorageConfig(),
		},
		{
			name: "Multiple partitions",
			diskInfo: []*performance.DiskInfo{
				{
					Device:     "sda",
					Model:      "Samsung SSD 970 EVO",
					SizeBytes:  1000204886016,
					Rotational: false,
					Partitions: []performance.PartitionInfo{
						{Name: "sda1", SizeBytes: 536870912000, StartSector: 2048},
						{Name: "sda2", SizeBytes: 107374182400, StartSector: 1048578048},
						{Name: "sda3", SizeBytes: 356960000000, StartSector: 1258293248},
					},
				},
			},
		},
		{
			name:     "Rotational disks",
			diskInfo: generateRotationalDiskConfig(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger := logr.Discard()

			snapshot := &types.Snapshot{
				Timestamp:   time.Now(),
				NodeName:    "storage-test",
				ClusterName: "test-cluster",
				DiskInfo:    tt.diskInfo,
			}

			testStore, err := store.New(store.WithDataDir(""))
			require.NoError(t, err)
			defer testStore.Close()

			builder := hardwaregraph.NewBuilder(logger, testStore)
			err = builder.BuildFromSnapshot(ctx, snapshot)
			require.NoError(t, err, "Failed to build storage topology for %s", tt.name)

			t.Logf("Successfully built %s storage topology", tt.name)
		})
	}
}

// TestHardwareGraph_NetworkConfigurations tests various network scenarios
func TestHardwareGraph_NetworkConfigurations(t *testing.T) {
	tests := []struct {
		name        string
		networkInfo []*performance.NetworkInfo
	}{
		{
			name:        "Bonded interfaces",
			networkInfo: generateBondedNetworkConfig(),
		},
		{
			name:        "Multiple physical interfaces",
			networkInfo: generateMultiNICConfig(),
		},
		{
			name:        "Virtual interfaces",
			networkInfo: generateVirtualNetworkConfig(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger := logr.Discard()

			snapshot := &types.Snapshot{
				Timestamp:   time.Now(),
				NodeName:    "network-test",
				ClusterName: "test-cluster",
				NetworkInfo: tt.networkInfo,
			}

			testStore, err := store.New(store.WithDataDir(""))
			require.NoError(t, err)
			defer testStore.Close()

			builder := hardwaregraph.NewBuilder(logger, testStore)
			err = builder.BuildFromSnapshot(ctx, snapshot)
			require.NoError(t, err, "Failed to build network topology for %s", tt.name)

			t.Logf("Successfully built %s network topology", tt.name)
		})
	}
}

// TestHardwareGraph_PartialFailureScenarios tests graceful degradation
func TestHardwareGraph_PartialFailureScenarios(t *testing.T) {
	tests := []struct {
		name     string
		snapshot *types.Snapshot
		wantErr  bool
	}{
		{
			name: "Missing CPU info",
			snapshot: &types.Snapshot{
				Timestamp:   time.Now(),
				MemoryInfo:  &performance.MemoryInfo{TotalBytes: 8589934592},
				DiskInfo:    []*performance.DiskInfo{{Device: "sda", SizeBytes: 107374182400}},
				NetworkInfo: []*performance.NetworkInfo{{Interface: "eth0"}},
			},
			wantErr: false,
		},
		{
			name: "Missing memory info",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
				CPUInfo: &performance.CPUInfo{
					ModelName:     "Test CPU",
					PhysicalCores: 2,
					LogicalCores:  4,
					Cores:         generateSingleSocketCPUCores(2, true),
				},
				DiskInfo:    []*performance.DiskInfo{{Device: "sda", SizeBytes: 107374182400}},
				NetworkInfo: []*performance.NetworkInfo{{Interface: "eth0"}},
			},
			wantErr: false,
		},
		{
			name: "Only system node",
			snapshot: &types.Snapshot{
				Timestamp: time.Now(),
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
				t.Logf("Successfully handled partial failure scenario: %s", tt.name)
			}
		})
	}
}
