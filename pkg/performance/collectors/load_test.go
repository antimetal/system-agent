// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// Valid scenarios
	validLoadavgContent    = "0.50 1.25 2.75 2/1234 12345"
	validUptimeContent     = "1234.56 5678.90"
	extraWhitespaceContent = "  0.50   1.25   2.75   2/1234   12345  "

	// Boundary conditions
	highLoadContent = "15.80 10.45 8.32 5/2048 98765"
	zeroLoadContent = "0.00 0.00 0.00 1/100 1"

	// Error conditions
	malformedLoadContent = "0.50 1.25"
	invalidFloatContent  = "invalid 1.25 2.75 2/1234 12345"
	invalidProcContent   = "0.50 1.25 2.75 invalid_procs 12345"
	invalidPIDContent    = "0.50 1.25 2.75 2/1234 invalid_pid"
	whitespaceContent    = "   \n   \t   "

	// Valid /proc/stat content with blocked processes
	validStatWithBlockedContent = `cpu  1234567 0 2345678 98765432 123456 78901 234567 0 0 0
cpu0 617283 0 1172839 49382716 61728 39450 117283 0 0 0
cpu1 617284 0 1172839 49382716 61728 39451 117284 0 0 0
intr 123456789 1234 5678
ctxt 987654321
btime 1638360000
processes 1234567
procs_running 4
procs_blocked 7
softirq 11111111 0 2222222 0 3333333 0 0 4444444 0 1111111 0
`

	// /proc/stat with only procs_blocked
	blockedOnlyStatContent = `procs_blocked 10
`

	// /proc/stat with zero blocked processes
	zeroBlockedStatContent = `cpu  1234567 0 2345678 98765432 123456 78901 234567 0 0 0
procs_running 4
procs_blocked 0
`

	// /proc/stat with max int32 blocked processes
	maxBlockedStatContent = `procs_blocked 2147483647
`

	// /proc/stat with invalid blocked value
	invalidBlockedStatContent = `procs_blocked invalid_number
`

	// /proc/stat with missing value
	missingBlockedValueStatContent = `procs_blocked
`

	// /proc/stat with extra whitespace
	extraWhitespaceBlockedStatContent = `procs_blocked    15    
`

	// /proc/stat with tab separation
	tabSeparatedBlockedStatContent = `procs_blocked	20
`

	// /proc/stat without procs_blocked line
	noBlockedStatContent = `cpu  1234567 0 2345678 98765432 123456 78901 234567 0 0 0
procs_running 4
`

	// empty /proc/stat
	emptyBlockedStatContent = ``

	// /proc/stat with multiple values on procs_blocked line
	multipleValuesStatContent = `procs_blocked 5 extra_value
`

	// /proc/stat with negative value (invalid but test parsing)
	negativeBlockedStatContent = `procs_blocked -5
`

	// /proc/stat with float value (invalid but test parsing)
	floatBlockedStatContent = `procs_blocked 3.14
`
)

func createTestLoadCollector(t *testing.T, loadavgContent, uptimeContent string) *collectors.LoadCollector {
	tmpDir := t.TempDir()

	if loadavgContent != "" {
		loadavgPath := filepath.Join(tmpDir, "loadavg")
		err := os.WriteFile(loadavgPath, []byte(loadavgContent), 0644)
		require.NoError(t, err)
	}

	if uptimeContent != "" {
		uptimePath := filepath.Join(tmpDir, "uptime")
		err := os.WriteFile(uptimePath, []byte(uptimeContent), 0644)
		require.NoError(t, err)
	}

	config := performance.CollectionConfig{
		HostProcPath: tmpDir,
	}
	collector, err := collectors.NewLoadCollector(logr.Discard(), config)
	require.NoError(t, err)
	return collector
}

func createTestLoadCollectorWithStat(t *testing.T, loadavgContent, uptimeContent, statContent string) *collectors.LoadCollector {
	tmpDir := t.TempDir()

	if loadavgContent != "" {
		loadavgPath := filepath.Join(tmpDir, "loadavg")
		err := os.WriteFile(loadavgPath, []byte(loadavgContent), 0644)
		require.NoError(t, err)
	}

	if uptimeContent != "" {
		uptimePath := filepath.Join(tmpDir, "uptime")
		err := os.WriteFile(uptimePath, []byte(uptimeContent), 0644)
		require.NoError(t, err)
	}

	if statContent != "" {
		statPath := filepath.Join(tmpDir, "stat")
		err := os.WriteFile(statPath, []byte(statContent), 0644)
		require.NoError(t, err)
	}

	config := performance.CollectionConfig{
		HostProcPath: tmpDir,
	}
	collector, err := collectors.NewLoadCollector(logr.Discard(), config)
	require.NoError(t, err)
	return collector
}

