// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package containers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
)

// Metadata represents all extractable metadata for a container
type Metadata struct {
	// Image information
	ImageName string
	ImageTag  string

	// Human-readable identifiers (container-specific only)
	// Note: Pod-level fields (pod name, namespace, app) are available in K8s Pod resources
	ContainerName string // "nginx", "web", "sidecar"
	WorkloadName  string // "web-server" (deployment name, hash stripped)

	// Full labels map (contains all K8s/Docker metadata)
	Labels map[string]string

	// Resource limits
	CPUShares        *int32
	CPUQuotaUs       *int32
	CPUPeriodUs      *int32
	MemoryLimitBytes *uint64
	CpusetCpus       string
	CpusetMems       string
}

// ExtractMetadata extracts all available metadata for a container
func ExtractMetadata(container *Container, hostRoot string) (*Metadata, error) {
	metadata := &Metadata{
		Labels: make(map[string]string),
	}

	// Extract image metadata
	imageName, imageTag, err := extractImageMetadata(container, hostRoot)
	if err == nil {
		metadata.ImageName = imageName
		metadata.ImageTag = imageTag
	}

	// Extract resource limits from cgroup files
	extractResourceLimits(container, metadata)

	// Extract labels from runtime metadata
	labels, err := extractLabels(container, hostRoot)
	if err == nil {
		metadata.Labels = labels
		// Extract human-readable names from labels
		extractHumanNames(labels, metadata)
	}

	return metadata, nil
}

// extractImageMetadata extracts image name and tag from runtime metadata files
func extractImageMetadata(container *Container, hostRoot string) (string, string, error) {
	switch container.Runtime {
	case runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER:
		return extractDockerImageMetadata(container.ID, hostRoot)
	case runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CONTAINERD,
		runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CRI_CONTAINERD:
		return extractContainerdImageMetadata(container.ID, hostRoot)
	case runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CRI_O:
		return extractCRIOImageMetadata(container.ID, hostRoot)
	case runtimev1.ContainerRuntime_CONTAINER_RUNTIME_PODMAN:
		return extractPodmanImageMetadata(container.ID, hostRoot)
	default:
		return "", "", fmt.Errorf("unsupported runtime: %v", container.Runtime)
	}
}

