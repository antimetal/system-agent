// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !integration

package collectors_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessCollector(t *testing.T) {
	logger := testr.New(t)
	config := performance.DefaultCollectionConfig()
	config.Interval = 100 * time.Millisecond // Fast interval for testing

	// Use actual /proc if available (Linux), otherwise skip
	if _, err := os.Stat("/proc/self/stat"); os.IsNotExist(err) {
		t.Skip("Skipping process collector test: /proc not available")
	}

	collector, err := collectors.NewProcessCollector(logger, config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test capabilities
	caps := collector.Capabilities()
	assert.False(t, caps.SupportsOneShot)
	assert.True(t, caps.SupportsContinuous)
	assert.Nil(t, caps.RequiredCapabilities) // No special capabilities required

	// Test continuous collection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	receiver := performance.NewMockReceiver("test-receiver")
	err = collector.Start(ctx, receiver)
	require.NoError(t, err)
	assert.Equal(t, performance.CollectorStatusActive, collector.Status())

	// Wait for at least 2 collections to test CPU percentage calculation
	var collections [][]*performance.ProcessStats
	timeout := time.After(1 * time.Second)

	// Poll the receiver for data
	for len(collections) < 2 {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for process data")
		default:
			calls := receiver.GetAcceptCalls()
			if len(calls) > len(collections) {
				// New data received
				for i := len(collections); i < len(calls); i++ {
					processes, ok := calls[i].Data.([]*performance.ProcessStats)
					require.True(t, ok, "expected []*performance.ProcessStats, got %T", calls[i].Data)
					collections = append(collections, processes)
				}
			}
			time.Sleep(10 * time.Millisecond) // Small delay to avoid busy waiting
		}
	}

	// Verify the second collection has CPU percentages
	secondCollection := collections[1]
	assert.NotEmpty(t, secondCollection, "should have collected at least one process")

	// Verify we got our own process
	selfPID := int32(os.Getpid())
	foundSelf := false
	for _, proc := range secondCollection {
		assert.NotEmpty(t, proc.Command, "process should have a command name")
		assert.NotEmpty(t, proc.State, "process should have a state")
		// Note: Kernel threads and some processes legitimately have 0 RSS

		if proc.PID == selfPID {
			foundSelf = true
			// Our test process should have RSS memory
			assert.Greater(t, proc.MemoryRSS, uint64(0), "test process should have RSS memory")
			// Second collection should have CPU percentage
			assert.GreaterOrEqual(t, proc.CPUPercent, 0.0, "CPU percentage should be non-negative")
		}
	}

	if !foundSelf {
		// Debug: print info about what we collected
		t.Logf("Did not find test process (PID %d) in top %d processes", selfPID, len(secondCollection))
		t.Logf("Top 5 processes by CPU:")
		for i := 0; i < 5 && i < len(secondCollection); i++ {
			t.Logf("  %d. PID %d (%s): %.2f%% CPU",
				i+1, secondCollection[i].PID, secondCollection[i].Command, secondCollection[i].CPUPercent)
		}
		// Note: It's possible the test process has very low CPU usage and doesn't make the top 20
		t.Skip("Test process not in top processes by CPU - this is expected on busy systems")
	}

	// Test that we respect the top N limit
	assert.LessOrEqual(t, len(secondCollection), 20)

	// Test cleanup via context cancellation
	cancel()
	// Give the collector time to clean up
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, performance.CollectorStatusDisabled, collector.Status())
}

func TestProcessCollector_Constructor(t *testing.T) {
	logger := testr.New(t)

	t.Run("valid absolute path", func(t *testing.T) {
		tmpDir := t.TempDir()
		config := performance.DefaultCollectionConfig()
		config.HostProcPath = tmpDir

		collector, err := collectors.NewProcessCollector(logger, config)
		assert.NoError(t, err)
		assert.NotNil(t, collector)
	})

	t.Run("relative proc path rejected", func(t *testing.T) {
		config := performance.DefaultCollectionConfig()
		config.HostProcPath = "proc"

		collector, err := collectors.NewProcessCollector(logger, config)
		assert.Error(t, err)
		assert.Nil(t, collector)
		assert.Contains(t, err.Error(), "HostProcPath must be an absolute path")
	})

	t.Run("sys path not required", func(t *testing.T) {
		tmpDir := t.TempDir()
		config := performance.DefaultCollectionConfig()
		config.HostProcPath = tmpDir
		config.HostSysPath = "" // empty since not required

		collector, err := collectors.NewProcessCollector(logger, config)
		assert.NoError(t, err)
		assert.NotNil(t, collector)
	})

	t.Run("empty path rejected", func(t *testing.T) {
		config := performance.DefaultCollectionConfig()
		config.HostProcPath = ""

		collector, err := collectors.NewProcessCollector(logger, config)
		assert.Error(t, err)
		assert.Nil(t, collector)
		assert.Contains(t, err.Error(), "HostProcPath is required but not provided")
	})

	t.Run("success on non-existent path", func(t *testing.T) {
		// ProcessCollector doesn't validate actual path existence in constructor
		config := performance.DefaultCollectionConfig()
		config.HostProcPath = "/this/path/does/not/exist"
		config.HostSysPath = "/also/does/not/exist"

		collector, err := collectors.NewProcessCollector(logger, config)
		assert.NoError(t, err)
		assert.NotNil(t, collector)
	})

}

