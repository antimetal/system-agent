// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// MinContainerIDLength is the minimum length for a valid container ID (first 12 chars of full ID)
	MinContainerIDLength = 12
	// KubernetesContainerIDLength is the expected length of a Kubernetes container ID
	KubernetesContainerIDLength = 64
)

// ContainerPath represents a discovered container with its cgroup location
type ContainerPath struct {
	ID         string
	Runtime    string
	CgroupPath string
}

// ContainerDiscovery provides methods to discover containers in cgroup hierarchies
//
// Container ID extraction patterns by runtime:
//
//	Docker:
//	  Path: /docker/<container_id>
//	  Example: /docker/abc123def456789...
//	  ID: First 12+ hex characters
//
//	Containerd:
//	  Path: /containerd/<container_id>
//	  Example: /containerd/0123456789abcdef...
//	  ID: Full 64-character hex string
//
//	CRI-O:
//	  Path: /crio-<container_id>.scope
//	  Example: /crio-abc123def456789.scope
//	  ID: Characters between "crio-" and ".scope"
//
//	Systemd (Docker):
//	  Path: /docker-<container_id>.scope
//	  Example: /system.slice/docker-abc123def456789.scope
//	  ID: Characters between "docker-" and ".scope"
//
//	Kubernetes (via containerd/CRI-O):
//	  Path: /kubepods.slice/.../cri-containerd-<container_id>.scope
//	  Example: /kubepods.slice/kubepods-pod123.slice/cri-containerd-0123456789abcdef.scope
//	  ID: 64-character hex string after last hyphen
//
// Performance note: Scanning large cgroup hierarchies (100+ containers) may impact
// collection performance. The discovery process walks the filesystem tree which can
// be expensive on systems with many containers. Consider caching discovered paths
// if collection frequency is high.
type ContainerDiscovery struct {
	cgroupPath string
}

// NewContainerDiscovery creates a new container discovery instance
func NewContainerDiscovery(cgroupPath string) *ContainerDiscovery {
	return &ContainerDiscovery{
		cgroupPath: cgroupPath,
	}
}

// DetectCgroupVersion determines if we're using cgroup v1 or v2
func (d *ContainerDiscovery) DetectCgroupVersion() (int, error) {
	// Check for cgroup v2 unified hierarchy
	v2Marker := filepath.Join(d.cgroupPath, "cgroup.controllers")
	if _, err := os.Stat(v2Marker); err == nil {
		return 2, nil
	}

	// Check for cgroup v1 controllers (any of them)
	v1Controllers := []string{"cpu", "memory", "cpuacct", "blkio", "devices"}
	for _, controller := range v1Controllers {
		controllerPath := filepath.Join(d.cgroupPath, controller)
		if _, err := os.Stat(controllerPath); err == nil {
			return 1, nil
		}
	}

	return 0, fmt.Errorf("unable to detect cgroup version at %s", d.cgroupPath)
}

// DiscoverContainers finds all containers by scanning cgroup directories
func (d *ContainerDiscovery) DiscoverContainers(subsystem string, version int) ([]ContainerPath, error) {
	var containers []ContainerPath

	if version == 1 {
		// Cgroup v1: Look in the specified subsystem
		subsystemPath := filepath.Join(d.cgroupPath, subsystem)
		containers = d.scanCgroupV1Directory(subsystemPath)
	} else {
		// Cgroup v2: Scan unified hierarchy
		containers = d.scanCgroupV2Directory(d.cgroupPath)
	}

	return containers, nil
}

// scanCgroupV1Directory scans a cgroup v1 controller directory for containers
func (d *ContainerDiscovery) scanCgroupV1Directory(basePath string) []ContainerPath {
	var containers []ContainerPath

	// Common runtime paths to check
	runtimePaths := map[string]string{
		"docker":     filepath.Join(basePath, "docker"),
		"containerd": filepath.Join(basePath, "containerd"),
		"crio":       filepath.Join(basePath, "crio"),
		"podman":     filepath.Join(basePath, "machine.slice"),
	}

	// Also check systemd slice format
	systemdPath := filepath.Join(basePath, "system.slice")
	if entries, err := os.ReadDir(systemdPath); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "docker-") {
				if id := ExtractContainerID(entry.Name()); id != "" {
					containers = append(containers, ContainerPath{
						ID:         id,
						Runtime:    "docker",
						CgroupPath: filepath.Join(systemdPath, entry.Name()),
					})
				}
			}
		}
	}

	// Check each runtime path
	for runtime, path := range runtimePaths {
		entries, err := os.ReadDir(path)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			// Extract container ID
			id := entry.Name()
			// Skip if it doesn't look like a container ID
			if len(id) < MinContainerIDLength || !IsHexString(id) {
				continue
			}

			containers = append(containers, ContainerPath{
				ID:         id,
				Runtime:    runtime,
				CgroupPath: filepath.Join(path, id),
			})
		}
	}

	return containers
}

// scanCgroupV2Directory scans a cgroup v2 unified hierarchy for containers
func (d *ContainerDiscovery) scanCgroupV2Directory(basePath string) []ContainerPath {
	var containers []ContainerPath

	// In cgroup v2, containers might be in various locations
	// Check system.slice for systemd-managed containers
	systemSlice := filepath.Join(basePath, "system.slice")
	if entries, err := os.ReadDir(systemSlice); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			// Look for patterns like docker-<id>.scope
			if strings.HasPrefix(entry.Name(), "docker-") && strings.HasSuffix(entry.Name(), ".scope") {
				if id := ExtractContainerID(entry.Name()); id != "" {
					containers = append(containers, ContainerPath{
						ID:         id,
						Runtime:    "docker",
						CgroupPath: filepath.Join(systemSlice, entry.Name()),
					})
				}
			}
		}
	}

	// Check for Kubernetes pods
	kubepods := filepath.Join(basePath, "kubepods.slice")
	if _, err := os.Stat(kubepods); err == nil {
		// Walk through kubepods hierarchy
		_ = filepath.Walk(kubepods, func(path string, info os.FileInfo, err error) error {
			if err != nil || !info.IsDir() {
				return nil
			}

			// Look for container directories (usually long hex strings)
			name := filepath.Base(path)
			if len(name) >= KubernetesContainerIDLength && IsHexString(name) {
				containers = append(containers, ContainerPath{
					ID:         name,
					Runtime:    "containerd", // Kubernetes typically uses containerd
					CgroupPath: path,
				})
			}
			return nil
		})
	}

	return containers
}

// ExtractContainerID extracts container ID from various cgroup path formats
func ExtractContainerID(name string) string {
	// Handle docker-<id>.scope format
	if strings.HasPrefix(name, "docker-") && strings.HasSuffix(name, ".scope") {
		id := strings.TrimPrefix(name, "docker-")
		id = strings.TrimSuffix(id, ".scope")
		// Validate that we have a valid container ID (at least 12 hex chars)
		if len(id) >= MinContainerIDLength && IsHexString(id) {
			return id
		}
	}

	// Handle containerd format
	if strings.Contains(name, "containerd-") {
		parts := strings.Split(name, "containerd-")
		if len(parts) > 1 {
			id := strings.TrimSuffix(parts[1], ".scope")
			// Validate that we have a valid container ID
			if len(id) >= MinContainerIDLength && IsHexString(id) {
				return id
			}
		}
	}

	return ""
}

// IsHexString checks if a string contains only hexadecimal characters
func IsHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