func validateLoadStats(t *testing.T, stats *performance.LoadStats, expected *performance.LoadStats) {
	assert.Equal(t, expected.Load1Min, stats.Load1Min)
	assert.Equal(t, expected.Load5Min, stats.Load5Min)
	assert.Equal(t, expected.Load15Min, stats.Load15Min)
	assert.Equal(t, expected.RunningProcs, stats.RunningProcs)
	assert.Equal(t, expected.TotalProcs, stats.TotalProcs)
	assert.Equal(t, expected.LastPID, stats.LastPID)
	assert.Equal(t, expected.BlockedProcs, stats.BlockedProcs)
	if expected.Uptime != 0 {
		assert.Equal(t, expected.Uptime, stats.Uptime)
	}
}

func TestLoadCollector_Constructor(t *testing.T) {
	t.Run("error on relative path", func(t *testing.T) {
		config := performance.CollectionConfig{HostProcPath: "relative/path"}
		_, err := collectors.NewLoadCollector(logr.Discard(), config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be an absolute path")
	})

	t.Run("error on empty path", func(t *testing.T) {
		config := performance.CollectionConfig{HostProcPath: ""}
		_, err := collectors.NewLoadCollector(logr.Discard(), config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "HostProcPath is required but not provided")
	})

	t.Run("success on non-existent path", func(t *testing.T) {
		// LoadCollector doesn't validate actual path existence in constructor
		config := performance.CollectionConfig{HostProcPath: "/non/existent/path/that/should/not/exist"}
		collector, err := collectors.NewLoadCollector(logr.Discard(), config)
		assert.NoError(t, err)
		assert.NotNil(t, collector)
	})
}

func TestLoadCollector_MissingFiles(t *testing.T) {
	tests := []struct {
		name          string
		createLoadavg bool
		createUptime  bool
		wantErr       bool
		expectedErr   string
	}{
		{
			name:          "missing loadavg file",
			createLoadavg: false,
			createUptime:  true,
			wantErr:       true,
			expectedErr:   "failed to read",
		},
		{
			name:          "missing uptime file (graceful degradation)",
			createLoadavg: true,
			createUptime:  false,
			wantErr:       false,
		},
		{
			name:          "missing both files",
			createLoadavg: false,
			createUptime:  false,
			wantErr:       true,
			expectedErr:   "failed to read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loadavgContent := ""
			uptimeContent := ""
			if tt.createLoadavg {
				loadavgContent = validLoadavgContent
			}
			if tt.createUptime {
				uptimeContent = validUptimeContent
			}
			collector := createTestLoadCollector(t, loadavgContent, uptimeContent)

			result, err := collector.Collect(context.Background())

			if tt.wantErr {
				assert.Error(t, err)
				if tt.expectedErr != "" {
					assert.Contains(t, err.Error(), tt.expectedErr)
				}
				return
			}

			require.NoError(t, err)
			stats, ok := result.(*performance.LoadStats)
			require.True(t, ok)

			// For missing uptime case, should have zero uptime but valid load data
			if !tt.createUptime && tt.createLoadavg {
				assert.Equal(t, 0.50, stats.Load1Min)
				assert.Equal(t, time.Duration(0), stats.Uptime)
			}
		})
	}
}

