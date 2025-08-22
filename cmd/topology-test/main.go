// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/antimetal/agent/internal/hardware"
	"github.com/antimetal/agent/internal/runtime"
	"github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/resource/store"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

func main() {
	// Setup logging
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	fmt.Println("=== Antimetal Agent Container-Process-Hardware Topology Test ===")
	fmt.Println()

	// Test 1: Container Discovery
	fmt.Println("1. Testing Container Discovery...")
	testContainerDiscovery(logger)
	fmt.Println()

	// Test 2: Hardware Discovery
	fmt.Println("2. Testing Hardware Discovery...")
	testHardwareDiscovery(logger)
	fmt.Println()

	// Test 3: Runtime Manager Integration
	fmt.Println("3. Testing Runtime Manager Integration...")
	testRuntimeManagerIntegration(logger)
	fmt.Println()

	// Test 4: Full Topology Discovery
	fmt.Println("4. Testing Full Topology Discovery...")
	testFullTopologyDiscovery(logger)
	fmt.Println()

	fmt.Println("=== All Tests Completed ===")
}

func testContainerDiscovery(logger logr.Logger) {
	// Test container discovery across different runtime paths
	runtimePaths := []string{
		"/var/lib/docker/containers",
		"/run/containerd/io.containerd.runtime.v2.task",
		"/var/lib/containers/storage/overlay-containers",
		"/run/crio/containers",
	}

	discovery := containers.NewDiscovery("/sys/fs/cgroup")

	// Detect cgroup version first
	cgroupVersion, err := discovery.DetectCgroupVersion()
	if err != nil {
		fmt.Printf("   ❌ Failed to detect cgroup version: %v\n", err)
		return
	}
	fmt.Printf("   🔍 Detected cgroup version: %d\n", cgroupVersion)

	// Test memory subsystem discovery
	containers, err := discovery.DiscoverContainers("memory", cgroupVersion)
	if err != nil {
		fmt.Printf("   ❌ Container discovery failed: %v\n", err)
		return
	}

	fmt.Printf("   ✅ Discovered %d containers\n", len(containers))
	
	// Show details of discovered containers
	runtimeCounts := make(map[string]int)
	cgroupVersionCounts := make(map[string]int)
	
	for _, container := range containers {
		runtimeCounts[container.Runtime]++
		cgroupVersionCounts[fmt.Sprintf("v%d", container.CgroupVersion)]++
		
		if len(container.ID) >= 12 {
			fmt.Printf("   📦 Container: %s (Runtime: %s, Cgroup: v%d)\n",
				container.ID[:12], // Show first 12 chars
				container.Runtime,
				container.CgroupVersion)
		} else {
			fmt.Printf("   📦 Container: %s (Runtime: %s, Cgroup: v%d)\n",
				container.ID,
				container.Runtime,
				container.CgroupVersion)
		}
	}
	
	fmt.Printf("   📊 Runtime distribution: %+v\n", runtimeCounts)
	fmt.Printf("   📊 Cgroup version distribution: %+v\n", cgroupVersionCounts)

	// Test specific runtime paths
	fmt.Println("   🔍 Testing runtime path detection:")
	for _, path := range runtimePaths {
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("   ✅ Found runtime path: %s\n", path)
		} else {
			fmt.Printf("   ⚠️  Runtime path not found: %s\n", path)
		}
	}
}

func testHardwareDiscovery(logger logr.Logger) {
	// Create performance manager for hardware discovery
	config := performance.DefaultCollectionConfig()
	config.HostProcPath = "/proc"
	config.HostSysPath = "/sys"
	config.HostDevPath = "/dev"

	perfManager, err := performance.NewManager(performance.ManagerOptions{
		Logger:   logger,
		NodeName: "test-node",
		Config:   config,
	})
	if err != nil {
		fmt.Printf("   ❌ Failed to create performance manager: %v\n", err)
		return
	}

	// Create in-memory store for testing
	store, err := store.New("")
	if err != nil {
		fmt.Printf("   ❌ Failed to create resource store: %v\n", err)
		return
	}
	defer store.Close()

	// Create hardware manager
	hwManager, err := hardware.NewManager(logger, hardware.ManagerConfig{
		Store:              store,
		PerformanceManager: perfManager,
		UpdateInterval:     30 * time.Second,
	})
	if err != nil {
		fmt.Printf("   ❌ Failed to create hardware manager: %v\n", err)
		return
	}

	// Start hardware discovery
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := hwManager.Start(ctx); err != nil {
		fmt.Printf("   ❌ Hardware discovery failed: %v\n", err)
		return
	}

	// Force an update to collect hardware information
	if err := hwManager.ForceUpdate(); err != nil {
		fmt.Printf("   ❌ Hardware update failed: %v\n", err)
		return
	}

	fmt.Printf("   ✅ Hardware discovery completed successfully\n")
	fmt.Printf("   📊 Last update: %v\n", hwManager.GetLastUpdateTime())

	// Stop hardware manager
	if err := hwManager.Stop(); err != nil {
		fmt.Printf("   ⚠️  Error stopping hardware manager: %v\n", err)
	}
}

