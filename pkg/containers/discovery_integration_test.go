// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package containers

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscovery_Integration_RealCgroups(t *testing.T) {
	// Integration test that discovers real containers on the system

	t.Run("DiscoverSystemContainers", func(t *testing.T) {
		// Check if we have access to cgroups
		if _, err := os.Stat("/sys/fs/cgroup"); os.IsNotExist(err) {
			t.Skip("Cgroup filesystem not available")
		}

		discovery := NewDiscovery("/sys/fs/cgroup")
		containers, err := discovery.DiscoverAllContainers()
		require.NoError(t, err, "Should be able to discover containers")

		t.Logf("Discovered %d containers on the system", len(containers))

		// If we're running in a container, we should at least find ourselves
		if isRunningInContainer() {
			assert.NotEmpty(t, containers, "Should find at least our own container")
		}

		// Log details about discovered containers
		for _, container := range containers {
			t.Logf("Container: ID=%s, Runtime=%s, CgroupVersion=%d, Path=%s",
				container.ID, container.Runtime, container.CgroupVersion, container.CgroupPath)
		}
	})

	t.Run("CgroupVersionDetection", func(t *testing.T) {
		discovery := NewDiscovery("/sys/fs/cgroup")
		version := discovery.DetectCgroupVersion()

		// Should be either v1 or v2
		assert.Contains(t, []int{1, 2}, version, "Should detect valid cgroup version")
		t.Logf("Detected cgroup version: %d", version)

		// Verify detection matches actual system
		if version == 2 {
			// Check for v2 unified hierarchy marker
			controllersPath := "/sys/fs/cgroup/cgroup.controllers"
			_, err := os.Stat(controllersPath)
			assert.NoError(t, err, "Should have cgroup.controllers file for v2")
		} else {
			// Check for v1 subsystem directories
			subsystems := []string{"memory", "cpu", "cpuset"}
			foundSubsystem := false
			for _, subsys := range subsystems {
				if _, err := os.Stat(filepath.Join("/sys/fs/cgroup", subsys)); err == nil {
					foundSubsystem = true
					break
				}
			}
			assert.True(t, foundSubsystem, "Should have subsystem directories for v1")
		}
	})

	t.Run("ContainerResourceLimits", func(t *testing.T) {
		// Test extraction of resource limits from cgroups
		discovery := NewDiscovery("/sys/fs/cgroup")
		containers, err := discovery.DiscoverAllContainers()
		require.NoError(t, err)

		for _, container := range containers {
			// Try to read resource limits
			limits := discovery.ExtractResourceLimits(container.CgroupPath, container.CgroupVersion)
			
			// Log discovered limits
			if limits.CPUQuotaUs != nil {
				t.Logf("Container %s CPU quota: %d us", container.ID, *limits.CPUQuotaUs)
			}
			if limits.MemoryLimitBytes != nil {
				t.Logf("Container %s memory limit: %d bytes", container.ID, *limits.MemoryLimitBytes)
			}
			if limits.CpusetCpus != "" {
				t.Logf("Container %s cpuset.cpus: %s", container.ID, limits.CpusetCpus)
			}
			if limits.CpusetMems != "" {
				t.Logf("Container %s cpuset.mems: %s", container.ID, limits.CpusetMems)
			}
		}
	})
}

func TestDiscovery_Integration_DockerContainers(t *testing.T) {
	// Test specifically for Docker containers if Docker is installed

	t.Run("DockerRuntimeDetection", func(t *testing.T) {
		// Check if Docker is available
		dockerSocket := "/var/run/docker.sock"
		if _, err := os.Stat(dockerSocket); os.IsNotExist(err) {
			t.Skip("Docker not available")
		}

		discovery := NewDiscovery("/sys/fs/cgroup")
		containers, err := discovery.DiscoverAllContainers()
		require.NoError(t, err)

		// Look for Docker containers
		dockerContainers := 0
		for _, container := range containers {
			if container.Runtime.String() == "CONTAINER_RUNTIME_DOCKER" {
				dockerContainers++
				
				// Docker containers should have specific path patterns
				assert.Contains(t, container.CgroupPath, "docker",
					"Docker container should have 'docker' in cgroup path")
			}
		}

		t.Logf("Found %d Docker containers", dockerContainers)
	})
}

func TestDiscovery_Integration_KubernetesContainers(t *testing.T) {
	// Test for Kubernetes containers if running in a K8s environment

	t.Run("KubernetesPodDetection", func(t *testing.T) {
		// Check if we're in a Kubernetes environment
		if _, err := os.Stat("/var/run/secrets/kubernetes.io"); os.IsNotExist(err) {
			t.Skip("Not running in Kubernetes")
		}

		discovery := NewDiscovery("/sys/fs/cgroup")
		containers, err := discovery.DiscoverAllContainers()
		require.NoError(t, err)

		// Look for Kubernetes pod containers
		k8sContainers := 0
		for _, container := range containers {
			// Kubernetes containers typically have kubepods in their path
			if strings.Contains(container.CgroupPath, "kubepods") {
				k8sContainers++
				
				// Should be using CRI runtime
				validRuntimes := []string{
					"CONTAINER_RUNTIME_CRI_CONTAINERD",
					"CONTAINER_RUNTIME_CRI_O",
					"CONTAINER_RUNTIME_CONTAINERD",
				}
				assert.Contains(t, validRuntimes, container.Runtime.String(),
					"K8s container should use CRI-compatible runtime")
			}
		}

		t.Logf("Found %d Kubernetes containers", k8sContainers)
		assert.NotZero(t, k8sContainers, "Should find Kubernetes containers")
	})
}