func TestLoadCollector_DataParsing(t *testing.T) {
	tests := []struct {
		name           string
		loadavgContent string
		uptimeContent  string
		wantErr        bool
		expectedErr    string
		expected       *performance.LoadStats
	}{
		// Valid parsing cases
		{
			name:           "valid load stats with uptime",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "high load values",
			loadavgContent: highLoadContent,
			uptimeContent:  "86400.25 172800.50",
			expected: &performance.LoadStats{
				Load1Min:     15.80,
				Load5Min:     10.45,
				Load15Min:    8.32,
				RunningProcs: 5,
				TotalProcs:   2048,
				LastPID:      98765,
				Uptime:       time.Duration(86400.25 * float64(time.Second)),
			},
		},
		{
			name:           "zero load values",
			loadavgContent: zeroLoadContent,
			uptimeContent:  "0.01 0.02",
			expected: &performance.LoadStats{
				Load1Min:     0.00,
				Load5Min:     0.00,
				Load15Min:    0.00,
				RunningProcs: 1,
				TotalProcs:   100,
				LastPID:      1,
				Uptime:       time.Duration(0.01 * float64(time.Second)),
			},
		},
		{
			name:           "very large load values",
			loadavgContent: "999.99 888.88 777.77 9999/99999 999999",
			uptimeContent:  "999999.99 1999999.98",
			expected: &performance.LoadStats{
				Load1Min:     999.99,
				Load5Min:     888.88,
				Load15Min:    777.77,
				RunningProcs: 9999,
				TotalProcs:   99999,
				LastPID:      999999,
				Uptime:       time.Duration(999999.99 * float64(time.Second)),
			},
		},
		{
			name:           "single digit process counts",
			loadavgContent: "0.01 0.02 0.03 1/1 1",
			uptimeContent:  "1.00 2.00",
			expected: &performance.LoadStats{
				Load1Min:     0.01,
				Load5Min:     0.02,
				Load15Min:    0.03,
				RunningProcs: 1,
				TotalProcs:   1,
				LastPID:      1,
				Uptime:       time.Duration(1.00 * float64(time.Second)),
			},
		},
		{
			name:           "extra whitespace in loadavg",
			loadavgContent: extraWhitespaceContent,
			uptimeContent:  "  1234.56   5678.90  ",
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "empty uptime (graceful degradation)",
			loadavgContent: validLoadavgContent,
			uptimeContent:  "",
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				Uptime:       time.Duration(0),
			},
		},
		// Error cases - loadavg parsing errors
		{
			name:           "malformed loadavg - insufficient fields",
			loadavgContent: malformedLoadContent,
			uptimeContent:  validUptimeContent,
			wantErr:        true,
			expectedErr:    "got 2 fields, expected 5",
		},
		{
			name:           "malformed loadavg - invalid float",
			loadavgContent: invalidFloatContent,
			uptimeContent:  validUptimeContent,
			wantErr:        true,
			expectedErr:    "invalid syntax",
		},
		{
			name:           "malformed loadavg - invalid process count format",
			loadavgContent: invalidProcContent,
			uptimeContent:  validUptimeContent,
			wantErr:        true,
			expectedErr:    "unexpected process count format",
		},
		{
			name:           "malformed loadavg - invalid PID",
			loadavgContent: invalidPIDContent,
			uptimeContent:  validUptimeContent,
			wantErr:        true,
			expectedErr:    "invalid syntax",
		},
		{
			name:           "missing loadavg file",
			loadavgContent: "",
			uptimeContent:  validUptimeContent,
			wantErr:        true,
			expectedErr:    "no such file or directory",
		},
		{
			name:           "whitespace only loadavg",
			loadavgContent: whitespaceContent,
			uptimeContent:  validUptimeContent,
			wantErr:        true,
			expectedErr:    "got 0 fields, expected 5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := createTestLoadCollector(t, tt.loadavgContent, tt.uptimeContent)
			result, err := collector.Collect(context.Background())

			if tt.wantErr {
				assert.Error(t, err)
				if tt.expectedErr != "" {
					assert.Contains(t, err.Error(), tt.expectedErr)
				}
				return
			}

			require.NoError(t, err)
			stats, ok := result.(*performance.LoadStats)
			require.True(t, ok)
			validateLoadStats(t, stats, tt.expected)
		})
	}
}

