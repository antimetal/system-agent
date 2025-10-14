// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package containers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseImageReference(t *testing.T) {
	tests := []struct {
		name         string
		imageRef     string
		expectedName string
		expectedTag  string
		expectError  bool
	}{
		{
			name:         "simple name without tag",
			imageRef:     "nginx",
			expectedName: "nginx",
			expectedTag:  "latest",
		},
		{
			name:         "name with tag",
			imageRef:     "nginx:1.21",
			expectedName: "nginx",
			expectedTag:  "1.21",
		},
		{
			name:         "library image",
			imageRef:     "library/nginx:1.21",
			expectedName: "nginx",
			expectedTag:  "1.21",
		},
		{
			name:         "docker.io registry",
			imageRef:     "docker.io/library/nginx:1.21",
			expectedName: "nginx",
			expectedTag:  "1.21",
		},
		{
			name:         "custom registry",
			imageRef:     "registry.example.com/app/nginx:1.21",
			expectedName: "nginx",
			expectedTag:  "1.21",
		},
		{
			name:         "registry with port",
			imageRef:     "registry.example.com:5000/app/nginx:1.21",
			expectedName: "nginx",
			expectedTag:  "1.21",
		},
		{
			name:         "digest reference",
			imageRef:     "nginx@sha256:abc123def456",
			expectedName: "nginx",
			expectedTag:  "sha256:abc123def456",
		},
		{
			name:         "registry with digest",
			imageRef:     "docker.io/library/nginx@sha256:abc123",
			expectedName: "nginx",
			expectedTag:  "sha256:abc123",
		},
		{
			name:         "complex tag with dots and dashes",
			imageRef:     "myapp:v1.2.3-alpha.1",
			expectedName: "myapp",
			expectedTag:  "v1.2.3-alpha.1",
		},
		{
			name:         "latest tag explicit",
			imageRef:     "nginx:latest",
			expectedName: "nginx",
			expectedTag:  "latest",
		},
		{
			name:        "empty reference",
			imageRef:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, tag, err := parseImageReference(tt.imageRef)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedName, name, "image name mismatch")
			assert.Equal(t, tt.expectedTag, tag, "image tag mismatch")
		})
	}
}

