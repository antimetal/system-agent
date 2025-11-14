// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package hardwaregraph_test

import (
	"github.com/antimetal/agent/pkg/performance"
)

// Test fixture generators shared across all test files (unit, integration, benchmarks)

func generateSingleSocketCPUCores(coreCount int32, hyperThreading bool) []performance.CPUCore {
	cores := make([]performance.CPUCore, 0)
	processor := int32(0)

	for coreID := int32(0); coreID < coreCount; coreID++ {
		cores = append(cores, performance.CPUCore{
			Processor:  processor,
			PhysicalID: 0,
			CoreID:     coreID,
			Siblings:   coreCount,
			CPUMHz:     2500.0,
		})
		processor++

		if hyperThreading {
			cores = append(cores, performance.CPUCore{
				Processor:  processor,
				PhysicalID: 0,
				CoreID:     coreID,
				Siblings:   coreCount,
				CPUMHz:     2500.0,
			})
			processor++
		}
	}

	return cores
}

func generateMultiSocketCPUCores(socketCount, coresPerSocket int32, hyperThreading bool) []performance.CPUCore {
	cores := make([]performance.CPUCore, 0)
	processor := int32(0)

	for socketID := int32(0); socketID < socketCount; socketID++ {
		for coreID := int32(0); coreID < coresPerSocket; coreID++ {
			cores = append(cores, performance.CPUCore{
				Processor:  processor,
				PhysicalID: socketID,
				CoreID:     coreID,
				Siblings:   coresPerSocket,
				CPUMHz:     2500.0,
			})
			processor++

			if hyperThreading {
				cores = append(cores, performance.CPUCore{
					Processor:  processor,
					PhysicalID: socketID,
					CoreID:     coreID,
					Siblings:   coresPerSocket,
					CPUMHz:     2500.0,
				})
				processor++
			}
		}
	}

	return cores
}

func generateNUMANodes(nodeCount int, coresPerNode int) []performance.NUMANode {
	nodes := make([]performance.NUMANode, nodeCount)
	totalBytes := uint64(268435456000) // 256GB per node

	for i := 0; i < nodeCount; i++ {
		cpus := make([]int32, coresPerNode*2) // Account for HT
		for j := 0; j < coresPerNode*2; j++ {
			cpus[j] = int32(i*coresPerNode*2 + j)
		}

		// Generate distance matrix
		distances := make([]int32, nodeCount)
		for j := 0; j < nodeCount; j++ {
			if i == j {
				distances[j] = 10
			} else if abs(i-j) == 1 {
				distances[j] = 21
			} else {
				distances[j] = 31
			}
		}

		nodes[i] = performance.NUMANode{
			NodeID:     int32(i),
			TotalBytes: totalBytes,
			CPUs:       cpus,
			Distances:  distances,
		}
	}

	return nodes
}

func generateServerDiskConfig() []*performance.DiskInfo {
	return []*performance.DiskInfo{
		{
			Device:            "nvme0n1",
			Model:             "Samsung SSD 980 PRO",
			Vendor:            "Samsung",
			SizeBytes:         1000204886016,
			Rotational:        false,
			BlockSize:         512,
			PhysicalBlockSize: 512,
			Scheduler:         "none",
			QueueDepth:        1024,
		},
		{
			Device:            "nvme1n1",
			Model:             "Samsung SSD 980 PRO",
			Vendor:            "Samsung",
			SizeBytes:         1000204886016,
			Rotational:        false,
			BlockSize:         512,
			PhysicalBlockSize: 512,
			Scheduler:         "none",
			QueueDepth:        1024,
		},
	}
}

func generateMixedStorageConfig() []*performance.DiskInfo {
	return []*performance.DiskInfo{
		{
			Device:     "nvme0n1",
			Model:      "Samsung SSD 970 EVO",
			SizeBytes:  500107862016,
			Rotational: false,
			Scheduler:  "none",
		},
		{
			Device:     "sda",
			Model:      "WDC WD40EZRZ",
			SizeBytes:  4000787030016,
			Rotational: true,
			Scheduler:  "mq-deadline",
		},
	}
}

func generateRotationalDiskConfig() []*performance.DiskInfo {
	return []*performance.DiskInfo{
		{
			Device:     "sda",
			Model:      "Seagate ST4000DM004",
			SizeBytes:  4000787030016,
			Rotational: true,
			Scheduler:  "mq-deadline",
		},
		{
			Device:     "sdb",
			Model:      "Seagate ST4000DM004",
			SizeBytes:  4000787030016,
			Rotational: true,
			Scheduler:  "mq-deadline",
		},
	}
}

