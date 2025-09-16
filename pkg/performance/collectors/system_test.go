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

const (
	// Valid scenarios
	validStatContent = `cpu  1234567 0 2345678 98765432 123456 78901 234567 0 0 0
cpu0 617283 0 1172839 49382716 61728 39450 117283 0 0 0
cpu1 617284 0 1172839 49382716 61728 39451 117284 0 0 0
intr 123456789 1234 5678 9012 3456 7890 1234 5678
ctxt 987654321
btime 1638360000
processes 1234567
procs_running 4
procs_blocked 2
softirq 11111111 0 2222222 0 3333333 0 0 4444444 0 1111111 0
`

	// Partial content (missing some fields)
	partialStatContent = `cpu  1234567 0 2345678 98765432 123456 78901 234567 0 0 0
intr 123456789
ctxt 987654321
`

	// Edge cases
	zeroValuesStatContent = `cpu  0 0 0 0 0 0 0 0 0 0
intr 0
ctxt 0
procs_blocked 0
`

	// Boundary conditions
	maxValuesStatContent = `cpu  18446744073709551615 0 0 0 0 0 0 0 0 0
intr 18446744073709551615 1 2 3 4 5
ctxt 18446744073709551615
procs_blocked 18446744073709551615
`

	// Large values (realistic for long-running systems)
	largeValuesStatContent = `cpu  1234567890123 0 2345678901234 98765432109876 123456789012 78901234567 234567890123 0 0 0
intr 9876543210987654 1234567 2345678 3456789
ctxt 8765432109876543
btime 1234567890
processes 123456789012
procs_running 128
procs_blocked 64
`

	// Error conditions
	malformedStatContent = `cpu  1234567 0 2345678 98765432 123456 78901 234567 0 0 0
intr invalid_number
ctxt 987654321
`

	invalidIntrLineContent = `cpu  1234567 0 2345678 98765432 123456 78901 234567 0 0 0
intr
ctxt 987654321
`

	invalidCtxtLineContent = `cpu  1234567 0 2345678 98765432 123456 78901 234567 0 0 0
intr 123456789
ctxt invalid_context_switches
`

	missingValuesContent = `cpu  1234567 0 2345678 98765432 123456 78901 234567 0 0 0
intr
ctxt
`

	emptyStatContent = ``

	// Whitespace variations
	extraWhitespaceSystemStatContent = `cpu  1234567 0 2345678 98765432 123456 78901 234567 0 0 0
intr    123456789    1234    5678
ctxt    987654321
procs_blocked    2
`

	tabSeparatedSystemStatContent = `cpu	1234567	0	2345678	98765432	123456	78901	234567	0	0	0
intr	123456789	1234	5678
ctxt	987654321
procs_blocked	2
`

	// Mixed valid and invalid content
	mixedValidInvalidContent = `cpu  1234567 0 2345678 98765432 123456 78901 234567 0 0 0
intr 123456789 1234 5678
invalid_line
ctxt 987654321
another_invalid_line with multiple parts
procs_blocked 2
`

	// Single line files
	onlyIntrContent = `intr 123456789 1234 5678`
	onlyCtxtContent = `ctxt 987654321`

	// Very long lines (stress test) - simulated with many values
	veryLongIntrLineContent = `intr 123456789 1234 5678 9012 3456 7890 1234 5678 9012 3456 7890 1234 5678 9012 3456 7890 1234 5678 9012 3456 7890 1234 5678 9012 3456 7890 1234 5678 9012 3456 7890 1234 5678 9012 3456 7890
ctxt 987654321
`
)

// Helper function to create test SystemStatsCollector with mock /proc/stat
func createTestSystemStatsCollector(t *testing.T, statContent string) (*collectors.SystemStatsCollector, string) {
	return createTestSystemStatsCollectorWithOptions(t, statContent, true)
}

func createTestSystemStatsCollectorWithOptions(t *testing.T, statContent string, createFile bool) (*collectors.SystemStatsCollector, string) {
	tmpDir := t.TempDir()
	procPath := filepath.Join(tmpDir, "proc")
	require.NoError(t, os.MkdirAll(procPath, 0755))

	if createFile {
		statPath := filepath.Join(procPath, "stat")
		err := os.WriteFile(statPath, []byte(statContent), 0644)
		require.NoError(t, err)
	}

	config := performance.CollectionConfig{
		HostProcPath: procPath,
	}
	collector, err := collectors.NewSystemStatsCollector(logr.Discard(), config)
	require.NoError(t, err)

	return collector, procPath
}