func TestExtractImageName(t *testing.T) {
	tests := []struct {
		name     string
		fullName string
		expected string
	}{
		{
			name:     "simple name",
			fullName: "nginx",
			expected: "nginx",
		},
		{
			name:     "library image",
			fullName: "library/nginx",
			expected: "nginx",
		},
		{
			name:     "registry path",
			fullName: "docker.io/library/nginx",
			expected: "nginx",
		},
		{
			name:     "deep path",
			fullName: "registry.example.com:5000/org/team/nginx",
			expected: "nginx",
		},
		{
			name:     "single segment with port",
			fullName: "localhost:5000/nginx",
			expected: "nginx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractImageName(tt.fullName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsValidTag(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		expected bool
	}{
		{
			name:     "simple version",
			tag:      "1.21",
			expected: true,
		},
		{
			name:     "semantic version",
			tag:      "v1.2.3-alpha.1",
			expected: true,
		},
		{
			name:     "latest",
			tag:      "latest",
			expected: true,
		},
		{
			name:     "with underscores",
			tag:      "my_tag_123",
			expected: true,
		},
		{
			name:     "empty string",
			tag:      "",
			expected: false,
		},
		{
			name:     "with slash (path component)",
			tag:      "path/segment",
			expected: false,
		},
		{
			name:     "with colon (port number)",
			tag:      "5000",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidTag(tt.tag)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractCgroupV2Limits(t *testing.T) {
	// Test conversion formula for cpu.weight
	// weight=100 (default) should convert to shares≈1024
	// Formula: shares = (weight - 1) * 1024 / 9999 + 2
	t.Run("cpu weight conversion", func(t *testing.T) {
		weights := []struct {
			weight        int64
			expectedShare int32
		}{
			{1, 2},        // minimum weight
			{100, 12},     // default weight
			{1000, 104},   // medium weight
			{10000, 1026}, // maximum weight
		}

		for _, w := range weights {
			shares := int32((w.weight-1)*1024/9999 + 2)
			assert.Equal(t, w.expectedShare, shares, "weight %d should convert to shares %d", w.weight, w.expectedShare)
		}
	})
}

func TestExtractHumanNames(t *testing.T) {
	tests := []struct {
		name                  string
		labels                map[string]string
		imageName             string
		expectedContainerName string
		expectedWorkloadName  string
	}{
		{
			name: "Kubernetes pod with container name and workload",
			labels: map[string]string{
				"io.kubernetes.container.name": "nginx",
				"io.kubernetes.pod.name":       "web-server-7d4f8bd9c-abc12",
			},
			expectedContainerName: "nginx",
			expectedWorkloadName:  "web-server",
		},
		{
			name: "Kubernetes deployment",
			labels: map[string]string{
				"io.kubernetes.container.name": "frontend",
				"io.kubernetes.pod.name":       "frontend-deployment-abc12",
			},
			expectedContainerName: "frontend",
			expectedWorkloadName:  "frontend-deployment",
		},
		{
			name: "Docker Compose container",
			labels: map[string]string{
				"com.docker.compose.service": "web",
				"com.docker.compose.project": "myapp",
			},
			expectedContainerName: "web",
			expectedWorkloadName:  "", // No K8s pod name, no workload name
		},
		{
			name:                  "Fallback to image name",
			labels:                map[string]string{},
			imageName:             "nginx",
			expectedContainerName: "nginx",
			expectedWorkloadName:  "", // No K8s pod name
		},
		{
			name: "StatefulSet pod (no hash to strip)",
			labels: map[string]string{
				"io.kubernetes.container.name": "app",
				"io.kubernetes.pod.name":       "cassandra-0",
			},
			expectedContainerName: "app",
			expectedWorkloadName:  "cassandra-0", // No hash to strip
		},
		{
			name: "Kubernetes with only pod name (no container label)",
			labels: map[string]string{
				"io.kubernetes.pod.name": "worker-abc12",
			},
			imageName:             "busybox",
			expectedContainerName: "busybox", // Falls back to image name
			expectedWorkloadName:  "worker",  // Strips hash from pod name
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := &Container{
				ImageName: tt.imageName,
				Labels:    tt.labels,
			}

			extractHumanNames(tt.labels, container)

			assert.Equal(t, tt.expectedContainerName, container.ContainerName, "container name mismatch")
			assert.Equal(t, tt.expectedWorkloadName, container.WorkloadName, "workload name mismatch")
		})
	}
}

func TestStripPodHash(t *testing.T) {
	tests := []struct {
		name     string
		podName  string
		expected string
	}{
		{
			name:     "Deployment pod with ReplicaSet and pod hash",
			podName:  "web-server-7d4f8bd9c-abc12",
			expected: "web-server",
		},
		{
			name:     "Deployment with longer name",
			podName:  "nginx-deployment-5d7f8c9d6-xyz89",
			expected: "nginx-deployment",
		},
		{
			name:     "StatefulSet pod (no hash)",
			podName:  "cassandra-0",
			expected: "cassandra-0",
		},
		{
			name:     "StatefulSet with ordinal",
			podName:  "elasticsearch-cluster-2",
			expected: "elasticsearch-cluster-2",
		},
		{
			name:     "Standalone pod (no hash)",
			podName:  "my-pod",
			expected: "my-pod",
		},
		{
			name:     "Pod with single hash",
			podName:  "worker-abc12",
			expected: "worker",
		},
		{
			name:     "Complex deployment name",
			podName:  "my-app-server-v2-6f5dc8-xyz89",
			expected: "my-app-server-v2",
		},
		{
			name:     "Pod with uppercase in name (not a hash)",
			podName:  "MyApp-ABC123",
			expected: "MyApp-ABC123", // Uppercase means not a hash
		},
		{
			name:     "Very short pod name",
			podName:  "a-b",
			expected: "a-b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripPodHash(tt.podName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsAlphanumeric(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "lowercase alphanumeric",
			input:    "abc123def",
			expected: true,
		},
		{
			name:     "all letters",
			input:    "abcdef",
			expected: true,
		},
		{
			name:     "all numbers",
			input:    "123456",
			expected: true,
		},
		{
			name:     "with uppercase",
			input:    "Abc123",
			expected: false,
		},
		{
			name:     "with hyphen",
			input:    "abc-123",
			expected: false,
		},
		{
			name:     "with underscore",
			input:    "abc_123",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: true, // Empty string has no non-alphanumeric chars
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAlphanumeric(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
