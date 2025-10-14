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
	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	"os"
	"path/filepath"
	"strings"
)

const (
	// minContainerIDLength is the minimum length for a valid container ID (first 12 chars of full ID)
	minContainerIDLength = 12
)

// ParseContainerRuntime converts a runtime string to a ContainerRuntime enum value
func ParseContainerRuntime(runtime string) runtimev1.ContainerRuntime {
	switch strings.ToLower(runtime) {
	case "docker":
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER
	case "containerd":
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CONTAINERD
	case "cri-containerd":
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CRI_CONTAINERD
	case "cri-o", "crio":
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CRI_O
	case "podman":
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_PODMAN
	default:
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_UNKNOWN
	}
}

// Container represents a discovered container with its cgroup location, runtime information,
// and extracted metadata. This structure provides all necessary information for container
// resource metrics collection and graph building.
type Container struct {
	// Discovery fields - populated during cgroup scanning
	ID            string                     // Container identifier (may be truncated for some runtimes)
	Runtime       runtimev1.ContainerRuntime // Detected container runtime
	CgroupPath    string                     // Full path to the container's cgroup directory
	CgroupVersion int                        // Cgroup version (1 or 2)

	// Image information - extracted from runtime metadata files
	ImageName string
	ImageTag  string

	// Human-readable identifiers (container-specific only)
	// Note: Pod-level fields (pod name, namespace, app) are available in K8s Pod resources
	ContainerName string // "nginx", "web", "sidecar"
	WorkloadName  string // "web-server" (deployment name, hash stripped)

	// Labels - full map containing all K8s/Docker metadata
	Labels map[string]string

	// Resource limits - extracted from cgroup files
	CPUShares        *int32
	CPUQuotaUs       *int32
	CPUPeriodUs      *int32
	MemoryLimitBytes *uint64
	CpusetCpus       string
	CpusetMems       string
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

	// Try cgroup v1 - look for available subsystems instead of assuming "cpu"
	v1Subsystems := []string{"cpu", "memory", "cpuacct", "blkio", "devices"}
	for _, subsystem := range v1Subsystems {
		v1Path := filepath.Join(d.cgroupPath, subsystem)
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
			break // Found a valid subsystem, no need to check others
		}
	}

	return allContainers, nil
}

// scanCgroupV1Directory discovers containers in cgroup v1 hierarchies.
//
// Cgroup v1 container organization is complex because different container runtimes
// organize their cgroup hierarchies differently:
//
// Docker:
//   - Path: /sys/fs/cgroup/*/docker/<container_id>
//   - Full 64-char hex IDs
//
// Containerd (including Kubernetes):
//   - Path: /sys/fs/cgroup/*/system.slice/containerd-<container_id>.scope
//   - Or: /sys/fs/cgroup/*/kubepods.slice/kubepods-<qos>/pod<pod_uid>/<container_id>
//   - Truncated 12-char hex IDs for non-k8s, full IDs for k8s
//
// CRI-O:
//   - Path: /sys/fs/cgroup/*/crio-<container_id>.scope
//   - Or: /sys/fs/cgroup/*/machine.slice/crio-<container_id>.scope
//   - Full 64-char hex IDs
//
// Podman:
//   - Path: /sys/fs/cgroup/*/machine.slice/libpod-<container_id>.scope
//   - Or: /sys/fs/cgroup/*/user.slice/user-<uid>.slice/user@<uid>.service/libpod-<container_id>.scope
//   - Full 64-char hex IDs
//
// The function handles these variations by:
// 1. Searching known runtime-specific paths
// 2. Extracting container IDs using pattern matching
// 3. Detecting runtime from path structure
//
// For detailed documentation on cgroup hierarchies and container runtime
// differences, see: https://github.com/antimetal/system-agent-wiki/blob/main/Cgroup/Container-Discovery.md
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
		_ = filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
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

