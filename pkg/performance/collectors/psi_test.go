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

// Test data fixtures - realistic PSI output from real systems

const (
	validCPUPSI = `some avg10=0.00 avg60=0.05 avg300=0.10 total=1234567890
full avg10=0.00 avg60=0.00 avg300=0.00 total=0`

	validMemoryPSI = `some avg10=12.50 avg60=15.20 avg300=18.75 total=9876543210
full avg10=5.25 avg60=8.10 avg300=12.30 total=5432109876`

	validIOPSI = `some avg10=2.30 avg60=3.45 avg300=4.67 total=3333333333
full avg10=1.10 avg60=1.50 avg300=2.00 total=1111111111`

	zeroPSI = `some avg10=0.00 avg60=0.00 avg300=0.00 total=0
full avg10=0.00 avg60=0.00 avg300=0.00 total=0`

	highPressurePSI = `some avg10=95.50 avg60=90.25 avg300=85.75 total=999999999999
full avg10=75.00 avg60=70.50 avg300=65.25 total=888888888888`

	malformedPSI = `some avg10=invalid avg60=0.00 avg300=0.00 total=0
full avg10=0.00 avg60=0.00 avg300=0.00 total=0`

	emptyPSI = ``
)

// TestPSICollector_Constructor tests PSI collector initialization
func TestPSICollector_Constructor(t *testing.T) {
	t.Run("valid configuration", func(t *testing.T) {
		tmpDir := t.TempDir()
		pressureDir := filepath.Join(tmpDir, "pressure")
		require.NoError(t, os.MkdirAll(pressureDir, 0755))

		// Create pressure files
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "cpu"), []byte(validCPUPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "memory"), []byte(validMemoryPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "io"), []byte(validIOPSI), 0644))

		config := performance.CollectionConfig{
			HostProcPath: tmpDir,
		}

		collector, err := collectors.NewPSICollector(logr.Discard(), config)
		require.NoError(t, err)
		require.NotNil(t, collector)
	})

	t.Run("missing pressure directory - PSI not available", func(t *testing.T) {
		tmpDir := t.TempDir()

		config := performance.CollectionConfig{
			HostProcPath: tmpDir,
		}

		collector, err := collectors.NewPSICollector(logr.Discard(), config)
		assert.Error(t, err, "Should fail when /proc/pressure doesn't exist")
		assert.Nil(t, collector)
		assert.Contains(t, err.Error(), "PSI not available")
	})

	t.Run("missing host proc path", func(t *testing.T) {
		config := performance.CollectionConfig{
			HostProcPath: "",
		}

		collector, err := collectors.NewPSICollector(logr.Discard(), config)
		assert.Error(t, err, "Should fail with missing HostProcPath")
		assert.Nil(t, collector)
	})
}