func TestDiscovery_Integration_PerformanceBaseline(t *testing.T) {
	// Benchmark container discovery performance

	t.Run("DiscoverySpeed", func(t *testing.T) {
		if _, err := os.Stat("/sys/fs/cgroup"); os.IsNotExist(err) {
			t.Skip("Cgroup filesystem not available")
		}

		discovery := NewDiscovery("/sys/fs/cgroup")

		// Warm up
		_, _ = discovery.DiscoverAllContainers()

		// Measure discovery time
		start := time.Now()
		containers, err := discovery.DiscoverAllContainers()
		duration := time.Since(start)

		require.NoError(t, err)
		
		t.Logf("Discovery took %v for %d containers", duration, len(containers))
		
		// Performance baseline: should complete within reasonable time
		// Even with 100+ containers, discovery should be fast
		if len(containers) > 0 {
			perContainer := duration / time.Duration(len(containers))
			t.Logf("Average time per container: %v", perContainer)
			
			// Warning if taking too long per container
			if perContainer > 10*time.Millisecond {
				t.Logf("WARNING: Discovery is slow, taking %v per container", perContainer)
			}
		}
	})
}

// Helper function to check if we're running in a container
func isRunningInContainer() bool {
	// Check for /.dockerenv or other container markers
	markers := []string{
		"/.dockerenv",
		"/run/.containerenv",
	}
	
	for _, marker := range markers {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}

	// Check if we're in a container cgroup
	cgroupData, err := os.ReadFile("/proc/self/cgroup")
	if err == nil {
		cgroupStr := string(cgroupData)
		containerRuntimes := []string{"docker", "containerd", "crio", "podman", "lxc"}
		for _, runtime := range containerRuntimes {
			if strings.Contains(cgroupStr, runtime) {
				return true
			}
		}
	}

	return false
}

// ExtractResourceLimits extracts resource limits from cgroup files
// This would be part of the Discovery implementation
func (d *Discovery) ExtractResourceLimits(cgroupPath string, version int) ResourceLimits {
	limits := ResourceLimits{}
	
	basePath := filepath.Join(d.CgroupPath, cgroupPath)
	
	if version == 2 {
		// cgroup v2 paths
		
		// CPU limits
		cpuMaxPath := filepath.Join(basePath, "cpu.max")
		if data, err := os.ReadFile(cpuMaxPath); err == nil {
			// Parse "quota period" format
			fields := strings.Fields(string(data))
			if len(fields) >= 2 && fields[0] != "max" {
				if quota, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
					quotaUs := int32(quota)
					limits.CPUQuotaUs = &quotaUs
				}
				if period, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					periodUs := int32(period)
					limits.CPUPeriodUs = &periodUs
				}
			}
		}
		
		// Memory limits
		memMaxPath := filepath.Join(basePath, "memory.max")
		if data, err := os.ReadFile(memMaxPath); err == nil {
			memStr := strings.TrimSpace(string(data))
			if memStr != "max" {
				if memBytes, err := strconv.ParseUint(memStr, 10, 64); err == nil {
					limits.MemoryLimitBytes = &memBytes
				}
			}
		}
		
		// CPU affinity
		cpusetPath := filepath.Join(basePath, "cpuset.cpus")
		if data, err := os.ReadFile(cpusetPath); err == nil {
			limits.CpusetCpus = strings.TrimSpace(string(data))
		}
		
		// NUMA affinity
		memsPath := filepath.Join(basePath, "cpuset.mems")
		if data, err := os.ReadFile(memsPath); err == nil {
			limits.CpusetMems = strings.TrimSpace(string(data))
		}
	} else {
		// cgroup v1 paths
		
		// CPU limits
		quotaPath := filepath.Join(d.CgroupPath, "cpu", cgroupPath, "cpu.cfs_quota_us")
		if data, err := os.ReadFile(quotaPath); err == nil {
			if quota, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 32); err == nil && quota > 0 {
				quotaUs := int32(quota)
				limits.CPUQuotaUs = &quotaUs
			}
		}
		
		periodPath := filepath.Join(d.CgroupPath, "cpu", cgroupPath, "cpu.cfs_period_us")
		if data, err := os.ReadFile(periodPath); err == nil {
			if period, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 32); err == nil {
				periodUs := int32(period)
				limits.CPUPeriodUs = &periodUs
			}
		}
		
		// Memory limits
		memPath := filepath.Join(d.CgroupPath, "memory", cgroupPath, "memory.limit_in_bytes")
		if data, err := os.ReadFile(memPath); err == nil {
			if memBytes, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
				// Check if it's not the default unlimited value
				if memBytes < uint64(1<<63) {
					limits.MemoryLimitBytes = &memBytes
				}
			}
		}
		
		// CPU affinity
		cpusetPath := filepath.Join(d.CgroupPath, "cpuset", cgroupPath, "cpuset.cpus")
		if data, err := os.ReadFile(cpusetPath); err == nil {
			limits.CpusetCpus = strings.TrimSpace(string(data))
		}
		
		// NUMA affinity
		memsPath := filepath.Join(d.CgroupPath, "cpuset", cgroupPath, "cpuset.mems")
		if data, err := os.ReadFile(memsPath); err == nil {
			limits.CpusetMems = strings.TrimSpace(string(data))
		}
	}
	
	return limits
}

// ResourceLimits represents container resource constraints
type ResourceLimits struct {
	CPUShares        *int32
	CPUQuotaUs       *int32
	CPUPeriodUs      *int32
	MemoryLimitBytes *uint64
	CpusetCpus       string
	CpusetMems       string
}