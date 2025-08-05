// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build unit

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

func TestNetworkCollector_Constructor(t *testing.T) {
	tests := []struct {
		name        string
		procPath    string
		sysPath     string
		expectError bool
	}{
		{
			name:        "valid paths",
			procPath:    "/proc",
			sysPath:     "/sys",
			expectError: false,
		},
		{
			name:        "relative proc path",
			procPath:    "proc",
			sysPath:     "/sys",
			expectError: true,
		},
		{
			name:        "relative sys path",
			procPath:    "/proc",
			sysPath:     "sys",
			expectError: true,
		},
		{
			name:        "empty proc path",
			procPath:    "",
			sysPath:     "/sys",
			expectError: true,
		},
		{
			name:        "empty sys path",
			procPath:    "/proc",
			sysPath:     "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := performance.CollectionConfig{
				HostProcPath: tt.procPath,
				HostSysPath:  tt.sysPath,
			}
			collector, err := collectors.NewNetworkCollector(logr.Discard(), config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
			}
		})
	}
}

// Helper functions

func createTestNetworkCollector(t *testing.T, procNetDev string, sysFiles map[string]string) (*collectors.NetworkCollector, string, string) {
	// Create temp directory
	tempDir := t.TempDir()
	procPath := filepath.Join(tempDir, "proc")
	sysPath := filepath.Join(tempDir, "sys")

	// Setup proc files
	if procNetDev != "" {
		netDir := filepath.Join(procPath, "net")
		require.NoError(t, os.MkdirAll(netDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(netDir, "dev"), []byte(procNetDev), 0644))
	}

	// Setup sys files
	if sysFiles != nil {
		classNetDir := filepath.Join(sysPath, "class", "net")
		for path, content := range sysFiles {
			fullPath := filepath.Join(classNetDir, path)
			require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
			require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
		}
	}

	// Create collector
	config := performance.CollectionConfig{
		HostProcPath: procPath,
		HostSysPath:  sysPath,
	}
	collector, err := collectors.NewNetworkCollector(logr.Discard(), config)
	require.NoError(t, err)

	return collector, procPath, sysPath
}

func collectAndValidateNetwork(t *testing.T, collector *collectors.NetworkCollector, expectError bool, validate func(t *testing.T, stats []performance.NetworkStats)) {
	result, err := collector.Collect(context.Background())

	if expectError {
		assert.Error(t, err)
		return
	}

	require.NoError(t, err)
	stats, ok := result.([]performance.NetworkStats)
	require.True(t, ok, "result should be []performance.NetworkStats")

	if validate != nil {
		validate(t, stats)
	}
}

// Test data constants

const validProcNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1234567890  123456    0    0    0     0          0         0 1234567890  123456    0    0    0     0       0          0
  eth0: 9876543210  654321   10    5    0     2          0       100 8765432109  543210   20   10    0     5       3          0
wlan0: 5555555555  333333    5    2    1     0          0        50 4444444444  222222    3    1    0     2       0          0`

const malformedProcNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
malformed line without colon
  eth0: 1000  100    0    0    0     0          0         0 2000  200    0    0    0     0       0          0`

const insufficientFieldsProcNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
  eth0: 1000 100`

func TestNetworkCollector_Collect(t *testing.T) {
	tests := []struct {
		name           string
		procNetDev     string
		sysFiles       map[string]string
		expectError    bool
		validateResult func(t *testing.T, stats []performance.NetworkStats)
	}{
		{
			name:       "valid network stats with sysfs metadata",
			procNetDev: validProcNetDev,
			sysFiles: map[string]string{
				"eth0/speed":      "1000",
				"eth0/duplex":     "full",
				"eth0/operstate":  "up",
				"eth0/carrier":    "1",
				"wlan0/speed":     "100",
				"wlan0/duplex":    "half",
				"wlan0/operstate": "down",
				"wlan0/carrier":   "0",
			},
			expectError: false,
			validateResult: func(t *testing.T, stats []performance.NetworkStats) {
				require.Len(t, stats, 3)

				// Check lo interface
				lo := findInterface(stats, "lo")
				require.NotNil(t, lo)
				assert.Equal(t, uint64(1234567890), lo.RxBytes)
				assert.Equal(t, uint64(123456), lo.RxPackets)
				assert.Equal(t, uint64(0), lo.RxErrors)
				assert.Equal(t, uint64(1234567890), lo.TxBytes)
				assert.Equal(t, uint64(123456), lo.TxPackets)

				// Check eth0 interface
				eth0 := findInterface(stats, "eth0")
				require.NotNil(t, eth0)
				assert.Equal(t, uint64(9876543210), eth0.RxBytes)
				assert.Equal(t, uint64(654321), eth0.RxPackets)
				assert.Equal(t, uint64(10), eth0.RxErrors)
				assert.Equal(t, uint64(5), eth0.RxDropped)
				assert.Equal(t, uint64(2), eth0.RxFrame)
				assert.Equal(t, uint64(100), eth0.RxMulticast)
				assert.Equal(t, uint64(8765432109), eth0.TxBytes)
				assert.Equal(t, uint64(543210), eth0.TxPackets)
				assert.Equal(t, uint64(20), eth0.TxErrors)
				assert.Equal(t, uint64(10), eth0.TxDropped)
				assert.Equal(t, uint64(5), eth0.TxCollisions)
				assert.Equal(t, uint64(3), eth0.TxCarrier)
				assert.Equal(t, uint64(1000), eth0.Speed)
				assert.Equal(t, "full", eth0.Duplex)
				assert.Equal(t, "up", eth0.OperState)
				assert.True(t, eth0.LinkDetected)

				// Check wlan0 interface
				wlan0 := findInterface(stats, "wlan0")
				require.NotNil(t, wlan0)
				assert.Equal(t, uint64(5555555555), wlan0.RxBytes)
				assert.Equal(t, uint64(333333), wlan0.RxPackets)
				assert.Equal(t, uint64(100), wlan0.Speed)
				assert.Equal(t, "half", wlan0.Duplex)
				assert.Equal(t, "down", wlan0.OperState)
				assert.False(t, wlan0.LinkDetected)
			},
		},
		{
			name:        "missing proc file",
			procNetDev:  "",
			expectError: true,
		},
		{
			name:        "malformed line - skip bad line",
			procNetDev:  malformedProcNetDev,
			expectError: false,
			validateResult: func(t *testing.T, stats []performance.NetworkStats) {
				// Should still parse eth0
				require.Len(t, stats, 1)
				assert.Equal(t, "eth0", stats[0].Interface)
				assert.Equal(t, uint64(1000), stats[0].RxBytes)
				assert.Equal(t, uint64(100), stats[0].RxPackets)
			},
		},
		{
			name:        "insufficient fields - skip bad line",
			procNetDev:  insufficientFieldsProcNetDev,
			expectError: false,
			validateResult: func(t *testing.T, stats []performance.NetworkStats) {
				// Should skip the line with insufficient fields
				require.Len(t, stats, 0)
			},
		},
		{
			name:        "missing sysfs files - graceful degradation",
			procNetDev:  validProcNetDev,
			sysFiles:    nil, // No sysfs files
			expectError: false,
			validateResult: func(t *testing.T, stats []performance.NetworkStats) {
				require.Len(t, stats, 3)

				// Check that basic stats are still collected
				eth0 := findInterface(stats, "eth0")
				require.NotNil(t, eth0)
				assert.Equal(t, uint64(9876543210), eth0.RxBytes)
				assert.Equal(t, uint64(654321), eth0.RxPackets)

				// Sysfs fields should be empty/zero
				assert.Equal(t, uint64(0), eth0.Speed)
				assert.Equal(t, "", eth0.Duplex)
				assert.Equal(t, "", eth0.OperState)
				assert.False(t, eth0.LinkDetected)
			},
		},
		{
			name: "zero values",
			procNetDev: `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
  eth0: 0  0    0    0    0     0          0         0 0  0    0    0    0     0       0          0`,
			expectError: false,
			validateResult: func(t *testing.T, stats []performance.NetworkStats) {
				require.Len(t, stats, 1)
				eth0 := stats[0]
				assert.Equal(t, uint64(0), eth0.RxBytes)
				assert.Equal(t, uint64(0), eth0.TxBytes)
			},
		},
		{
			name: "very large values",
			procNetDev: `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
  eth0: 18446744073709551615  18446744073709551615    0    0    0     0          0         0 18446744073709551615  18446744073709551615    0    0    0     0       0          0`,
			expectError: false,
			validateResult: func(t *testing.T, stats []performance.NetworkStats) {
				require.Len(t, stats, 1)
				eth0 := stats[0]
				assert.Equal(t, uint64(18446744073709551615), eth0.RxBytes) // Max uint64
				assert.Equal(t, uint64(18446744073709551615), eth0.TxBytes)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectError && tt.procNetDev == "" {
				// Special case for missing file - create empty temp dir
				tempDir := t.TempDir()
				config := performance.CollectionConfig{
					HostProcPath: filepath.Join(tempDir, "proc"),
					HostSysPath:  filepath.Join(tempDir, "sys"),
				}
				collector, err := collectors.NewNetworkCollector(logr.Discard(), config)
				require.NoError(t, err)

				_, err = collector.Collect(context.Background())
				assert.Error(t, err)
				return
			}

			collector, _, _ := createTestNetworkCollector(t, tt.procNetDev, tt.sysFiles)
			collectAndValidateNetwork(t, collector, tt.expectError, tt.validateResult)
		})
	}
}

func findInterface(stats []performance.NetworkStats, name string) *performance.NetworkStats {
	for i := range stats {
		if stats[i].Interface == name {
			return &stats[i]
		}
	}
	return nil
}