func TestProcessCollectorParseStatFull(t *testing.T) {
	logger := testr.New(t)
	config := performance.DefaultCollectionConfig()
	config.HostProcPath = t.TempDir()
	collector, err := collectors.NewProcessCollector(logger, config)
	require.NoError(t, err)

	// Sample stat line (simplified for testing)
	// Format: pid (comm) state ppid pgrp session tty_nr tpgid flags minflt cminflt majflt cmajflt utime stime cutime cstime priority nice num_threads itrealvalue starttime vsize rss...
	statData := `1234 (test process) S 1000 1234 1234 0 -1 4194560 1000 0 10 0 500 300 0 0 20 0 2 0 12345 1073741824 2048 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 1 0 0 0 0 0 0 0 0 0 0 0 0 0`

	stats := &performance.ProcessStats{
		PID:        1234,
		CPUTime:    800,  // Pre-calculated from minimal
		CPUPercent: 10.0, // Pre-calculated from minimal
	}
	err = collector.ParseStatFull(stats, statData)
	require.NoError(t, err)

	assert.Equal(t, "test process", stats.Command)
	assert.Equal(t, "S", stats.State)
	assert.Equal(t, int32(1000), stats.PPID)
	assert.Equal(t, int32(1234), stats.PGID)
	assert.Equal(t, int32(1234), stats.SID)
	assert.Equal(t, uint64(1000), stats.MinorFaults)
	assert.Equal(t, uint64(10), stats.MajorFaults)
	assert.Equal(t, uint64(800), stats.CPUTime) // Should remain unchanged
	assert.Equal(t, 10.0, stats.CPUPercent)     // Should remain unchanged
	assert.Equal(t, int32(20), stats.Priority)
	assert.Equal(t, int32(0), stats.Nice)
	assert.Equal(t, int32(2), stats.Threads)
	assert.Equal(t, uint64(1073741824), stats.MemoryVSZ)
	// Page size is typically 4096, but we can't assume that in tests
	// Just verify RSS was parsed correctly (2048 pages)
	assert.Greater(t, stats.MemoryRSS, uint64(0), "RSS should be parsed")
}

func TestProcessCollectorReadMinimalStats(t *testing.T) {
	logger := testr.New(t)
	config := performance.DefaultCollectionConfig()
	config.HostProcPath = t.TempDir()
	collector, err := collectors.NewProcessCollector(logger, config)
	require.NoError(t, err)

	// Create a temp directory for mock /proc
	tmpDir := t.TempDir()
	mockProcDir := filepath.Join(tmpDir, "proc")
	require.NoError(t, os.Mkdir(mockProcDir, 0755))

	// Create a mock process directory
	pid1Dir := filepath.Join(mockProcDir, "1234")
	require.NoError(t, os.Mkdir(pid1Dir, 0755))

	// Create mock stat file
	statData := `1234 (test process) S 1000 1234 1234 0 -1 4194560 1000 0 10 0 500 300 0 0 20 0 2 0 12345 1073741824 2048 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 1 0 0 0 0 0 0 0 0 0 0 0 0 0`
	require.NoError(t, os.WriteFile(filepath.Join(pid1Dir, "stat"), []byte(statData), 0644))

	// Update collector to use mock proc path
	collector.ProcPath = mockProcDir

	// Test without previous CPU times
	lastCPUTimes := make(map[int32]*collectors.ProcessCPUTime)
	minimal, err := collector.ReadMinimalStats(1234, 1.0, lastCPUTimes)
	require.NoError(t, err)

	assert.Equal(t, int32(1234), minimal.PID)
	assert.Equal(t, uint64(800), minimal.CPUTime) // 500 + 300
	assert.Equal(t, 0.0, minimal.CPUPercent)      // No previous data

	// Test with previous CPU times
	lastCPUTimes[1234] = &collectors.ProcessCPUTime{
		TotalTime: 700, // Previous CPU time
		Timestamp: time.Now().Add(-1 * time.Second),
	}
	minimal, err = collector.ReadMinimalStats(1234, 1.0, lastCPUTimes)
	require.NoError(t, err)
	// CPU percent should be (800-700)/100/1.0 * 100 = 100%
	assert.Equal(t, 100.0, minimal.CPUPercent)
}

