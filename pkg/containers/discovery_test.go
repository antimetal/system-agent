// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package containers_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/antimetal/agent/pkg/containers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createFile is a test helper to create a file with content
func createFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func TestDiscovery_DetectCgroupVersion(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(t *testing.T, basePath string)
		wantVersion int
		wantErr     bool
	}{
		{
			name: "cgroup v2",
			setupFunc: func(t *testing.T, basePath string) {
				createFile(t, filepath.Join(basePath, "cgroup.controllers"), "cpu io memory")
			},
			wantVersion: 2,
		},
		{
			name: "cgroup v1 with cpu controller",
			setupFunc: func(t *testing.T, basePath string) {
				require.NoError(t, os.MkdirAll(filepath.Join(basePath, "cpu"), 0755))
			},
			wantVersion: 1,
		},
		{
			name: "cgroup v1 with memory controller",
			setupFunc: func(t *testing.T, basePath string) {
				require.NoError(t, os.MkdirAll(filepath.Join(basePath, "memory"), 0755))
			},
			wantVersion: 1,
		},
		{
			name: "no cgroup markers",
			setupFunc: func(t *testing.T, basePath string) {
				// Don't create any markers
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cgroupPath := filepath.Join(tmpDir, "cgroup")
			require.NoError(t, os.MkdirAll(cgroupPath, 0755))

			if tt.setupFunc != nil {
				tt.setupFunc(t, cgroupPath)
			}

			discovery := containers.NewDiscovery(cgroupPath)
			version, err := discovery.DetectCgroupVersion()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantVersion, version)
			}
		})
	}
}

func TestDiscovery_DiscoverContainers(t *testing.T) {
	t.Run("cgroup v1", func(t *testing.T) {
		tmpDir := t.TempDir()
		cgroupPath := filepath.Join(tmpDir, "cgroup")

		// Setup cgroup v1 structure
		cpuPath := filepath.Join(cgroupPath, "cpu")

		// Create docker container
		dockerPath := filepath.Join(cpuPath, "docker", "abc123def456789")
		require.NoError(t, os.MkdirAll(dockerPath, 0755))

		// Create systemd container
		systemdPath := filepath.Join(cpuPath, "system.slice", "docker-fedcba987654321.scope")
		require.NoError(t, os.MkdirAll(systemdPath, 0755))

		// Create containerd container
		containerdPath := filepath.Join(cpuPath, "containerd", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
		require.NoError(t, os.MkdirAll(containerdPath, 0755))

		// Create invalid entries
		require.NoError(t, os.MkdirAll(filepath.Join(cpuPath, "docker", "tooshort"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(cpuPath, "docker", "notahexstring!"), 0755))

		discovery := containers.NewDiscovery(cgroupPath)
		containers, err := discovery.DiscoverContainers("cpu", 1)

		require.NoError(t, err)
		assert.Len(t, containers, 3)

		// Check discovered containers
		foundDocker := false
		foundSystemd := false
		foundContainerd := false

		for _, container := range containers {
			switch container.ID {
			case "abc123def456789":
				foundDocker = true
				assert.Equal(t, "docker", container.Runtime)
			case "fedcba987654321":
				foundSystemd = true
				assert.Equal(t, "docker", container.Runtime)
			case "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef":
				foundContainerd = true
				assert.Equal(t, "containerd", container.Runtime)
			}
		}

		assert.True(t, foundDocker, "Should find docker container")
		assert.True(t, foundSystemd, "Should find systemd-managed docker container")
		assert.True(t, foundContainerd, "Should find containerd container")
	})

	t.Run("cgroup v2", func(t *testing.T) {
		tmpDir := t.TempDir()
		cgroupPath := filepath.Join(tmpDir, "cgroup")

		// Create cgroup v2 marker
		createFile(t, filepath.Join(cgroupPath, "cgroup.controllers"), "cpu io memory")

		// Create docker container in systemd slice
		dockerPath := filepath.Join(cgroupPath, "system.slice", "docker-abc123def456789.scope")
		require.NoError(t, os.MkdirAll(dockerPath, 0755))
		createFile(t, filepath.Join(dockerPath, "cgroup.procs"), "1234\n5678\n")

		// Create kubernetes pod
		kubePath := filepath.Join(cgroupPath, "kubepods.slice", "kubepods-pod123.slice",
			"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
		require.NoError(t, os.MkdirAll(kubePath, 0755))
		createFile(t, filepath.Join(kubePath, "cgroup.procs"), "9012\n3456\n")

		discovery := containers.NewDiscovery(cgroupPath)
		containers, err := discovery.DiscoverContainers("", 2)

		require.NoError(t, err)
		assert.Len(t, containers, 2)

		// Check discovered containers
		foundDocker := false
		foundKube := false

		for _, container := range containers {
			switch container.ID {
			case "abc123def456789":
				foundDocker = true
				assert.Equal(t, "docker", container.Runtime)
			case "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef":
				foundKube = true
				// Kubernetes pods under kubepods.slice might not have runtime info in path
				assert.Contains(t, []string{"containerd", "unknown"}, container.Runtime)
			}
		}

		assert.True(t, foundDocker, "Should find docker container")
		assert.True(t, foundKube, "Should find kubernetes container")
	})
}

func TestExtractContainerID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "docker scope format",
			input:    "docker-abc123def456789.scope",
			expected: "abc123def456789",
		},
		{
			name:     "cri-containerd format",
			input:    "cri-containerd-abc123def456789.scope",
			expected: "abc123def456789",
		},
		{
			name:     "no match",
			input:    "random-string",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containers.ExtractContainerID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsHexString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid hex lowercase",
			input:    "abc123def456",
			expected: true,
		},
		{
			name:     "valid hex uppercase",
			input:    "ABC123DEF456",
			expected: true,
		},
		{
			name:     "valid hex mixed case",
			input:    "aBc123DeF456",
			expected: true,
		},
		{
			name:     "invalid with special char",
			input:    "abc123!def456",
			expected: false,
		},
		{
			name:     "invalid with letter outside hex range",
			input:    "abc123ghi456",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containers.IsHexString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
