// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkCollector(t *testing.T) {
	tests := []struct {
		name           string
		procNetDev     string
		sysFiles       map[string]string
		expectError    bool
		validateResult func(t *testing.T, stats []performance.NetworkStats)
	}{
		{
			name: "valid network stats",
			procNetDev: `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1234567890  123456    0    0    0     0          0         0 1234567890  123456    0    0    0     0       0          0
  eth0: 9876543210  654321   10    5    0     2          0       100 8765432109  543210   20   10    0     5       3          0
wlan0: 5555555555  333333    5    2    1     0          0        50 4444444444  222222    3    1    0     2       0          0`,
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
			name: "calculate rates on second collection",
			procNetDev: `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
  eth0: 1000  100    0    0    0     0          0         0 2000  200    0    0    0     0       0          0`,
			expectError: false,
			validateResult: func(t *testing.T, stats []performance.NetworkStats) {
				require.Len(t, stats, 1)
				// First collection should have zero rates
				assert.Equal(t, float64(0), stats[0].RxBytesPerSec)
				assert.Equal(t, float64(0), stats[0].RxPacketsPerSec)
				assert.Equal(t, float64(0), stats[0].TxBytesPerSec)
				assert.Equal(t, float64(0), stats[0].TxPacketsPerSec)
			},
		},
		{
			name:        "missing proc file",
			procNetDev:  "",
			expectError: true,
		},
		{
			name: "malformed line",
			procNetDev: `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
malformed line without colon
  eth0: 1000  100    0    0    0     0          0         0 2000  200    0    0    0     0       0          0`,
			expectError: false,
			validateResult: func(t *testing.T, stats []performance.NetworkStats) {
				// Should still parse eth0
				require.Len(t, stats, 1)
				assert.Equal(t, "eth0", stats[0].Interface)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tempDir := t.TempDir()
			procPath := filepath.Join(tempDir, "proc")
			sysPath := filepath.Join(tempDir, "sys")

			// Setup proc files
			if tt.procNetDev != "" {
				netDir := filepath.Join(procPath, "net")
				require.NoError(t, os.MkdirAll(netDir, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(netDir, "dev"), []byte(tt.procNetDev), 0644))
			}

			// Setup sys files
			if tt.sysFiles != nil {
				classNetDir := filepath.Join(sysPath, "class", "net")
				for path, content := range tt.sysFiles {
					fullPath := filepath.Join(classNetDir, path)
					require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
					require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
				}
			}

			// Create collector
			logger := testr.New(t)
			config := performance.CollectionConfig{
				HostProcPath: procPath,
				HostSysPath:  sysPath,
			}
			collector := NewNetworkCollector(logger, config)

			// Collect stats
			result, err := collector.Collect(context.Background())

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				stats, ok := result.([]performance.NetworkStats)
				require.True(t, ok, "result should be []performance.NetworkStats")
				if tt.validateResult != nil {
					tt.validateResult(t, stats)
				}
			}
		})
	}
}

func TestNetworkCollectorRateCalculation(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()
	procPath := filepath.Join(tempDir, "proc")
	sysPath := filepath.Join(tempDir, "sys")

	netDir := filepath.Join(procPath, "net")
	require.NoError(t, os.MkdirAll(netDir, 0755))

	// Create collector
	logger := testr.New(t)
	config := performance.CollectionConfig{
		HostProcPath: procPath,
		HostSysPath:  sysPath,
	}
	collector := NewNetworkCollector(logger, config)

	// First collection
	procNetDev1 := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
  eth0: 1000  100    0    0    0     0          0         0 2000  200    0    0    0     0       0          0`
	require.NoError(t, os.WriteFile(filepath.Join(netDir, "dev"), []byte(procNetDev1), 0644))

	result1, err := collector.Collect(context.Background())
	require.NoError(t, err)
	stats1 := result1.([]performance.NetworkStats)
	require.Len(t, stats1, 1)

	// First collection should have zero rates
	assert.Equal(t, float64(0), stats1[0].RxBytesPerSec)
	assert.Equal(t, float64(0), stats1[0].RxPacketsPerSec)

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Second collection with increased counters
	procNetDev2 := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
  eth0: 2000  200    0    0    0     0          0         0 4000  400    0    0    0     0       0          0`
	require.NoError(t, os.WriteFile(filepath.Join(netDir, "dev"), []byte(procNetDev2), 0644))

	result2, err := collector.Collect(context.Background())
	require.NoError(t, err)
	stats2 := result2.([]performance.NetworkStats)
	require.Len(t, stats2, 1)

	// Should have calculated rates
	assert.Greater(t, stats2[0].RxBytesPerSec, float64(0))
	assert.Greater(t, stats2[0].RxPacketsPerSec, float64(0))
	assert.Greater(t, stats2[0].TxBytesPerSec, float64(0))
	assert.Greater(t, stats2[0].TxPacketsPerSec, float64(0))

	// Rates should be approximately 10000 bytes/sec and 1000 packets/sec
	// (1000 bytes in ~0.1 seconds)
	assert.InDelta(t, 10000, stats2[0].RxBytesPerSec, 2000)
	assert.InDelta(t, 1000, stats2[0].RxPacketsPerSec, 200)
	assert.InDelta(t, 20000, stats2[0].TxBytesPerSec, 4000)
	assert.InDelta(t, 2000, stats2[0].TxPacketsPerSec, 400)
}

func findInterface(stats []performance.NetworkStats, name string) *performance.NetworkStats {
	for i := range stats {
		if stats[i].Interface == name {
			return &stats[i]
		}
	}
	return nil
}
