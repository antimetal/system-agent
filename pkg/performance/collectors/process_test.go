// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessCollector(t *testing.T) {
	logger := testr.New(t)
	config := performance.DefaultCollectionConfig()

	// Use actual /proc if available (Linux), otherwise skip
	if _, err := os.Stat("/proc/self/stat"); os.IsNotExist(err) {
		t.Skip("Skipping process collector test: /proc not available")
	}

	collector := NewProcessCollector(logger, config)
	require.NotNil(t, collector)

	// Test capabilities
	caps := collector.Capabilities()
	assert.True(t, caps.SupportsOneShot)
	assert.True(t, caps.SupportsContinuous)
	assert.False(t, caps.RequiresRoot)

	// Test collection
	ctx := context.Background()
	data, err := collector.Collect(ctx)
	require.NoError(t, err)

	processes, ok := data.([]*performance.ProcessStats)
	require.True(t, ok, "expected []*performance.ProcessStats, got %T", data)
	assert.NotEmpty(t, processes, "should have collected at least one process")

	// Verify we got our own process
	selfPID := int32(os.Getpid())
	foundSelf := false
	for _, proc := range processes {
		assert.NotEmpty(t, proc.Command, "process should have a command name")
		assert.NotEmpty(t, proc.State, "process should have a state")
		assert.Greater(t, proc.MemoryRSS, uint64(0), "process should have RSS memory")

		if proc.PID == selfPID {
			foundSelf = true
		}
	}
	assert.True(t, foundSelf, "should have found our own process")

	// Test that we respect the top N limit
	assert.LessOrEqual(t, len(processes), defaultTopProcessCount)
}

func TestProcessCollectorParseStat(t *testing.T) {
	logger := testr.New(t)
	config := performance.DefaultCollectionConfig()
	collector := NewProcessCollector(logger, config)

	// Sample stat line (simplified for testing)
	// Format: pid (comm) state ppid pgrp session tty_nr tpgid flags minflt cminflt majflt cmajflt utime stime cutime cstime priority nice num_threads itrealvalue starttime vsize rss...
	statData := `1234 (test process) S 1000 1234 1234 0 -1 4194560 1000 0 10 0 500 300 0 0 20 0 2 0 12345 1073741824 2048 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 1 0 0 0 0 0 0 0 0 0 0 0 0 0`

	stats := &performance.ProcessStats{PID: 1234}
	err := collector.parseStat(stats, statData, 1.0)
	require.NoError(t, err)

	assert.Equal(t, "test process", stats.Command)
	assert.Equal(t, "S", stats.State)
	assert.Equal(t, int32(1000), stats.PPID)
	assert.Equal(t, int32(1234), stats.PGID)
	assert.Equal(t, int32(1234), stats.SID)
	assert.Equal(t, uint64(1000), stats.MinorFaults)
	assert.Equal(t, uint64(10), stats.MajorFaults)
	assert.Equal(t, uint64(800), stats.CPUTime) // 500 + 300
	assert.Equal(t, int32(20), stats.Priority)
	assert.Equal(t, int32(0), stats.Nice)
	assert.Equal(t, int32(2), stats.Threads)
	assert.Equal(t, uint64(1073741824), stats.MemoryVSZ)
	assert.Equal(t, uint64(2048*pageSize), stats.MemoryRSS)
}

func TestProcessCollectorParseStatus(t *testing.T) {
	logger := testr.New(t)
	config := performance.DefaultCollectionConfig()
	collector := NewProcessCollector(logger, config)

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
	collector.parseStatus(stats, statusData)

	assert.Equal(t, int32(4), stats.NumThreads)
	assert.Equal(t, uint64(100), stats.VoluntaryCtxt)
	assert.Equal(t, uint64(50), stats.InvoluntaryCtxt)
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
	collector := NewProcessCollector(logger, config)

	// Test collection
	ctx := context.Background()
	data, err := collector.Collect(ctx)
	require.NoError(t, err)

	processes, ok := data.([]*performance.ProcessStats)
	require.True(t, ok)
	require.Len(t, processes, 1)

	proc := processes[0]
	assert.Equal(t, int32(1234), proc.PID)
	assert.Equal(t, "mock_process", proc.Command)
	assert.Equal(t, "R", proc.State)
	assert.Equal(t, int32(1), proc.PPID)
	assert.Equal(t, uint64(1500), proc.CPUTime) // 1000 + 500
	assert.Equal(t, uint64(1073741824), proc.MemoryVSZ)
	assert.Equal(t, uint64(1024*pageSize), proc.MemoryRSS)
	assert.Equal(t, int32(3), proc.NumFds)
	assert.Equal(t, int32(1), proc.NumThreads)
	assert.Equal(t, uint64(10), proc.VoluntaryCtxt)
	assert.Equal(t, uint64(5), proc.InvoluntaryCtxt)
}