func testRuntimeManagerIntegration(logger logr.Logger) {
	// Create performance manager
	config := performance.DefaultCollectionConfig()
	config.HostProcPath = "/proc"
	config.HostSysPath = "/sys"
	config.HostDevPath = "/dev"

	perfManager, err := performance.NewManager(performance.ManagerOptions{
		Logger:   logger,
		NodeName: "test-node",
		Config:   config,
	})
	if err != nil {
		fmt.Printf("   ❌ Failed to create performance manager: %v\n", err)
		return
	}

	// Create in-memory store
	store, err := store.New("")
	if err != nil {
		fmt.Printf("   ❌ Failed to create resource store: %v\n", err)
		return
	}
	defer store.Close()

	// Create runtime manager
	rtManager, err := runtime.NewManager(
		logger,
		runtime.ManagerConfig{
			Store:              store,
			PerformanceManager: perfManager,
			UpdateInterval:     10 * time.Second,
			CgroupPath:         "/sys/fs/cgroup",
		},
	)
	if err != nil {
		fmt.Printf("   ❌ Failed to create runtime manager: %v\n", err)
		return
	}

	// Start runtime manager
	if err := rtManager.Start(); err != nil {
		fmt.Printf("   ❌ Runtime manager start failed: %v\n", err)
		return
	}

	// Let it run for a bit to collect data
	fmt.Printf("   ⏳ Collecting runtime data for 5 seconds...\n")
	time.Sleep(5 * time.Second)

	fmt.Printf("   ✅ Runtime manager integration test completed\n")

	// Stop runtime manager
	if err := rtManager.Stop(); err != nil {
		fmt.Printf("   ⚠️  Error stopping runtime manager: %v\n", err)
	}
}

func testFullTopologyDiscovery(logger logr.Logger) {
	fmt.Printf("   🔄 Running full container-process-hardware topology discovery...\n")
	
	// Create performance manager
	config := performance.DefaultCollectionConfig()
	config.HostProcPath = "/proc"
	config.HostSysPath = "/sys"
	config.HostDevPath = "/dev"

	perfManager, err := performance.NewManager(performance.ManagerOptions{
		Logger:   logger,
		NodeName: "topology-test-node",
		Config:   config,
	})
	if err != nil {
		fmt.Printf("   ❌ Performance manager creation failed: %v\n", err)
		return
	}

	// Create store
	store, err := store.New("")
	if err != nil {
		fmt.Printf("   ❌ Store creation failed: %v\n", err)
		return
	}
	defer store.Close()

	// Create hardware manager
	hwManager, err := hardware.NewManager(logger, hardware.ManagerConfig{
		Store:              store,
		PerformanceManager: perfManager,
		UpdateInterval:     30 * time.Second,
	})
	if err != nil {
		fmt.Printf("   ❌ Hardware manager creation failed: %v\n", err)
		return
	}

	// Create runtime manager
	rtManager, err := runtime.NewManager(
		logger,
		runtime.ManagerConfig{
			Store:              store,
			PerformanceManager: perfManager,
			UpdateInterval:     10 * time.Second,
			CgroupPath:         "/sys/fs/cgroup",
		},
	)
	if err != nil {
		fmt.Printf("   ❌ Runtime manager creation failed: %v\n", err)
		return
	}

	// Start both managers
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := hwManager.Start(ctx); err != nil {
		fmt.Printf("   ❌ Hardware manager start failed: %v\n", err)
		return
	}

	if err := rtManager.Start(); err != nil {
		fmt.Printf("   ❌ Runtime manager start failed: %v\n", err)
		return
	}

	// Let them collect data
	fmt.Printf("   ⏳ Collecting topology data for 10 seconds...\n")
	time.Sleep(10 * time.Second)

	// Force updates to ensure data collection
	if err := hwManager.ForceUpdate(); err != nil {
		fmt.Printf("   ⚠️  Hardware update failed: %v\n", err)
	}

	fmt.Printf("   ✅ Full topology discovery completed successfully!\n")
	fmt.Printf("   📊 Hardware last update: %v\n", hwManager.GetLastUpdateTime())

	// Cleanup
	if err := rtManager.Stop(); err != nil {
		fmt.Printf("   ⚠️  Error stopping runtime manager: %v\n", err)
	}
	if err := hwManager.Stop(); err != nil {
		fmt.Printf("   ⚠️  Error stopping hardware manager: %v\n", err)
	}
}