// extractDockerImageMetadata reads image metadata from Docker's config.v2.json
func extractDockerImageMetadata(containerID, hostRoot string) (string, string, error) {
	// Docker stores container config at /var/lib/docker/containers/<full-id>/config.v2.json
	// We may only have a truncated ID, so we need to search for matching directories
	dockerRoot := filepath.Join(hostRoot, "/var/lib/docker/containers")

	matches, err := filepath.Glob(filepath.Join(dockerRoot, containerID+"*"))
	if err != nil || len(matches) == 0 {
		return "", "", fmt.Errorf("docker container directory not found for ID: %s", containerID)
	}

	configPath := filepath.Join(matches[0], "config.v2.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read docker config: %w", err)
	}

	var config struct {
		Config struct {
			Image string `json:"Image"`
		} `json:"Config"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return "", "", fmt.Errorf("failed to parse docker config: %w", err)
	}

	return parseImageReference(config.Config.Image)
}

// extractContainerdImageMetadata reads image metadata from containerd's state files
func extractContainerdImageMetadata(containerID, hostRoot string) (string, string, error) {
	// Containerd stores metadata differently depending on whether it's standalone or CRI
	// Try CRI path first: /run/containerd/io.containerd.runtime.v2.task/k8s.io/<id>/config.json
	// Then standalone: /run/containerd/io.containerd.runtime.v2.task/default/<id>/config.json

	namespaces := []string{"k8s.io", "default", "moby"}
	containerdRoot := filepath.Join(hostRoot, "/run/containerd/io.containerd.runtime.v2.task")

	for _, ns := range namespaces {
		configPath := filepath.Join(containerdRoot, ns, containerID, "config.json")
		if data, err := os.ReadFile(configPath); err == nil {
			var config struct {
				Process struct {
					Env []string `json:"env"`
				} `json:"process"`
				Annotations map[string]string `json:"annotations"`
			}

			if err := json.Unmarshal(data, &config); err == nil {
				// Try to get image from annotations (Kubernetes sets this)
				if image, ok := config.Annotations["io.kubernetes.cri.image-name"]; ok {
					return parseImageReference(image)
				}
			}
		}
	}

	return "", "", fmt.Errorf("containerd container config not found for ID: %s", containerID)
}

// extractCRIOImageMetadata reads image metadata from CRI-O's state files
func extractCRIOImageMetadata(containerID, hostRoot string) (string, string, error) {
	// CRI-O stores state at /var/run/crio/containers/<id>/state.json
	crioRoot := filepath.Join(hostRoot, "/var/run/crio/containers")

	// Try exact ID match
	statePath := filepath.Join(crioRoot, containerID, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		// Try glob pattern for truncated IDs
		matches, err := filepath.Glob(filepath.Join(crioRoot, containerID+"*"))
		if err != nil || len(matches) == 0 {
			return "", "", fmt.Errorf("crio container state not found for ID: %s", containerID)
		}
		statePath = filepath.Join(matches[0], "state.json")
		data, err = os.ReadFile(statePath)
		if err != nil {
			return "", "", err
		}
	}

	var state struct {
		Annotations map[string]string `json:"annotations"`
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return "", "", fmt.Errorf("failed to parse crio state: %w", err)
	}

	// CRI-O uses annotations for image info
	if image, ok := state.Annotations["io.kubernetes.cri.image-name"]; ok {
		return parseImageReference(image)
	}

	return "", "", fmt.Errorf("image name not found in crio annotations")
}

// extractPodmanImageMetadata reads image metadata from Podman's config files
func extractPodmanImageMetadata(containerID, hostRoot string) (string, string, error) {
	// Podman rootful: /var/lib/containers/storage/overlay-containers/<id>/userdata/config.json
	// Podman rootless: ~/.local/share/containers/storage/overlay-containers/<id>/userdata/config.json

	storagePaths := []string{
		filepath.Join(hostRoot, "/var/lib/containers/storage/overlay-containers"),
		filepath.Join(hostRoot, "/var/lib/containers/storage/vfs-containers"),
	}

	for _, storagePath := range storagePaths {
		configPath := filepath.Join(storagePath, containerID, "userdata", "config.json")
		data, err := os.ReadFile(configPath)
		if err != nil {
			// Try glob for truncated IDs
			matches, globErr := filepath.Glob(filepath.Join(storagePath, containerID+"*"))
			if globErr == nil && len(matches) > 0 {
				configPath = filepath.Join(matches[0], "userdata", "config.json")
				data, err = os.ReadFile(configPath)
			}
		}

		if err == nil {
			var config struct {
				Config struct {
					Image string `json:"Image"`
				} `json:"Config"`
			}

			if err := json.Unmarshal(data, &config); err == nil {
				return parseImageReference(config.Config.Image)
			}
		}
	}

	return "", "", fmt.Errorf("podman container config not found for ID: %s", containerID)
}

// parseImageReference parses an image reference into name and tag
// Handles various formats:
//   - "nginx" -> ("nginx", "latest")
//   - "nginx:1.21" -> ("nginx", "1.21")
//   - "docker.io/library/nginx:1.21" -> ("nginx", "1.21")
//   - "registry.example.com:5000/app/nginx:1.21" -> ("nginx", "1.21")
//   - "nginx@sha256:abc123..." -> ("nginx", "sha256:abc123...")
func parseImageReference(imageRef string) (string, string, error) {
	if imageRef == "" {
		return "", "", fmt.Errorf("empty image reference")
	}

	// Handle digest references (image@sha256:...)
	if idx := strings.Index(imageRef, "@"); idx > 0 {
		imagePart := imageRef[:idx]
		digest := imageRef[idx+1:]

		// Extract name from image part (remove registry/repo path)
		name := extractImageName(imagePart)
		return name, digest, nil
	}

	// Split on ":" to separate name and tag
	parts := strings.Split(imageRef, ":")

	// Handle cases with registry port (e.g., "registry:5000/image:tag")
	// We need to find the last ":" that's not part of a port number
	var name, tag string
	if len(parts) == 1 {
		// No tag specified
		name = parts[0]
		tag = "latest"
	} else if len(parts) == 2 {
		// Simple case: name:tag
		name = parts[0]
		tag = parts[1]
	} else {
		// Multiple colons - likely registry:port/image:tag
		// Find the last colon that's followed by valid tag characters (not part of a path)
		lastColon := strings.LastIndex(imageRef, ":")
		potentialTag := imageRef[lastColon+1:]

		// If potential tag looks like a tag (alphanumeric + dots/dashes), use it
		if isValidTag(potentialTag) {
			name = imageRef[:lastColon]
			tag = potentialTag
		} else {
			// Otherwise, assume no tag
			name = imageRef
			tag = "latest"
		}
	}

	// Extract just the image name (remove registry and repository path)
	name = extractImageName(name)

	return name, tag, nil
}

// extractImageName extracts the image name from a full image reference
// Examples:
//   - "nginx" -> "nginx"
//   - "library/nginx" -> "nginx"
//   - "docker.io/library/nginx" -> "nginx"
//   - "registry.example.com:5000/app/nginx" -> "nginx"
func extractImageName(fullName string) string {
	// Remove any port number from registry
	// Split on "/" and take the last part
	parts := strings.Split(fullName, "/")
	if len(parts) == 0 {
		return fullName
	}
	return parts[len(parts)-1]
}

// isValidTag checks if a string looks like a valid Docker tag
func isValidTag(s string) bool {
	if s == "" {
		return false
	}
	// Tags can contain alphanumeric, dots, dashes, underscores
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// extractResourceLimits reads cgroup resource limit files and populates the metadata
func extractResourceLimits(container *Container, metadata *Metadata) {
	cgroupPath := container.CgroupPath

	if container.CgroupVersion == 1 {
		extractCgroupV1Limits(cgroupPath, metadata)
	} else if container.CgroupVersion == 2 {
		extractCgroupV2Limits(cgroupPath, metadata)
	}
}

// extractCgroupV1Limits reads resource limits from cgroup v1 files
func extractCgroupV1Limits(cgroupPath string, metadata *Metadata) {
	// CPU limits - need to read from cpu controller cgroup
	// The cgroupPath might be from memory controller, so we need to find the cpu controller path
	cpuPath := strings.Replace(cgroupPath, "/memory/", "/cpu,cpuacct/", 1)
	cpuPath = strings.Replace(cpuPath, "/memory", "/cpu,cpuacct", 1)

	// CPU shares (cpu.shares)
	if data, err := os.ReadFile(filepath.Join(cpuPath, "cpu.shares")); err == nil {
		if val, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 32); err == nil {
			shares := int32(val)
			metadata.CPUShares = &shares
		}
	}

	// CPU quota (cpu.cfs_quota_us)
	if data, err := os.ReadFile(filepath.Join(cpuPath, "cpu.cfs_quota_us")); err == nil {
		if val, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 32); err == nil && val > 0 {
			quota := int32(val)
			metadata.CPUQuotaUs = &quota
		}
	}

	// CPU period (cpu.cfs_period_us)
	if data, err := os.ReadFile(filepath.Join(cpuPath, "cpu.cfs_period_us")); err == nil {
		if val, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 32); err == nil {
			period := int32(val)
			metadata.CPUPeriodUs = &period
		}
	}

	// Cpuset limits
	if data, err := os.ReadFile(filepath.Join(cpuPath, "cpuset.cpus")); err == nil {
		metadata.CpusetCpus = strings.TrimSpace(string(data))
	}

	if data, err := os.ReadFile(filepath.Join(cpuPath, "cpuset.mems")); err == nil {
		metadata.CpusetMems = strings.TrimSpace(string(data))
	}

	// Memory limits
	memPath := cgroupPath
	if !strings.Contains(memPath, "/memory") {
		memPath = strings.Replace(cgroupPath, "/cpu,cpuacct/", "/memory/", 1)
		memPath = strings.Replace(memPath, "/cpu/", "/memory/", 1)
	}

	// Memory limit (memory.limit_in_bytes)
	if data, err := os.ReadFile(filepath.Join(memPath, "memory.limit_in_bytes")); err == nil {
		if val, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
			// Check for max value (no limit)
			if val < uint64(1)<<62 {
				metadata.MemoryLimitBytes = &val
			}
		}
	}
}

// extractCgroupV2Limits reads resource limits from cgroup v2 files
func extractCgroupV2Limits(cgroupPath string, metadata *Metadata) {
	// CPU weight (cpu.weight) - cgroup v2 uses weight instead of shares
	// Convert to shares-equivalent: shares = (weight - 1) * 1024 / 9999 + 2
	if data, err := os.ReadFile(filepath.Join(cgroupPath, "cpu.weight")); err == nil {
		if weight, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
			// Convert weight (1-10000) to shares-equivalent (2-262144)
			shares := int32((weight-1)*1024/9999 + 2)
			metadata.CPUShares = &shares
		}
	}

	// CPU max (cpu.max) - format: "$MAX $PERIOD"
	if data, err := os.ReadFile(filepath.Join(cgroupPath, "cpu.max")); err == nil {
		parts := strings.Fields(strings.TrimSpace(string(data)))
		if len(parts) >= 2 {
			if parts[0] != "max" {
				if quota, err := strconv.ParseInt(parts[0], 10, 32); err == nil {
					q := int32(quota)
					metadata.CPUQuotaUs = &q
				}
			}
			if period, err := strconv.ParseInt(parts[1], 10, 32); err == nil {
				p := int32(period)
				metadata.CPUPeriodUs = &p
			}
		}
	}

	// Cpuset (cpuset.cpus and cpuset.mems)
	if data, err := os.ReadFile(filepath.Join(cgroupPath, "cpuset.cpus")); err == nil {
		metadata.CpusetCpus = strings.TrimSpace(string(data))
	}

	if data, err := os.ReadFile(filepath.Join(cgroupPath, "cpuset.mems")); err == nil {
		metadata.CpusetMems = strings.TrimSpace(string(data))
	}

	// Memory limit (memory.max)
	if data, err := os.ReadFile(filepath.Join(cgroupPath, "memory.max")); err == nil {
		limitStr := strings.TrimSpace(string(data))
		if limitStr != "max" {
			if val, err := strconv.ParseUint(limitStr, 10, 64); err == nil {
				metadata.MemoryLimitBytes = &val
			}
		}
	}
}

// extractLabels reads container labels from runtime metadata files
func extractLabels(container *Container, hostRoot string) (map[string]string, error) {
	switch container.Runtime {
	case runtimev1.ContainerRuntime_CONTAINER_RUNTIME_DOCKER:
		return extractDockerLabels(container.ID, hostRoot)
	case runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CONTAINERD,
		runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CRI_CONTAINERD:
		return extractContainerdLabels(container.ID, hostRoot)
	case runtimev1.ContainerRuntime_CONTAINER_RUNTIME_CRI_O:
		return extractCRIOLabels(container.ID, hostRoot)
	case runtimev1.ContainerRuntime_CONTAINER_RUNTIME_PODMAN:
		return extractPodmanLabels(container.ID, hostRoot)
	default:
		return nil, fmt.Errorf("unsupported runtime: %v", container.Runtime)
	}
}

// extractDockerLabels reads labels from Docker's config.v2.json
func extractDockerLabels(containerID, hostRoot string) (map[string]string, error) {
	dockerRoot := filepath.Join(hostRoot, "/var/lib/docker/containers")
	matches, err := filepath.Glob(filepath.Join(dockerRoot, containerID+"*"))
	if err != nil || len(matches) == 0 {
		return nil, fmt.Errorf("docker container directory not found")
	}

	configPath := filepath.Join(matches[0], "config.v2.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return config.Config.Labels, nil
}

// extractContainerdLabels reads labels from containerd config
func extractContainerdLabels(containerID, hostRoot string) (map[string]string, error) {
	namespaces := []string{"k8s.io", "default", "moby"}
	containerdRoot := filepath.Join(hostRoot, "/run/containerd/io.containerd.runtime.v2.task")

	for _, ns := range namespaces {
		configPath := filepath.Join(containerdRoot, ns, containerID, "config.json")
		if data, err := os.ReadFile(configPath); err == nil {
			var config struct {
				Annotations map[string]string `json:"annotations"`
			}

			if err := json.Unmarshal(data, &config); err == nil {
				return config.Annotations, nil
			}
		}
	}

	return nil, fmt.Errorf("containerd labels not found")
}

// extractCRIOLabels reads labels from CRI-O state
func extractCRIOLabels(containerID, hostRoot string) (map[string]string, error) {
	crioRoot := filepath.Join(hostRoot, "/var/run/crio/containers")
	statePath := filepath.Join(crioRoot, containerID, "state.json")

	data, err := os.ReadFile(statePath)
	if err != nil {
		matches, err := filepath.Glob(filepath.Join(crioRoot, containerID+"*"))
		if err != nil || len(matches) == 0 {
			return nil, err
		}
		statePath = filepath.Join(matches[0], "state.json")
		data, err = os.ReadFile(statePath)
		if err != nil {
			return nil, err
		}
	}

	var state struct {
		Annotations map[string]string `json:"annotations"`
		Labels      map[string]string `json:"labels"`
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	// Merge annotations and labels
	labels := make(map[string]string)
	for k, v := range state.Labels {
		labels[k] = v
	}
	for k, v := range state.Annotations {
		labels[k] = v
	}

	return labels, nil
}

// extractPodmanLabels reads labels from Podman config
func extractPodmanLabels(containerID, hostRoot string) (map[string]string, error) {
	storagePaths := []string{
		filepath.Join(hostRoot, "/var/lib/containers/storage/overlay-containers"),
		filepath.Join(hostRoot, "/var/lib/containers/storage/vfs-containers"),
	}

	for _, storagePath := range storagePaths {
		configPath := filepath.Join(storagePath, containerID, "userdata", "config.json")
		data, err := os.ReadFile(configPath)
		if err != nil {
			matches, globErr := filepath.Glob(filepath.Join(storagePath, containerID+"*"))
			if globErr == nil && len(matches) > 0 {
				configPath = filepath.Join(matches[0], "userdata", "config.json")
				data, err = os.ReadFile(configPath)
			}
		}

		if err == nil {
			var config struct {
				Config struct {
					Labels map[string]string `json:"Labels"`
				} `json:"Config"`
			}

			if err := json.Unmarshal(data, &config); err == nil {
				return config.Config.Labels, nil
			}
		}
	}

	return nil, fmt.Errorf("podman labels not found")
}

// extractHumanNames extracts container-specific human-readable identifiers from labels
// Note: Pod-level fields (pod name, namespace, app) are NOT extracted - they're available
// in Kubernetes Pod resources to avoid duplication
func extractHumanNames(labels map[string]string, metadata *Metadata) {
	// Container name - most important identifier
	// Priority: K8s container name > Docker Compose service > fallback to image name
	if name := labels["io.kubernetes.container.name"]; name != "" {
		metadata.ContainerName = name
	} else if name := labels["com.docker.compose.service"]; name != "" {
		metadata.ContainerName = name
	} else if metadata.ImageName != "" {
		// Fall back to image name if no explicit container name
		metadata.ContainerName = metadata.ImageName
	}

	// Workload name - derived from pod name by stripping K8s-generated hashes
	// Only extract if we have a pod name label (Kubernetes only)
	if podName := labels["io.kubernetes.pod.name"]; podName != "" {
		metadata.WorkloadName = stripPodHash(podName)
	}
}

// stripPodHash removes the ReplicaSet hash suffix from a Kubernetes pod name
// Examples:
//   - "web-server-7d4f8bd9c-abc12" -> "web-server"
//   - "nginx-deployment-abc123def" -> "nginx-deployment"
//   - "standalone-pod" -> "standalone-pod" (no hash to strip)
func stripPodHash(podName string) string {
	// Kubernetes pod names from Deployments/StatefulSets have format:
	// <workload-name>-<replicaset-hash>-<pod-hash>
	// We want to strip both hashes to get the workload name

	// Split by hyphens
	parts := strings.Split(podName, "-")

	// Need at least 2 parts to have any hash
	if len(parts) < 2 {
		return podName
	}

	// Check if last parts look like hashes
	// Kubernetes hashes are lowercase alphanumeric, typically 3-10 chars
	lastPart := parts[len(parts)-1]
	secondLastPart := ""
	if len(parts) >= 3 {
		secondLastPart = parts[len(parts)-2]
	}

	lastIsHash := isKubernetesHash(lastPart)
	secondLastIsHash := secondLastPart != "" && isKubernetesHash(secondLastPart)

	if lastIsHash && secondLastIsHash {
		// Strip both hashes (Deployment: name-replicaset-pod)
		return strings.Join(parts[:len(parts)-2], "-")
	} else if lastIsHash {
		// Strip just the last hash
		return strings.Join(parts[:len(parts)-1], "-")
	}

	// No recognizable hash pattern, return as-is
	return podName
}

// isKubernetesHash checks if a string looks like a Kubernetes-generated hash
// K8s hashes are lowercase alphanumeric, typically 5-10 characters, with both letters and numbers
func isKubernetesHash(s string) bool {
	length := len(s)
	// Kubernetes hashes are typically 5-10 characters
	// (ReplicaSet hash: 5-10 chars, Pod hash: 5 chars)
	if length < 5 || length > 10 {
		return false
	}

	if !isAlphanumeric(s) {
		return false
	}

	// Must have BOTH letters and numbers to be a hash (not just "myapp" or "12345")
	hasLetter := false
	hasDigit := false
	for _, c := range s {
		if c >= 'a' && c <= 'z' {
			hasLetter = true
		} else if c >= '0' && c <= '9' {
			hasDigit = true
		}
		if hasLetter && hasDigit {
			return true
		}
	}

	return false
}

// isAlphanumeric checks if a string contains only lowercase alphanumeric characters
func isAlphanumeric(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}