// TestPSICollector_Collect tests PSI data collection
func TestPSICollector_Collect(t *testing.T) {
	t.Run("collect all PSI metrics successfully", func(t *testing.T) {
		tmpDir := t.TempDir()
		pressureDir := filepath.Join(tmpDir, "pressure")
		require.NoError(t, os.MkdirAll(pressureDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "cpu"), []byte(validCPUPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "memory"), []byte(validMemoryPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "io"), []byte(validIOPSI), 0644))

		config := performance.CollectionConfig{HostProcPath: tmpDir}
		collector, err := collectors.NewPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		event, err := collector.Collect(context.Background())
		require.NoError(t, err)

		stats, ok := event.Data.(*performance.PSIStats)
		require.True(t, ok, "Event data should be PSIStats")

		// Verify CPU pressure
		require.NotNil(t, stats.CPU)
		assert.Equal(t, 0.00, stats.CPU.SomeAvg10)
		assert.Equal(t, 0.05, stats.CPU.SomeAvg60)
		assert.Equal(t, 0.10, stats.CPU.SomeAvg300)
		assert.Equal(t, uint64(1234567890), stats.CPU.SomeTotal)
		assert.Equal(t, 0.00, stats.CPU.FullAvg10) // CPU full is always 0

		// Verify Memory pressure
		require.NotNil(t, stats.Memory)
		assert.Equal(t, 12.50, stats.Memory.SomeAvg10)
		assert.Equal(t, 15.20, stats.Memory.SomeAvg60)
		assert.Equal(t, 18.75, stats.Memory.SomeAvg300)
		assert.Equal(t, uint64(9876543210), stats.Memory.SomeTotal)
		assert.Equal(t, 5.25, stats.Memory.FullAvg10)
		assert.Equal(t, 8.10, stats.Memory.FullAvg60)
		assert.Equal(t, 12.30, stats.Memory.FullAvg300)
		assert.Equal(t, uint64(5432109876), stats.Memory.FullTotal)

		// Verify I/O pressure
		require.NotNil(t, stats.IO)
		assert.Equal(t, 2.30, stats.IO.SomeAvg10)
		assert.Equal(t, 3.45, stats.IO.SomeAvg60)
		assert.Equal(t, 4.67, stats.IO.SomeAvg300)
		assert.Equal(t, uint64(3333333333), stats.IO.SomeTotal)
	})

	t.Run("zero pressure values", func(t *testing.T) {
		tmpDir := t.TempDir()
		pressureDir := filepath.Join(tmpDir, "pressure")
		require.NoError(t, os.MkdirAll(pressureDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "cpu"), []byte(zeroPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "memory"), []byte(zeroPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "io"), []byte(zeroPSI), 0644))

		config := performance.CollectionConfig{HostProcPath: tmpDir}
		collector, err := collectors.NewPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		event, err := collector.Collect(context.Background())
		require.NoError(t, err)

		stats := event.Data.(*performance.PSIStats)
		assert.Equal(t, 0.00, stats.CPU.SomeAvg10)
		assert.Equal(t, uint64(0), stats.CPU.SomeTotal)
		assert.Equal(t, 0.00, stats.Memory.SomeAvg10)
		assert.Equal(t, uint64(0), stats.Memory.SomeTotal)
	})

	t.Run("high pressure scenario", func(t *testing.T) {
		tmpDir := t.TempDir()
		pressureDir := filepath.Join(tmpDir, "pressure")
		require.NoError(t, os.MkdirAll(pressureDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "cpu"), []byte(highPressurePSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "memory"), []byte(highPressurePSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "io"), []byte(highPressurePSI), 0644))

		config := performance.CollectionConfig{HostProcPath: tmpDir}
		collector, err := collectors.NewPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		event, err := collector.Collect(context.Background())
		require.NoError(t, err)

		stats := event.Data.(*performance.PSIStats)
		assert.Equal(t, 95.50, stats.CPU.SomeAvg10)
		assert.Equal(t, 75.00, stats.Memory.FullAvg10)
	})
}

// TestPSICollector_ErrorHandling tests error scenarios
func TestPSICollector_ErrorHandling(t *testing.T) {
	t.Run("missing CPU pressure file", func(t *testing.T) {
		tmpDir := t.TempDir()
		pressureDir := filepath.Join(tmpDir, "pressure")
		require.NoError(t, os.MkdirAll(pressureDir, 0755))

		// Only create memory and io files
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "memory"), []byte(validMemoryPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "io"), []byte(validIOPSI), 0644))

		config := performance.CollectionConfig{HostProcPath: tmpDir}
		collector, err := collectors.NewPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		_, err = collector.Collect(context.Background())
		assert.Error(t, err, "Should fail when CPU pressure file is missing")
		assert.Contains(t, err.Error(), "CPU pressure")
	})

	t.Run("malformed PSI data", func(t *testing.T) {
		tmpDir := t.TempDir()
		pressureDir := filepath.Join(tmpDir, "pressure")
		require.NoError(t, os.MkdirAll(pressureDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "cpu"), []byte(malformedPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "memory"), []byte(validMemoryPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "io"), []byte(validIOPSI), 0644))

		config := performance.CollectionConfig{HostProcPath: tmpDir}
		collector, err := collectors.NewPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		_, err = collector.Collect(context.Background())
		assert.Error(t, err, "Should fail with malformed PSI data")
	})

	t.Run("empty PSI file", func(t *testing.T) {
		tmpDir := t.TempDir()
		pressureDir := filepath.Join(tmpDir, "pressure")
		require.NoError(t, os.MkdirAll(pressureDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "cpu"), []byte(emptyPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "memory"), []byte(validMemoryPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "io"), []byte(validIOPSI), 0644))

		config := performance.CollectionConfig{HostProcPath: tmpDir}
		collector, err := collectors.NewPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		event, err := collector.Collect(context.Background())
		require.NoError(t, err, "Empty PSI should parse successfully with zero values")

		stats := event.Data.(*performance.PSIStats)
		assert.NotNil(t, stats.CPU)
	})
}