// Helper function to collect and validate SystemStats
func collectAndValidateSystemStats(t *testing.T, collector *collectors.SystemStatsCollector, expectError bool, validate func(t *testing.T, stats *performance.SystemStats)) {
	result, err := collector.Collect(context.Background())

	if expectError {
		assert.Error(t, err)
		return
	}

	require.NoError(t, err)
	stats, ok := result.Data.(*performance.SystemStats)
	require.True(t, ok, "result should be *performance.SystemStats")

	if validate != nil {
		validate(t, stats)
	}
}

func TestSystemStatsCollector_Constructor(t *testing.T) {
	tests := []struct {
		name         string
		hostProcPath string
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "valid absolute path",
			hostProcPath: "/proc",
			expectError:  false,
		},
		{
			name:         "custom absolute path",
			hostProcPath: "/custom/proc",
			expectError:  false,
		},
		{
			name:         "relative path rejected",
			hostProcPath: "proc",
			expectError:  true,
			errorMsg:     "HostProcPath must be an absolute path",
		},
		{
			name:         "empty path rejected",
			hostProcPath: "",
			expectError:  true,
			errorMsg:     "HostProcPath is required but not provided",
		},
		{
			name:         "path with trailing slash",
			hostProcPath: "/proc/",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := performance.CollectionConfig{
				HostProcPath: tt.hostProcPath,
			}
			collector, err := collectors.NewSystemStatsCollector(logr.Discard(), config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)

				// Verify proper initialization
				assert.Equal(t, performance.MetricTypeSystem, collector.Type())
				assert.Equal(t, "System Activity Collector", collector.Name())
				capabilities := collector.Capabilities()
				assert.Nil(t, capabilities.RequiredCapabilities)
			}
		})
	}
}

func TestSystemStatsCollector_Collect(t *testing.T) {
	tests := []struct {
		name        string
		statContent string
		expectError bool
		errorMsg    string
		validate    func(t *testing.T, stats *performance.SystemStats)
	}{
		{
			name:        "valid complete stat file",
			statContent: validStatContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(123456789), stats.Interrupts)
				assert.Equal(t, uint64(987654321), stats.ContextSwitches)
			},
		},
		{
			name:        "partial stat file with only required fields",
			statContent: partialStatContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(123456789), stats.Interrupts)
				assert.Equal(t, uint64(987654321), stats.ContextSwitches)
			},
		},
		{
			name:        "zero values",
			statContent: zeroValuesStatContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(0), stats.Interrupts)
				assert.Equal(t, uint64(0), stats.ContextSwitches)
			},
		},
		{
			name:        "maximum uint64 values",
			statContent: maxValuesStatContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(18446744073709551615), stats.Interrupts)
				assert.Equal(t, uint64(18446744073709551615), stats.ContextSwitches)
			},
		},
		{
			name:        "large realistic values",
			statContent: largeValuesStatContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(9876543210987654), stats.Interrupts)
				assert.Equal(t, uint64(8765432109876543), stats.ContextSwitches)
			},
		},
		{
			name:        "malformed interrupt value (graceful degradation)",
			statContent: malformedStatContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(0), stats.Interrupts) // Failed to parse, left as zero
				assert.Equal(t, uint64(987654321), stats.ContextSwitches)
			},
		},
		{
			name:        "invalid context switches value (graceful degradation)",
			statContent: invalidCtxtLineContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(123456789), stats.Interrupts)
				assert.Equal(t, uint64(0), stats.ContextSwitches) // Failed to parse, left as zero
			},
		},
		{
			name:        "missing interrupt value",
			statContent: invalidIntrLineContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(0), stats.Interrupts) // No value to parse
				assert.Equal(t, uint64(987654321), stats.ContextSwitches)
			},
		},
		{
			name:        "empty values after labels",
			statContent: missingValuesContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(0), stats.Interrupts)
				assert.Equal(t, uint64(0), stats.ContextSwitches)
			},
		},
		{
			name:        "extra whitespace handling",
			statContent: extraWhitespaceSystemStatContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(123456789), stats.Interrupts)
				assert.Equal(t, uint64(987654321), stats.ContextSwitches)
			},
		},
		{
			name:        "tab separated values",
			statContent: tabSeparatedSystemStatContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(123456789), stats.Interrupts)
				assert.Equal(t, uint64(987654321), stats.ContextSwitches)
			},
		},
		{
			name:        "mixed valid and invalid lines",
			statContent: mixedValidInvalidContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(123456789), stats.Interrupts)
				assert.Equal(t, uint64(987654321), stats.ContextSwitches)
			},
		},
		{
			name:        "only intr line present",
			statContent: onlyIntrContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(123456789), stats.Interrupts)
				assert.Equal(t, uint64(0), stats.ContextSwitches) // Missing ctxt line
			},
		},
		{
			name:        "only ctxt line present",
			statContent: onlyCtxtContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(0), stats.Interrupts) // Missing intr line
				assert.Equal(t, uint64(987654321), stats.ContextSwitches)
			},
		},
		{
			name:        "very long interrupt line (stress test)",
			statContent: veryLongIntrLineContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(123456789), stats.Interrupts) // Should parse first value correctly
				assert.Equal(t, uint64(987654321), stats.ContextSwitches)
			},
		},
		{
			name:        "empty file",
			statContent: emptyStatContent,
			expectError: false,
			validate: func(t *testing.T, stats *performance.SystemStats) {
				assert.Equal(t, uint64(0), stats.Interrupts)
				assert.Equal(t, uint64(0), stats.ContextSwitches)
			},
		},
		{
			name:        "missing stat file",
			statContent: "", // Don't create file
			expectError: true,
			errorMsg:    "failed to open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Special handling for missing file test
			createFile := tt.statContent != "" || tt.name == "empty file"
			collector, _ := createTestSystemStatsCollectorWithOptions(t, tt.statContent, createFile)
			collectAndValidateSystemStats(t, collector, tt.expectError, tt.validate)
		})
	}
}

