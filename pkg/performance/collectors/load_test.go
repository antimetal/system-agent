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
)

func createTestCollector(t *testing.T, loadavgContent, uptimeContent string) *collectors.LoadCollector {
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

func validateLoadStats(t *testing.T, stats *performance.LoadStats, expected *performance.LoadStats) {
	assert.Equal(t, expected.Load1Min, stats.Load1Min)
	assert.Equal(t, expected.Load5Min, stats.Load5Min)
	assert.Equal(t, expected.Load15Min, stats.Load15Min)
	assert.Equal(t, expected.RunningProcs, stats.RunningProcs)
	assert.Equal(t, expected.TotalProcs, stats.TotalProcs)
	assert.Equal(t, expected.LastPID, stats.LastPID)
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
		assert.Contains(t, err.Error(), "must be an absolute path")
	})

	t.Run("error on non-existent path", func(t *testing.T) {
		config := performance.CollectionConfig{HostProcPath: "/non/existent/path/that/should/not/exist"}
		_, err := collectors.NewLoadCollector(logr.Discard(), config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "HostProcPath validation failed")
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
			collector := createTestCollector(t, loadavgContent, uptimeContent)

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
			collector := createTestCollector(t, tt.loadavgContent, tt.uptimeContent)
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