// TestPSIParsing_EdgeCases tests PSI parsing edge cases
func TestPSIParsing_EdgeCases(t *testing.T) {
	testCases := []struct {
		name        string
		psiContent  string
		expectError bool
		description string
	}{
		{
			name: "whitespace variations",
			psiContent: `some   avg10=1.23   avg60=2.34   avg300=3.45   total=12345
full  avg10=0.12  avg60=0.23  avg300=0.34  total=1234`,
			expectError: false,
			description: "Should handle extra whitespace",
		},
		{
			name:        "only some line",
			psiContent:  `some avg10=1.00 avg60=2.00 avg300=3.00 total=100`,
			expectError: false,
			description: "Should handle missing full line",
		},
		{
			name: "decimal precision",
			psiContent: `some avg10=0.123456 avg60=1.234567 avg300=2.345678 total=123456789012345
full avg10=0.000001 avg60=0.000002 avg300=0.000003 total=1`,
			expectError: false,
			description: "Should handle high decimal precision",
		},
		{
			name:        "missing avg fields",
			psiContent:  `some avg10=1.00 total=100`,
			expectError: false,
			description: "Should handle missing avg fields (defaults to 0)",
		},
		{
			name:        "missing total field",
			psiContent:  `some avg10=1.00 avg60=2.00 avg300=3.00`,
			expectError: false,
			description: "Should handle missing total (defaults to 0)",
		},
		{
			name:        "negative values",
			psiContent:  `some avg10=-1.00 avg60=2.00 avg300=3.00 total=100`,
			expectError: true,
			description: "Should reject negative percentage values",
		},
		{
			name:        "values over 100",
			psiContent:  `some avg10=150.00 avg60=2.00 avg300=3.00 total=100`,
			expectError: true,
			description: "Should reject percentage values over 100",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			pressureDir := filepath.Join(tmpDir, "pressure")
			require.NoError(t, os.MkdirAll(pressureDir, 0755))

			require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "cpu"), []byte(tc.psiContent), 0644))
			require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "memory"), []byte(validMemoryPSI), 0644))
			require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "io"), []byte(validIOPSI), 0644))

			config := performance.CollectionConfig{HostProcPath: tmpDir}
			collector, err := collectors.NewPSICollector(logr.Discard(), config)
			require.NoError(t, err)

			event, err := collector.Collect(context.Background())

			if tc.expectError {
				assert.Error(t, err, tc.description)
			} else {
				assert.NoError(t, err, tc.description)
				stats := event.Data.(*performance.PSIStats)
				assert.NotNil(t, stats.CPU, tc.description)
			}
		})
	}
}

// TestPSIParsing_RealWorldData tests parsing with real PSI output from different systems
func TestPSIParsing_RealWorldData(t *testing.T) {
	testCases := []struct {
		name       string
		cpuPSI     string
		memoryPSI  string
		ioPSI      string
		verifyFunc func(*testing.T, *performance.PSIStats)
	}{
		{
			name: "idle system",
			cpuPSI: `some avg10=0.00 avg60=0.00 avg300=0.00 total=123456
full avg10=0.00 avg60=0.00 avg300=0.00 total=0`,
			memoryPSI: `some avg10=0.00 avg60=0.00 avg300=0.00 total=234567
full avg10=0.00 avg60=0.00 avg300=0.00 total=0`,
			ioPSI: `some avg10=0.00 avg60=0.00 avg300=0.00 total=345678
full avg10=0.00 avg60=0.00 avg300=0.00 total=0`,
			verifyFunc: func(t *testing.T, stats *performance.PSIStats) {
				assert.Equal(t, 0.00, stats.CPU.SomeAvg10, "Idle system should have zero averages")
				assert.Greater(t, stats.CPU.SomeTotal, uint64(0), "Total should accumulate even when idle")
			},
		},
		{
			name:   "memory constrained system",
			cpuPSI: validCPUPSI,
			memoryPSI: `some avg10=45.50 avg60=42.30 avg300=40.10 total=99999999999
full avg10=25.20 avg60=22.10 avg300=20.50 total=55555555555`,
			ioPSI: validIOPSI,
			verifyFunc: func(t *testing.T, stats *performance.PSIStats) {
				assert.Greater(t, stats.Memory.SomeAvg10, 40.0, "Memory constrained system")
				assert.Greater(t, stats.Memory.FullAvg10, 20.0, "Should have full memory stalls")
			},
		},
		{
			name:      "I/O constrained system",
			cpuPSI:    validCPUPSI,
			memoryPSI: validMemoryPSI,
			ioPSI: `some avg10=65.75 avg60=60.50 avg300=55.25 total=77777777777
full avg10=45.50 avg60=42.30 avg300=40.10 total=44444444444`,
			verifyFunc: func(t *testing.T, stats *performance.PSIStats) {
				assert.Greater(t, stats.IO.SomeAvg10, 60.0, "I/O constrained system")
				assert.Greater(t, stats.IO.FullAvg10, 40.0, "Should have full I/O stalls")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			pressureDir := filepath.Join(tmpDir, "pressure")
			require.NoError(t, os.MkdirAll(pressureDir, 0755))

			require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "cpu"), []byte(tc.cpuPSI), 0644))
			require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "memory"), []byte(tc.memoryPSI), 0644))
			require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "io"), []byte(tc.ioPSI), 0644))

			config := performance.CollectionConfig{HostProcPath: tmpDir}
			collector, err := collectors.NewPSICollector(logr.Discard(), config)
			require.NoError(t, err)

			event, err := collector.Collect(context.Background())
			require.NoError(t, err)

			stats := event.Data.(*performance.PSIStats)
			tc.verifyFunc(t, stats)
		})
	}
}