func generateServerNetworkConfig() []*performance.NetworkInfo {
	return []*performance.NetworkInfo{
		{
			Interface:  "eno1",
			MACAddress: "a0:36:9f:00:00:01",
			Speed:      10000,
			Duplex:     "full",
			MTU:        1500,
			Driver:     "ixgbe",
			Type:       "ether",
			OperState:  "up",
			Carrier:    true,
		},
		{
			Interface:  "eno2",
			MACAddress: "a0:36:9f:00:00:02",
			Speed:      10000,
			Duplex:     "full",
			MTU:        1500,
			Driver:     "ixgbe",
			Type:       "ether",
			OperState:  "up",
			Carrier:    true,
		},
	}
}

func generateBondedNetworkConfig() []*performance.NetworkInfo {
	return []*performance.NetworkInfo{
		{
			Interface:  "bond0",
			MACAddress: "a0:36:9f:00:00:01",
			Speed:      20000, // Aggregated
			Duplex:     "full",
			MTU:        1500,
			Driver:     "bonding",
			Type:       "ether",
			OperState:  "up",
			Carrier:    true,
		},
		{
			Interface:  "eth0",
			MACAddress: "a0:36:9f:00:00:01",
			Speed:      10000,
			Duplex:     "full",
			MTU:        1500,
			Driver:     "ixgbe",
			Type:       "ether",
			OperState:  "up",
			Carrier:    true,
		},
		{
			Interface:  "eth1",
			MACAddress: "a0:36:9f:00:00:02",
			Speed:      10000,
			Duplex:     "full",
			MTU:        1500,
			Driver:     "ixgbe",
			Type:       "ether",
			OperState:  "up",
			Carrier:    true,
		},
	}
}

func generateMultiNICConfig() []*performance.NetworkInfo {
	return []*performance.NetworkInfo{
		{
			Interface:  "eth0",
			MACAddress: "a0:36:9f:00:00:01",
			Speed:      10000,
			Duplex:     "full",
			MTU:        1500,
			Driver:     "ixgbe",
			Type:       "ether",
			OperState:  "up",
			Carrier:    true,
		},
		{
			Interface:  "eth1",
			MACAddress: "a0:36:9f:00:00:02",
			Speed:      10000,
			Duplex:     "full",
			MTU:        1500,
			Driver:     "ixgbe",
			Type:       "ether",
			OperState:  "up",
			Carrier:    true,
		},
		{
			Interface:  "eth2",
			MACAddress: "a0:36:9f:00:00:03",
			Speed:      1000,
			Duplex:     "full",
			MTU:        1500,
			Driver:     "e1000e",
			Type:       "ether",
			OperState:  "up",
			Carrier:    true,
		},
	}
}

func generateVirtualNetworkConfig() []*performance.NetworkInfo {
	return []*performance.NetworkInfo{
		{
			Interface:  "eth0",
			MACAddress: "02:42:ac:11:00:02",
			Speed:      10000,
			Duplex:     "full",
			MTU:        1500,
			Driver:     "veth",
			Type:       "ether",
			OperState:  "up",
			Carrier:    true,
		},
		{
			Interface:  "docker0",
			MACAddress: "02:42:5e:7f:00:01",
			Speed:      0,
			Duplex:     "unknown",
			MTU:        1500,
			Driver:     "bridge",
			Type:       "ether",
			OperState:  "up",
			Carrier:    true,
		},
	}
}

func generateLargeServerDisks(count int) []*performance.DiskInfo {
	disks := make([]*performance.DiskInfo, count)
	for i := 0; i < count; i++ {
		diskType := "sata"
		rotational := true
		sizeBytes := uint64(4000787030016) // 4TB
		scheduler := "mq-deadline"

		if i < 4 {
			diskType = "nvme"
			rotational = false
			sizeBytes = 1000204886016 // 1TB
			scheduler = "none"
		}

		disks[i] = &performance.DiskInfo{
			Device:     diskType + string(rune('a'+i)),
			Model:      "Server Disk",
			SizeBytes:  sizeBytes,
			Rotational: rotational,
			Scheduler:  scheduler,
		}
	}
	return disks
}

func generateManyNetworkInterfaces(count int) []*performance.NetworkInfo {
	interfaces := make([]*performance.NetworkInfo, count)
	for i := 0; i < count; i++ {
		speed := uint64(1000)
		if i < 4 {
			speed = 10000
		}

		interfaces[i] = &performance.NetworkInfo{
			Interface:  "eth" + string(rune('0'+i)),
			MACAddress: "00:11:22:33:44:" + string(rune('0'+i)),
			Speed:      speed,
			Duplex:     "full",
			MTU:        1500,
			Driver:     "e1000e",
			Type:       "ether",
			OperState:  "up",
			Carrier:    true,
		}
	}
	return interfaces
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