// scanCgroupV2Directory discovers containers in cgroup v2 unified hierarchy.
//
// Cgroup v2 uses a unified hierarchy which simplifies container discovery compared to v1,
// but different runtimes still organize containers differently:
//
// Docker:
//   - Path: /sys/fs/cgroup/docker/<container_id>
//   - Or: /sys/fs/cgroup/system.slice/docker-<container_id>.scope
//   - Full 64-char hex IDs
//
// Containerd (including Kubernetes):
//   - Path: /sys/fs/cgroup/system.slice/containerd-<container_id>.scope
//   - K8s: /sys/fs/cgroup/kubepods.slice/kubepods-<qos>.slice/kubepods-<qos>-pod<pod_uid>.slice/<runtime>-<container_id>.scope
//   - Where <runtime> is "cri-containerd", "containerd", or "crio"
//   - Truncated or full IDs depending on context
//
// CRI-O:
//   - Path: /sys/fs/cgroup/machine.slice/crio-<container_id>.scope
//   - K8s: Similar to containerd but with "crio" prefix
//   - Full 64-char hex IDs
//
// Podman (rootless):
//   - Path: /sys/fs/cgroup/user.slice/user-<uid>.slice/user@<uid>.service/user.slice/libpod-<container_id>.scope
//   - Rootful: /sys/fs/cgroup/machine.slice/libpod-<container_id>.scope
//   - Full 64-char hex IDs
//
// Systemd-nspawn:
//   - Path: /sys/fs/cgroup/machine.slice/machine-<name>.scope
//   - Names instead of hex IDs
//
// The unified hierarchy means we can walk the entire tree once instead of checking
// multiple subsystem directories like in v1. Container cgroup directories typically
// contain a "cgroup.controllers" file listing available controllers.
//
// For detailed documentation on cgroup v2 unified hierarchy and container organization,
// see: https://github.com/antimetal/system-agent-wiki/blob/main/Cgroup/Container-Discovery-v2.md
func (d *Discovery) scanCgroupV2Directory(basePath string, cgroupVersion int) []Container {
	var containers []Container

	_ = filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
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
func detectRuntimeFromPath(path string) runtimev1.ContainerRuntime {
	path = strings.ToLower(path)

	// Check for CRI (Container Runtime Interface) prefixes first
	// These indicate Kubernetes-managed containers
	if strings.Contains(path, "cri-containerd") {
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CRI_CONTAINERD
	}
	if strings.Contains(path, "cri-o") || strings.Contains(path, "crio") {
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CRI_O
	}

	// Check for standalone runtimes
	switch {
	case strings.Contains(path, "docker"):
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER
	case strings.Contains(path, "containerd"):
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CONTAINERD
	case strings.Contains(path, "podman"):
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_PODMAN
	default:
		return runtimev1.ContainerRuntime_CONTAINER_RUNTIME_UNKNOWN
	}
}

// ExtractContainerID extracts a container ID from a cgroup path component.
// It handles various runtime-specific naming patterns.
func ExtractContainerID(name string) string {
	// Handle systemd scope units (docker-<id>.scope, crio-<id>.scope, cri-containerd-<id>.scope, libpod-<id>.scope)
	if strings.HasSuffix(name, ".scope") {
		// Remove the .scope suffix
		nameWithoutScope := strings.TrimSuffix(name, ".scope")

		// Handle CRI prefixes (cri-containerd-<id>, cri-o-<id>)
		if strings.HasPrefix(nameWithoutScope, "cri-") {
			// Extract everything after the last dash
			if idx := strings.LastIndex(nameWithoutScope, "-"); idx > 0 {
				possibleID := nameWithoutScope[idx+1:]
				if IsHexString(possibleID) && len(possibleID) >= minContainerIDLength {
					return possibleID
				}
			}
		}

		// Handle simple runtime prefixes (docker-<id>, containerd-<id>, libpod-<id>)
		parts := strings.SplitN(nameWithoutScope, "-", 2)
		if len(parts) == 2 {
			id := parts[1]
			// Check if this is a direct container ID
			if IsHexString(id) && len(id) >= minContainerIDLength {
				return id
			}
			// For nested paths, check the last segment
			if idx := strings.LastIndex(id, "-"); idx > 0 {
				possibleID := id[idx+1:]
				if IsHexString(possibleID) && len(possibleID) >= minContainerIDLength {
					return possibleID
				}
			}
		}
	}

	// Handle directory-based paths (/docker/<id>, /containerd/<id>, /podman/<id>)
	parts := strings.Split(name, "/")
	for i, part := range parts {
		if part == "docker" || part == "containerd" || part == "crio" || part == "podman" {
			if i+1 < len(parts) {
				id := parts[i+1]
				if IsHexString(id) && len(id) >= minContainerIDLength {
					return id
				}
			}
		}
	}

	// Check if the last path component is a container ID (for Kubernetes pods)
	// This handles paths like /kubepods.slice/kubepods-pod123.slice/<container-id>
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		if IsHexString(lastPart) && len(lastPart) >= minContainerIDLength {
			return lastPart
		}
	}

	// Check if the name itself is a container ID
	if IsHexString(name) && len(name) >= minContainerIDLength {
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
