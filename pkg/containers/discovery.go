// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

// Package containers provides container discovery and information management
// for various container runtimes in Linux environments.
package containers

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

// Container represents a discovered container with its cgroup location and runtime information.
// This structure provides the necessary information to collect resource metrics for a container.
type Container struct {
	// ID is the container identifier (may be truncated for some runtimes)
	ID string
	// Runtime is the detected container runtime (docker, containerd, crio, etc.)
	Runtime string
	// CgroupPath is the full path to the container's cgroup directory
	CgroupPath string
	// CgroupVersion indicates whether this container uses cgroup v1 or v2
	// Note: Different runtimes on the same host may use different cgroup versions
	CgroupVersion int
}

// Discovery provides methods to discover containers in cgroup hierarchies.
// It handles multiple container runtimes and both cgroup v1 and v2.
type Discovery struct {
	cgroupPath string
}

// NewDiscovery creates a new container discovery instance for the given cgroup root path.
func NewDiscovery(cgroupPath string) *Discovery {
	return &Discovery{
		cgroupPath: cgroupPath,
	}
}

// DetectCgroupVersion determines if we're using cgroup v1 or v2.
// This checks at the root level, but individual containers may use different versions
// if multiple runtimes are present (e.g., Docker with cgroupfs + Podman with cgroup v2).
func (d *Discovery) DetectCgroupVersion() (int, error) {
	// Check for cgroup v2 unified hierarchy
	v2Marker := filepath.Join(d.cgroupPath, "cgroup.controllers")
	if _, err := os.Stat(v2Marker); err == nil {
		return 2, nil
	}

	// Check for cgroup v1 controllers
	v1Controllers := []string{"cpu", "memory", "cpuacct", "blkio", "devices"}
	for _, controller := range v1Controllers {
		controllerPath := filepath.Join(d.cgroupPath, controller)
		if _, err := os.Stat(controllerPath); err == nil {
			return 1, nil
		}
	}

	return 0, fmt.Errorf("unable to detect cgroup version at %s", d.cgroupPath)
}

// DiscoverContainers finds all containers in the cgroup hierarchy.
// For cgroup v1, it searches within the specified subsystem (e.g., "cpu", "memory").
// For cgroup v2, it searches the unified hierarchy and the subsystem parameter is ignored.
//
// The version parameter allows callers to specify which cgroup version to search,
// which is useful when multiple container runtimes use different versions on the same host.
func (d *Discovery) DiscoverContainers(subsystem string, version int) ([]Container, error) {
	var containers []Container

	switch version {
	case 1:
		basePath := filepath.Join(d.cgroupPath, subsystem)
		if _, err := os.Stat(basePath); err != nil {
			return nil, fmt.Errorf("cgroup v1 subsystem %s not found: %w", subsystem, err)
		}
		containers = d.scanCgroupV1Directory(basePath, version)
	case 2:
		containers = d.scanCgroupV2Directory(d.cgroupPath, version)
	default:
		return nil, fmt.Errorf("unsupported cgroup version: %d", version)
	}

	return containers, nil
}

// DiscoverAllContainers attempts to discover containers in both cgroup v1 and v2 hierarchies.
// This is useful when multiple container runtimes are present that may use different cgroup versions.
func (d *Discovery) DiscoverAllContainers() ([]Container, error) {
	var allContainers []Container

	// Try cgroup v2
	v2Containers := d.scanCgroupV2Directory(d.cgroupPath, 2)
	allContainers = append(allContainers, v2Containers...)

	// Try cgroup v1 (check cpu subsystem as it's most commonly used)
	v1Path := filepath.Join(d.cgroupPath, "cpu")
	if _, err := os.Stat(v1Path); err == nil {
		v1Containers := d.scanCgroupV1Directory(v1Path, 1)
		// Deduplicate based on container ID
		seen := make(map[string]bool)
		for _, c := range allContainers {
			seen[c.ID] = true
		}
		for _, c := range v1Containers {
			if !seen[c.ID] {
				allContainers = append(allContainers, c)
			}
		}
	}

	return allContainers, nil
}