func TestLoadCollector_BlockedProcsIntegration(t *testing.T) {
	tests := []struct {
		name           string
		loadavgContent string
		uptimeContent  string
		statContent    string
		wantErr        bool
		expected       *performance.LoadStats
	}{
		{
			name:           "valid load stats with blocked processes",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    validStatWithBlockedContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 7,
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "stat with only procs_blocked line",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    blockedOnlyStatContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 10,
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "zero blocked processes",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    zeroBlockedStatContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 0,
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "max int32 blocked processes",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    maxBlockedStatContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 2147483647,
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "invalid blocked value (graceful degradation)",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    invalidBlockedStatContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 0, // Failed to parse, left as zero
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "missing blocked value",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    missingBlockedValueStatContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 0, // No value to parse
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "extra whitespace in stat",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    extraWhitespaceBlockedStatContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 15,
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "tab-separated stat",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    tabSeparatedBlockedStatContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 0, // Tab separation not supported by collector (expects space)
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "stat without procs_blocked line (graceful degradation)",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    noBlockedStatContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 0, // Missing line, defaults to zero
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "empty stat file",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    emptyBlockedStatContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 0,
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "stat with multiple values on procs_blocked line (uses first value)",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    multipleValuesStatContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 0, // Should fail parsing due to unexpected format
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "missing stat file (graceful degradation)",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    "", // No file created
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 0, // No stat file, defaults to zero
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "negative blocked value (graceful degradation)",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    negativeBlockedStatContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: -5, // Parses but would be invalid
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
		{
			name:           "float blocked value (graceful degradation)",
			loadavgContent: validLoadavgContent,
			uptimeContent:  validUptimeContent,
			statContent:    floatBlockedStatContent,
			expected: &performance.LoadStats{
				Load1Min:     0.50,
				Load5Min:     1.25,
				Load15Min:    2.75,
				RunningProcs: 2,
				TotalProcs:   1234,
				LastPID:      12345,
				BlockedProcs: 0, // Float parsing should fail
				Uptime:       time.Duration(1234.56 * float64(time.Second)),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := createTestLoadCollectorWithStat(t, tt.loadavgContent, tt.uptimeContent, tt.statContent)

			result, err := collector.Collect(context.Background())

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			stats, ok := result.(*performance.LoadStats)
			require.True(t, ok, "result should be *performance.LoadStats")

			validateLoadStats(t, stats, tt.expected)
		})
	}
}

func TestLoadCollector_StatPermissions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create loadavg and uptime files
	loadavgPath := filepath.Join(tmpDir, "loadavg")
	err := os.WriteFile(loadavgPath, []byte(validLoadavgContent), 0644)
	require.NoError(t, err)

	uptimePath := filepath.Join(tmpDir, "uptime")
	err = os.WriteFile(uptimePath, []byte(validUptimeContent), 0644)
	require.NoError(t, err)

	// Create stat file with no read permissions
	statPath := filepath.Join(tmpDir, "stat")
	err = os.WriteFile(statPath, []byte(validStatWithBlockedContent), 0000)
	require.NoError(t, err)

	config := performance.CollectionConfig{
		HostProcPath: tmpDir,
	}
	collector, err := collectors.NewLoadCollector(logr.Discard(), config)
	require.NoError(t, err)

	// Collection should succeed (graceful degradation - stat is optional)
	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats, ok := result.(*performance.LoadStats)
	require.True(t, ok)

	// Load data should be present
	assert.Equal(t, 0.50, stats.Load1Min)
	assert.Equal(t, 1.25, stats.Load5Min)
	assert.Equal(t, 2.75, stats.Load15Min)

	// Blocked processes should be zero (couldn't read stat)
	assert.Equal(t, int32(0), stats.BlockedProcs)
}

func TestLoadCollector_StatAsDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create loadavg and uptime files
	loadavgPath := filepath.Join(tmpDir, "loadavg")
	err := os.WriteFile(loadavgPath, []byte(validLoadavgContent), 0644)
	require.NoError(t, err)

	uptimePath := filepath.Join(tmpDir, "uptime")
	err = os.WriteFile(uptimePath, []byte(validUptimeContent), 0644)
	require.NoError(t, err)

	// Create stat as a directory instead of a file
	statPath := filepath.Join(tmpDir, "stat")
	err = os.MkdirAll(statPath, 0755)
	require.NoError(t, err)

	config := performance.CollectionConfig{
		HostProcPath: tmpDir,
	}
	collector, err := collectors.NewLoadCollector(logr.Discard(), config)
	require.NoError(t, err)

	// Collection should succeed (graceful degradation)
	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats, ok := result.(*performance.LoadStats)
	require.True(t, ok)

	// Load data should be present
	assert.Equal(t, 0.50, stats.Load1Min)

	// Blocked processes should be zero
	assert.Equal(t, int32(0), stats.BlockedProcs)
}

func TestLoadCollector_AllFieldsWithStat(t *testing.T) {
	collector := createTestLoadCollectorWithStat(t, validLoadavgContent, validUptimeContent, validStatWithBlockedContent)

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats, ok := result.(*performance.LoadStats)
	require.True(t, ok, "result should be *performance.LoadStats")

	// Verify all fields including the new BlockedProcs
	assert.Equal(t, 0.50, stats.Load1Min, "Load1Min")
	assert.Equal(t, 1.25, stats.Load5Min, "Load5Min")
	assert.Equal(t, 2.75, stats.Load15Min, "Load15Min")
	assert.Equal(t, int32(2), stats.RunningProcs, "RunningProcs")
	assert.Equal(t, int32(1234), stats.TotalProcs, "TotalProcs")
	assert.Equal(t, int32(12345), stats.LastPID, "LastPID")
	assert.Equal(t, int32(7), stats.BlockedProcs, "BlockedProcs from /proc/stat")
	assert.Equal(t, time.Duration(1234.56*float64(time.Second)), stats.Uptime, "Uptime")

	t.Log("Successfully validated all LoadStats fields including BlockedProcs from /proc/stat")
}