// TestPSIParsing_CPUNofull tests that CPU "full" is always zero
func TestPSIParsing_CPUNoFull(t *testing.T) {
	tmpDir := t.TempDir()
	pressureDir := filepath.Join(tmpDir, "pressure")
	require.NoError(t, os.MkdirAll(pressureDir, 0755))

	// CPU pressure with non-zero full values (shouldn't happen in real systems post-5.13)
	cpuPSI := `some avg10=5.00 avg60=4.00 avg300=3.00 total=1000000
full avg10=0.00 avg60=0.00 avg300=0.00 total=0`

	require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "cpu"), []byte(cpuPSI), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "memory"), []byte(validMemoryPSI), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "io"), []byte(validIOPSI), 0644))

	config := performance.CollectionConfig{HostProcPath: tmpDir}
	collector, err := collectors.NewPSICollector(logr.Discard(), config)
	require.NoError(t, err)

	event, err := collector.Collect(context.Background())
	require.NoError(t, err)

	stats := event.Data.(*performance.PSIStats)
	assert.Equal(t, 0.00, stats.CPU.FullAvg10, "CPU full should be zero at system level")
	assert.Equal(t, 0.00, stats.CPU.FullAvg60, "CPU full should be zero at system level")
	assert.Equal(t, 0.00, stats.CPU.FullAvg300, "CPU full should be zero at system level")
	assert.Equal(t, uint64(0), stats.CPU.FullTotal, "CPU full total should be zero")
}

// TestPSICollector_FilePermissions tests permission handling
func TestPSICollector_FilePermissions(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	t.Run("unreadable pressure file", func(t *testing.T) {
		tmpDir := t.TempDir()
		pressureDir := filepath.Join(tmpDir, "pressure")
		require.NoError(t, os.MkdirAll(pressureDir, 0755))

		cpuFile := filepath.Join(pressureDir, "cpu")
		require.NoError(t, os.WriteFile(cpuFile, []byte(validCPUPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "memory"), []byte(validMemoryPSI), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "io"), []byte(validIOPSI), 0644))

		// Remove read permission
		require.NoError(t, os.Chmod(cpuFile, 0000))
		defer func() { _ = os.Chmod(cpuFile, 0644) }()

		config := performance.CollectionConfig{HostProcPath: tmpDir}
		collector, err := collectors.NewPSICollector(logr.Discard(), config)
		require.NoError(t, err)

		_, err = collector.Collect(context.Background())
		assert.Error(t, err, "Should fail when pressure file is unreadable")
	})
}

// TestPSICollector_LargeValues tests handling of very large total values
func TestPSICollector_LargeValues(t *testing.T) {
	tmpDir := t.TempDir()
	pressureDir := filepath.Join(tmpDir, "pressure")
	require.NoError(t, os.MkdirAll(pressureDir, 0755))

	// Large total values (systems that have been up for a long time)
	largeTotalPSI := `some avg10=1.00 avg60=2.00 avg300=3.00 total=18446744073709551615
full avg10=0.50 avg60=1.00 avg300=1.50 total=9223372036854775807`

	require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "cpu"), []byte(largeTotalPSI), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "memory"), []byte(largeTotalPSI), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(pressureDir, "io"), []byte(largeTotalPSI), 0644))

	config := performance.CollectionConfig{HostProcPath: tmpDir}
	collector, err := collectors.NewPSICollector(logr.Discard(), config)
	require.NoError(t, err)

	event, err := collector.Collect(context.Background())
	require.NoError(t, err, "Should handle large uint64 values")

	stats := event.Data.(*performance.PSIStats)
	assert.Equal(t, uint64(18446744073709551615), stats.CPU.SomeTotal, "Should preserve max uint64")
}