func TestProcessCollectorParseStatus(t *testing.T) {
	logger := testr.New(t)
	config := performance.DefaultCollectionConfig()
	config.HostProcPath = t.TempDir()
	collector, err := collectors.NewProcessCollector(logger, config)
	require.NoError(t, err)

	statusData := `Name:	test
Umask:	0002
State:	S (sleeping)
Tgid:	1234
Ngid:	0
Pid:	1234
PPid:	1000
TracerPid:	0
Uid:	1000	1000	1000	1000
Gid:	1000	1000	1000	1000
FDSize:	256
Groups:	4 24 27 30 46 120 131 132 1000
Threads: 4
voluntary_ctxt_switches: 100
nonvoluntary_ctxt_switches: 50`

	stats := &performance.ProcessStats{PID: 1234}
	collector.ParseStatus(stats, statusData)

	assert.Equal(t, int32(4), stats.NumThreads)
	assert.Equal(t, uint64(100), stats.VoluntaryCtxt)
	assert.Equal(t, uint64(50), stats.InvoluntaryCtxt)
}

func TestProcessCollectorTopProcessCount(t *testing.T) {
	logger := testr.New(t)
	tmpDir := t.TempDir()

	// Test with custom top process count
	config := performance.DefaultCollectionConfig()
	config.HostProcPath = tmpDir
	config.TopProcessCount = 5

	collector, err := collectors.NewProcessCollector(logger, config)
	require.NoError(t, err)
	assert.Equal(t, 5, collector.TopProcesses)

	// Test with zero (should use default)
	config.TopProcessCount = 0
	config.HostProcPath = tmpDir
	collector, err = collectors.NewProcessCollector(logger, config)
	require.NoError(t, err)
	assert.Equal(t, 20, collector.TopProcesses)

	// Test with negative (should use default)
	config.TopProcessCount = -1
	config.HostProcPath = tmpDir
	collector, err = collectors.NewProcessCollector(logger, config)
	require.NoError(t, err)
	assert.Equal(t, 20, collector.TopProcesses)
}

func TestProcessCollectorWithMockProc(t *testing.T) {
	// Create a temporary directory to mock /proc
	tmpDir := t.TempDir()
	mockProcDir := filepath.Join(tmpDir, "proc")
	require.NoError(t, os.Mkdir(mockProcDir, 0755))

	// Create a mock process directory
	pid1Dir := filepath.Join(mockProcDir, "1234")
	require.NoError(t, os.Mkdir(pid1Dir, 0755))

	// Create mock stat file
	statData := `1234 (mock_process) R 1 1234 1234 0 -1 4194560 100 0 5 0 1000 500 0 0 20 0 1 0 12345 1073741824 1024 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 1 0 0 0 0 0 0 0 0 0 0 0 0 0`
	require.NoError(t, os.WriteFile(filepath.Join(pid1Dir, "stat"), []byte(statData), 0644))

	// Create mock comm file
	require.NoError(t, os.WriteFile(filepath.Join(pid1Dir, "comm"), []byte("mock_process\n"), 0644))

	// Create mock status file
	statusData := `Name:	mock_process
State:	R (running)
Tgid:	1234
Pid:	1234
PPid:	1
Threads: 1
voluntary_ctxt_switches: 10
nonvoluntary_ctxt_switches: 5`
	require.NoError(t, os.WriteFile(filepath.Join(pid1Dir, "status"), []byte(statusData), 0644))

	// Create mock fd directory
	fdDir := filepath.Join(pid1Dir, "fd")
	require.NoError(t, os.Mkdir(fdDir, 0755))
	// Create some fake file descriptors
	for i := 0; i < 3; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(fdDir, fmt.Sprintf("%d", i)), []byte{}, 0644))
	}

	// Create collector with mock proc path
	logger := testr.New(t)
	config := performance.DefaultCollectionConfig()
	config.HostProcPath = mockProcDir
	config.Interval = 100 * time.Millisecond // Fast interval for testing
	collector, err := collectors.NewProcessCollector(logger, config)
	require.NoError(t, err)

	// Test continuous collection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	receiver := performance.NewMockReceiver("test-receiver")
	err = collector.Start(ctx, receiver)
	require.NoError(t, err)

	// Wait for first collection
	timeout := time.After(2 * time.Second)
	var processes []*performance.ProcessStats

	// Poll the receiver for data
	for {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for process data")
		default:
			calls := receiver.GetAcceptCalls()
			if len(calls) > 0 {
				var ok bool
				processes, ok = calls[0].Data.([]*performance.ProcessStats)
				require.True(t, ok)
				require.Len(t, processes, 1)
				goto done
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
done:

	proc := processes[0]
	assert.Equal(t, int32(1234), proc.PID)
	assert.Equal(t, "mock_process", proc.Command)
	assert.Equal(t, "R", proc.State)
	assert.Equal(t, int32(1), proc.PPID)
	assert.Equal(t, uint64(1500), proc.CPUTime) // 1000 + 500
	assert.Equal(t, uint64(1073741824), proc.MemoryVSZ)
	// Page size is typically 4096, but we can't assume that in tests
	assert.Greater(t, proc.MemoryRSS, uint64(0), "RSS should be parsed")
	assert.Equal(t, int32(3), proc.NumFds)
	assert.Equal(t, int32(1), proc.NumThreads)
	assert.Equal(t, uint64(10), proc.VoluntaryCtxt)
	assert.Equal(t, uint64(5), proc.InvoluntaryCtxt)
}
