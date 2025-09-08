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

func TestCPUCollector_Constructor(t *testing.T) {
	t.Run("valid absolute path", func(t *testing.T) {
		config := performance.CollectionConfig{
			HostProcPath: "/proc",
		}
		logger := logr.Discard()
		collector, err := collectors.NewCPUCollector(logger, config)
		assert.NoError(t, err)
		assert.NotNil(t, collector)
	})

	t.Run("invalid relative path", func(t *testing.T) {
		config := performance.CollectionConfig{
			HostProcPath: "proc",
		}
		logger := logr.Discard()
		collector, err := collectors.NewCPUCollector(logger, config)
		assert.Error(t, err)
		assert.Nil(t, collector)
		assert.Contains(t, err.Error(), "HostProcPath must be an absolute path")
	})
}

func TestCPUCollector_Collect(t *testing.T) {
	tests := []struct {
		name        string
		statContent string
		wantErr     bool
		validate    func(t *testing.T, stats []*performance.CPUStats)
	}{
		{
			name: "basic CPU stats",
			statContent: `cpu  1234 56 789 10000 200 30 40 50 60 70
cpu0 600 30 400 5000 100 15 20 25 30 35
cpu1 634 26 389 5000 100 15 20 25 30 35
intr 1234567 123 456
ctxt 12345678
btime 1234567890
processes 12345
procs_running 2
procs_blocked 0
`,
			wantErr: false,
			validate: func(t *testing.T, stats []*performance.CPUStats) {
				require.Len(t, stats, 3) // cpu, cpu0, cpu1

				// Check aggregate CPU line
				assert.Equal(t, int32(-1), stats[0].CPUIndex)
				assert.Equal(t, uint64(1234), stats[0].User)
				assert.Equal(t, uint64(56), stats[0].Nice)
				assert.Equal(t, uint64(789), stats[0].System)
				assert.Equal(t, uint64(10000), stats[0].Idle)
				assert.Equal(t, uint64(200), stats[0].IOWait)
				assert.Equal(t, uint64(30), stats[0].IRQ)
				assert.Equal(t, uint64(40), stats[0].SoftIRQ)
				assert.Equal(t, uint64(50), stats[0].Steal)
				assert.Equal(t, uint64(60), stats[0].Guest)
				assert.Equal(t, uint64(70), stats[0].GuestNice)

				// Check cpu0
				assert.Equal(t, int32(0), stats[1].CPUIndex)
				assert.Equal(t, uint64(600), stats[1].User)
				assert.Equal(t, uint64(30), stats[1].Nice)
				assert.Equal(t, uint64(400), stats[1].System)
				assert.Equal(t, uint64(5000), stats[1].Idle)

				// Check cpu1
				assert.Equal(t, int32(1), stats[2].CPUIndex)
				assert.Equal(t, uint64(634), stats[2].User)
			},
		},
		{
			name: "older kernel format without steal/guest",
			statContent: `cpu  1234 56 789 10000 200 30 40
cpu0 600 30 400 5000 100 15 20
`,
			wantErr: false,
			validate: func(t *testing.T, stats []*performance.CPUStats) {
				require.Len(t, stats, 2)

				// Check that optional fields are 0
				assert.Equal(t, uint64(0), stats[0].Steal)
				assert.Equal(t, uint64(0), stats[0].Guest)
				assert.Equal(t, uint64(0), stats[0].GuestNice)

			},
		},
		{
			name:        "empty file",
			statContent: "",
			wantErr:     true,
		},
		{
			name: "no CPU lines",
			statContent: `intr 1234567 123 456
ctxt 12345678
btime 1234567890
`,
			wantErr: true,
		},
		{
			name: "malformed CPU line",
			statContent: `cpu invalid data here
`,
			wantErr: true,
		},
		{
			name: "high CPU count",
			statContent: `cpu  1000 0 1000 8000 0 0 0 0 0 0
cpu0 100 0 100 800 0 0 0 0 0 0
cpu1 100 0 100 800 0 0 0 0 0 0
cpu2 100 0 100 800 0 0 0 0 0 0
cpu3 100 0 100 800 0 0 0 0 0 0
cpu4 100 0 100 800 0 0 0 0 0 0
cpu5 100 0 100 800 0 0 0 0 0 0
cpu6 100 0 100 800 0 0 0 0 0 0
cpu7 100 0 100 800 0 0 0 0 0 0
cpu8 100 0 100 800 0 0 0 0 0 0
cpu9 100 0 100 800 0 0 0 0 0 0
`,
			wantErr: false,
			validate: func(t *testing.T, stats []*performance.CPUStats) {
				require.Len(t, stats, 11) // cpu + cpu0-cpu9

				// Check individual CPUs
				for i := 1; i <= 10; i++ {
					assert.Equal(t, int32(i-1), stats[i].CPUIndex)
				}
			},
		},
		{
			name: "missing CPU indices",
			statContent: `cpu  1000 0 1000 8000 0 0 0 0 0 0
cpu0 250 0 250 2000 0 0 0 0 0 0
cpu2 250 0 250 2000 0 0 0 0 0 0
cpu5 250 0 250 2000 0 0 0 0 0 0
cpu7 250 0 250 2000 0 0 0 0 0 0
`,
			wantErr: false,
			validate: func(t *testing.T, stats []*performance.CPUStats) {
				require.Len(t, stats, 5) // cpu + cpu0, cpu2, cpu5, cpu7

				// Check that we have the expected CPU indices
				cpuIndices := make(map[int32]bool)
				for _, stat := range stats {
					cpuIndices[stat.CPUIndex] = true
				}

				assert.True(t, cpuIndices[-1]) // aggregate
				assert.True(t, cpuIndices[0])
				assert.True(t, cpuIndices[2])
				assert.True(t, cpuIndices[5])
				assert.True(t, cpuIndices[7])

				// Missing indices: 1, 3, 4, 6
				assert.False(t, cpuIndices[1])
				assert.False(t, cpuIndices[3])
				assert.False(t, cpuIndices[4])
				assert.False(t, cpuIndices[6])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tmpDir := t.TempDir()

			// Write test stat file
			statPath := filepath.Join(tmpDir, "stat")
			err := os.WriteFile(statPath, []byte(tt.statContent), 0644)
			require.NoError(t, err)

			// Create collector
			config := performance.CollectionConfig{
				HostProcPath: tmpDir,
			}
			logger := logr.Discard()
			collector, err := collectors.NewCPUCollector(logger, config)
			assert.NoError(t, err)

			// Collect CPU stats
			receiver := performance.NewMockReceiver("test-receiver")
			err = collector.Collect(context.Background(), receiver)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			calls := receiver.GetAcceptCalls()
			require.Len(t, calls, 1, "Expected exactly one Accept call")
			stats, ok := calls[0].Data.([]*performance.CPUStats)
			require.True(t, ok, "Expected []*performance.CPUStats")

			if tt.validate != nil {
				tt.validate(t, stats)
			}
		})
	}
}