func (d *Discovery) scanCgroupV1Directory(basePath string, cgroupVersion int) []Container {
	var containers []Container

	// Common cgroup v1 patterns for different runtimes
	searchPaths := []string{
		filepath.Join(basePath, "docker"),
		filepath.Join(basePath, "containerd"),
		filepath.Join(basePath, "system.slice"),
		filepath.Join(basePath, "crio"),
		filepath.Join(basePath, "machine.slice"),
	}

	for _, searchPath := range searchPaths {
		filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || !info.IsDir() {
				return nil
			}

			// Extract container ID from the path
			relativePath := strings.TrimPrefix(path, basePath)
			if containerID := ExtractContainerID(relativePath); containerID != "" {
				// Detect runtime from the actual path content, not the search directory
				runtime := detectRuntimeFromPath(relativePath)
				containers = append(containers, Container{
					ID:            containerID,
					Runtime:       runtime,
					CgroupPath:    path,
					CgroupVersion: cgroupVersion,
				})
			}
			return nil
		})
	}

	// Also check for containers directly under the base path (some configurations)
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return containers
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Skip system directories
		if name == "." || name == ".." || name == "init.scope" || name == "system.slice" || name == "user.slice" {
			continue
		}

		if containerID := ExtractContainerID(name); containerID != "" {
			containers = append(containers, Container{
				ID:            containerID,
				Runtime:       detectRuntimeFromPath(name),
				CgroupPath:    filepath.Join(basePath, name),
				CgroupVersion: cgroupVersion,
			})
		}
	}

	return containers
}

func (d *Discovery) scanCgroupV2Directory(basePath string, cgroupVersion int) []Container {
	var containers []Container

	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}

		// Skip the root directory
		if path == basePath {
			return nil
		}

		// Look for container patterns in the path
		relativePath := strings.TrimPrefix(path, basePath)
		if containerID := ExtractContainerID(relativePath); containerID != "" {
			// Verify this is actually a container by checking for cgroup.procs
			if _, err := os.Stat(filepath.Join(path, "cgroup.procs")); err == nil {
				containers = append(containers, Container{
					ID:            containerID,
					Runtime:       detectRuntimeFromPath(relativePath),
					CgroupPath:    path,
					CgroupVersion: cgroupVersion,
				})
				// Don't descend into container directories
				return filepath.SkipDir
			}
		}
		return nil
	})

	return containers
}

// detectRuntimeFromPath attempts to identify the container runtime from the cgroup path
func detectRuntimeFromPath(path string) string {
	path = strings.ToLower(path)
	switch {
	case strings.Contains(path, "docker"):
		return "docker"
	case strings.Contains(path, "containerd"):
		return "containerd"
	case strings.Contains(path, "crio"):
		return "crio"
	case strings.Contains(path, "podman"):
		return "podman"
	default:
		return "unknown"
	}
}

// ExtractContainerID extracts a container ID from a cgroup path component.
// It handles various runtime-specific naming patterns.
func ExtractContainerID(name string) string {
	// Handle systemd scope units (docker-<id>.scope, crio-<id>.scope)
	if strings.HasSuffix(name, ".scope") {
		parts := strings.SplitN(name, "-", 2)
		if len(parts) == 2 {
			id := strings.TrimSuffix(parts[1], ".scope")
			// For nested paths like cri-containerd-<id>.scope
			if idx := strings.LastIndex(id, "-"); idx > 0 {
				possibleID := id[idx+1:]
				if IsHexString(possibleID) && len(possibleID) >= MinContainerIDLength {
					return possibleID
				}
			}
			// Simple case: docker-<id>.scope
			if IsHexString(id) && len(id) >= MinContainerIDLength {
				return id
			}
		}
	}

	// Handle directory-based paths (/docker/<id>, /containerd/<id>)
	parts := strings.Split(name, "/")
	for i, part := range parts {
		if part == "docker" || part == "containerd" || part == "crio" {
			if i+1 < len(parts) {
				id := parts[i+1]
				if IsHexString(id) && len(id) >= MinContainerIDLength {
					return id
				}
			}
		}
	}

	// Check if the last path component is a container ID (for Kubernetes pods)
	// This handles paths like /kubepods.slice/kubepods-pod123.slice/<container-id>
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		if IsHexString(lastPart) && len(lastPart) >= MinContainerIDLength {
			return lastPart
		}
	}

	// Check if the name itself is a container ID
	if IsHexString(name) && len(name) >= MinContainerIDLength {
		return name
	}

	return ""
}

// IsHexString checks if a string contains only hexadecimal characters
func IsHexString(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
