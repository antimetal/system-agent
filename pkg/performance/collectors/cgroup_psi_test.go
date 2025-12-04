// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !integration

package collectors_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reuse PSI test data from psi_test.go
const (
	cgroupValidCPUPSI = `some avg10=0.50 avg60=1.05 avg300=1.50 total=5000000
full avg10=0.00 avg60=0.00 avg300=0.00 total=0`

	cgroupValidMemoryPSI = `some avg10=8.20 avg60=10.50 avg300=12.75 total=8000000000
full avg10=2.10 avg60=3.50 avg300=5.20 total=2000000000`

	cgroupValidIOPSI = `some avg10=15.50 avg60=18.30 avg300=20.10 total=15000000000
full avg10=5.00 avg60=7.50 avg300=10.00 total=5000000000`
)

// TestCgroupPSICollector_Constructor tests cgroup PSI collector initialization
func TestCgroupPSICollector_Constructor(t *testing.T) {
	t.Run("valid configuration", func(t *testing.T) {
		tmpDir := t.TempDir()
		cgroupDir := filepath.Join(tmpDir, "fs", "cgroup")
		require.NoError(t, os.MkdirAll(cgroupDir, 0755))

		// Create cgroup v2 marker
		require.NoError(t, os.WriteFile(filepath.Join(cgroupDir, "cgroup.controllers"), []byte("cpu memory io"), 0644))

		config := performance.CollectionConfig{
			HostSysPath: tmpDir,
		}

		collector, err := collectors.NewCgroupPSICollector(logr.Discard(), config)
		require.NoError(t, err)
		require.NotNil(t, collector)
	})

	t.Run("missing host sys path", func(t *testing.T) {
		config := performance.CollectionConfig{
			HostSysPath: "",
		}

		collector, err := collectors.NewCgroupPSICollector(logr.Discard(), config)
		assert.Error(t, err)
		assert.Nil(t, collector)
	})
}