func TestCPUCollector_SimpleCollection(t *testing.T) {
	// Create a test environment with a known stat file
	tmpDir := t.TempDir()
	statPath := filepath.Join(tmpDir, "stat")

	content := `cpu  1234 56 789 10000 200 30 40 50 60 70
cpu0 600 30 400 5000 100 15 20 25 30 35
cpu1 634 26 389 5000 100 15 20 25 30 35
intr 1234567 123 456
ctxt 12345678
btime 1234567890
processes 12345
procs_running 2
procs_blocked 0
`
	err := os.WriteFile(statPath, []byte(content), 0644)
	require.NoError(t, err)

	config := performance.CollectionConfig{
		HostProcPath: tmpDir,
	}
	logger := logr.Discard()
	collector, err := collectors.NewCPUCollector(logger, config)
	assert.NoError(t, err)

	// Collect CPU stats
	receiver := performance.NewMockReceiver("test-receiver")
	err = collector.Collect(context.Background(), receiver)
	require.NoError(t, err)

	calls := receiver.GetAcceptCalls()
	require.Len(t, calls, 1, "Expected exactly one Accept call")
	stats, ok := calls[0].Data.([]*performance.CPUStats)
	require.True(t, ok, "Expected []*performance.CPUStats")
	require.Len(t, stats, 3) // cpu, cpu0, cpu1

	// Verify the aggregate CPU line
	assert.Equal(t, int32(-1), stats[0].CPUIndex)
	assert.Equal(t, uint64(1234), stats[0].User)
	assert.Equal(t, uint64(56), stats[0].Nice)
	assert.Equal(t, uint64(789), stats[0].System)
	assert.Equal(t, uint64(10000), stats[0].Idle)

	// Verify cpu0
	assert.Equal(t, int32(0), stats[1].CPUIndex)
	assert.Equal(t, uint64(600), stats[1].User)

	// Verify cpu1
	assert.Equal(t, int32(1), stats[2].CPUIndex)
	assert.Equal(t, uint64(634), stats[2].User)
}