func TestSystemStatsCollector_FilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	procPath := filepath.Join(tmpDir, "proc")
	require.NoError(t, os.MkdirAll(procPath, 0755))

	statPath := filepath.Join(procPath, "stat")
	err := os.WriteFile(statPath, []byte(validStatContent), 0000) // No read permissions
	require.NoError(t, err)

	config := performance.CollectionConfig{
		HostProcPath: procPath,
	}
	collector, err := collectors.NewSystemStatsCollector(logr.Discard(), config)
	require.NoError(t, err)

	result, err := collector.Collect(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
	assert.Equal(t, performance.Event{}, result)
}

func TestSystemStatsCollector_DirectoryAsStatFile(t *testing.T) {
	tmpDir := t.TempDir()
	procPath := filepath.Join(tmpDir, "proc")
	require.NoError(t, os.MkdirAll(procPath, 0755))

	// Create a directory instead of a file
	statPath := filepath.Join(procPath, "stat")
	require.NoError(t, os.MkdirAll(statPath, 0755))

	config := performance.CollectionConfig{
		HostProcPath: procPath,
	}
	collector, err := collectors.NewSystemStatsCollector(logr.Discard(), config)
	require.NoError(t, err)

	result, err := collector.Collect(context.Background())
	assert.Error(t, err)
	assert.Equal(t, performance.Event{}, result)
}

func TestSystemStatsCollector_InterfaceCompliance(t *testing.T) {
	collector, _ := createTestSystemStatsCollector(t, validStatContent)

	// Verify it implements the Collector interface
	var _ performance.Collector = collector

	// Verify capabilities
	capabilities := collector.Capabilities()
	assert.True(t, capabilities.SupportsOneShot)
	assert.False(t, capabilities.SupportsContinuous)
	assert.Nil(t, capabilities.RequiredCapabilities)
	assert.Equal(t, "2.6.0", capabilities.MinKernelVersion)
}

func TestSystemStatsCollector_ConcurrentCollection(t *testing.T) {
	collector, _ := createTestSystemStatsCollector(t, validStatContent)

	// Run multiple collections concurrently
	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			_, err := collector.Collect(context.Background())
			results <- err
		}()
	}

	// Verify all collections succeed
	for i := 0; i < numGoroutines; i++ {
		err := <-results
		assert.NoError(t, err)
	}
}

func TestSystemStatsCollector_ContextCancellation(t *testing.T) {
	collector, _ := createTestSystemStatsCollector(t, validStatContent)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Collection should still work since we don't check context during file read
	result, err := collector.Collect(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}