// TestCgroupPSICollector_CgroupV2 tests collection from cgroup v2
func TestCgroupPSICollector_CgroupV2(t *testing.T) {
	t.Run("collect from single container", func(t *testing.T) {
		tmpDir := t.TempDir()
		cgroupDir := filepath.Join(tmpDir, "fs", "cgroup")
		require.NoError(t, os.MkdirAll(cgroupDir, 0755))

		// Create cgroup v2 marker
		require.NoError(t, os.WriteFile(filepath.Join(cgroupDir, "cgroup.controllers"), []byte("cpu memory io"), 0644))

		// Create a container with PSI files (container IDs must be 12+ hex chars)
		containerID := "abc123def456"
		containerPath := filepath.Join(cgroupDir, "docker", containerID)
		require.NoError(t, os.MkdirAll(containerPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(containerPath, "cgroup.procs"), []byte("1234\n"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(containerPath, "cpu.pressure"), []byte(cgroupValidCPUPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(containerPath, "memory.pressure"), []byte(cgroupValidMemoryPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(containerPath, "io.pressure"), []byte(cgroupValidIOPSI), 0644))

		config := performance.CollectionConfig{HostSysPath: tmpDir}
		collector, err := collectors.NewCgroupPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		event, err := collector.Collect(context.Background())
		require.NoError(t, err)

		stats, ok := event.Data.([]*performance.CgroupPSIStats)
		require.True(t, ok)
		require.Len(t, stats, 1)

		// Verify container stats
		assert.Contains(t, stats[0].ContainerID, containerID)
		assert.NotNil(t, stats[0].CPU)
		assert.NotNil(t, stats[0].Memory)
		assert.NotNil(t, stats[0].IO)

		// Verify CPU pressure
		assert.Equal(t, 0.50, stats[0].CPU.SomeAvg10)
		assert.Equal(t, uint64(5000000), stats[0].CPU.SomeTotal)

		// Verify Memory pressure
		assert.Equal(t, 8.20, stats[0].Memory.SomeAvg10)
		assert.Equal(t, 2.10, stats[0].Memory.FullAvg10)
	})

	t.Run("collect from multiple containers", func(t *testing.T) {
		tmpDir := t.TempDir()
		cgroupDir := filepath.Join(tmpDir, "fs", "cgroup")
		require.NoError(t, os.MkdirAll(cgroupDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(cgroupDir, "cgroup.controllers"), []byte("cpu memory io"), 0644))

		// Container 1 (container IDs must be 12+ hex chars)
		container1Path := filepath.Join(cgroupDir, "docker", "aabbccdd1111")
		require.NoError(t, os.MkdirAll(container1Path, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(container1Path, "cgroup.procs"), []byte("100\n"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(container1Path, "cpu.pressure"), []byte(cgroupValidCPUPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(container1Path, "memory.pressure"), []byte(cgroupValidMemoryPSI), 0644))

		// Container 2
		container2Path := filepath.Join(cgroupDir, "docker", "aabbccdd2222")
		require.NoError(t, os.MkdirAll(container2Path, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(container2Path, "cgroup.procs"), []byte("200\n"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(container2Path, "cpu.pressure"), []byte(cgroupValidCPUPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(container2Path, "io.pressure"), []byte(cgroupValidIOPSI), 0644))

		config := performance.CollectionConfig{HostSysPath: tmpDir}
		collector, err := collectors.NewCgroupPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		event, err := collector.Collect(context.Background())
		require.NoError(t, err)

		stats := event.Data.([]*performance.CgroupPSIStats)
		require.Len(t, stats, 2)
	})

	t.Run("partial PSI files - graceful degradation", func(t *testing.T) {
		tmpDir := t.TempDir()
		cgroupDir := filepath.Join(tmpDir, "fs", "cgroup")
		require.NoError(t, os.MkdirAll(cgroupDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(cgroupDir, "cgroup.controllers"), []byte("cpu memory io"), 0644))

		// Container with only memory.pressure (CPU and IO missing)
		// Container IDs must be 12+ hex chars
		containerPath := filepath.Join(cgroupDir, "docker", "aabbccdd3333")
		require.NoError(t, os.MkdirAll(containerPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(containerPath, "cgroup.procs"), []byte("1\n"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(containerPath, "memory.pressure"), []byte(cgroupValidMemoryPSI), 0644))

		config := performance.CollectionConfig{HostSysPath: tmpDir}
		collector, err := collectors.NewCgroupPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		event, err := collector.Collect(context.Background())
		require.NoError(t, err)

		stats := event.Data.([]*performance.CgroupPSIStats)
		require.Len(t, stats, 1)

		// Should have memory but not CPU/IO
		assert.Nil(t, stats[0].CPU, "CPU PSI should be nil when file missing")
		assert.NotNil(t, stats[0].Memory, "Memory PSI should be present")
		assert.Nil(t, stats[0].IO, "IO PSI should be nil when file missing")
	})

	t.Run("no PSI files - skip container", func(t *testing.T) {
		tmpDir := t.TempDir()
		cgroupDir := filepath.Join(tmpDir, "fs", "cgroup")
		require.NoError(t, os.MkdirAll(cgroupDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(cgroupDir, "cgroup.controllers"), []byte("cpu memory io"), 0644))

		// Container with no PSI files (cgroup v1 scenario)
		// Container IDs must be 12+ hex chars
		containerPath := filepath.Join(cgroupDir, "docker", "aabbccdd4444")
		require.NoError(t, os.MkdirAll(containerPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(containerPath, "cgroup.procs"), []byte("1\n"), 0644))

		config := performance.CollectionConfig{HostSysPath: tmpDir}
		collector, err := collectors.NewCgroupPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		event, err := collector.Collect(context.Background())
		require.NoError(t, err)

		stats := event.Data.([]*performance.CgroupPSIStats)
		assert.Len(t, stats, 0, "Containers without any PSI files should be skipped")
	})
}

// TestCgroupPSICollector_ErrorHandling tests error scenarios
func TestCgroupPSICollector_ErrorHandling(t *testing.T) {
	t.Run("missing cgroup directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		config := performance.CollectionConfig{HostSysPath: tmpDir}
		collector, err := collectors.NewCgroupPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		_, err = collector.Collect(context.Background())
		// Should return error when cgroup directory structure is invalid
		assert.Error(t, err)
	})

	t.Run("malformed PSI file - skip that resource", func(t *testing.T) {
		tmpDir := t.TempDir()
		cgroupDir := filepath.Join(tmpDir, "fs", "cgroup")
		require.NoError(t, os.MkdirAll(cgroupDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(cgroupDir, "cgroup.controllers"), []byte("cpu memory io"), 0644))

		// Container IDs must be 12+ hex chars
		containerPath := filepath.Join(cgroupDir, "docker", "aabbccdd5555")
		require.NoError(t, os.MkdirAll(containerPath, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(containerPath, "cgroup.procs"), []byte("1\n"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(containerPath, "cpu.pressure"), []byte("invalid data"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(containerPath, "memory.pressure"), []byte(cgroupValidMemoryPSI), 0644))

		config := performance.CollectionConfig{HostSysPath: tmpDir}
		collector, err := collectors.NewCgroupPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		event, err := collector.Collect(context.Background())
		require.NoError(t, err)

		stats := event.Data.([]*performance.CgroupPSIStats)
		require.Len(t, stats, 1)

		// CPU should be nil (parse failed), but memory should work
		assert.Nil(t, stats[0].CPU, "Malformed CPU PSI should be skipped")
		assert.NotNil(t, stats[0].Memory, "Valid memory PSI should be parsed")
	})
}
