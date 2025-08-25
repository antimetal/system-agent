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

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/antimetal/agent/pkg/performance"
)

func TestTCPCollector_CollectWithDelta_Integration(t *testing.T) {
	t.Run("full integration with mock filesystem", func(t *testing.T) {
		// Create temporary directory structure
		tempDir := t.TempDir()

		// Create mock /proc/net/snmp file
		snmpContent := `Ip: Forwarding DefaultTTL InReceives InHdrErrors InAddrErrors ForwDatagrams InUnknownProtos InDiscards InDelivers OutRequests
Ip: 1 64 1000 0 0 0 0 0 1000 1000
Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
Tcp: 1 200 120000 -1 1000 500 10 5 50 100000 95000 200 0 15 0
`
		snmpPath := filepath.Join(tempDir, "net", "snmp")
		require.NoError(t, os.MkdirAll(filepath.Dir(snmpPath), 0755))
		require.NoError(t, os.WriteFile(snmpPath, []byte(snmpContent), 0644))

		// Create mock /proc/net/netstat file
		netstatContent := `TcpExt: SyncookiesSent SyncookiesRecv SyncookiesFailed ListenOverflows ListenDrops TCPLostRetransmit TCPFastRetrans TCPSlowStartRetrans TCPTimeouts
TcpExt: 5 3 2 8 12 25 40 30 100
`
		netstatPath := filepath.Join(tempDir, "net", "netstat")
		require.NoError(t, os.WriteFile(netstatPath, []byte(netstatContent), 0644))

		// Create mock /proc/net/tcp file
		tcpContent := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12346 1 0000000000000000 100 0 0 10 0
   2: 0A000001:1F90 0A000002:C350 01 00000000:00000000 00:00000000 00000000     0        0 12347 1 0000000000000000 100 0 0 10 0
`
		tcpPath := filepath.Join(tempDir, "net", "tcp")
		require.NoError(t, os.WriteFile(tcpPath, []byte(tcpContent), 0644))

		// Create mock /proc/net/tcp6 file
		tcp6Content := `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000001000000:1F90 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12348 1 0000000000000000 100 0 0 10 0
`
		tcp6Path := filepath.Join(tempDir, "net", "tcp6")
		require.NoError(t, os.WriteFile(tcp6Path, []byte(tcp6Content), 0644))

		// Create collector with delta enabled
		logger := logr.Discard()
		config := performance.CollectionConfig{
			HostProcPath: tempDir,
			Delta: performance.DeltaConfig{
				Mode:        performance.DeltaModeEnabled,
				MinInterval: 100 * time.Millisecond,
				MaxInterval: 5 * time.Minute,
				EnabledCollectors: map[performance.MetricType]bool{
					performance.MetricTypeTCP: true,
				},
			},
		}

		collector, err := NewTCPCollector(logger, config)
		require.NoError(t, err)

		// First collection - should have no deltas
		result1, err := collector.CollectWithDelta(context.Background(), config.Delta)
		require.NoError(t, err)

		stats1, ok := result1.(*performance.TCPStats)
		require.True(t, ok)

		// Verify basic stats are collected
		assert.Equal(t, uint64(1000), stats1.ActiveOpens)
		assert.Equal(t, uint64(500), stats1.PassiveOpens)
		assert.Equal(t, uint64(100000), stats1.InSegs)
		assert.Equal(t, uint64(95000), stats1.OutSegs)
		assert.Equal(t, uint64(200), stats1.RetransSegs)

		// First collection should have no deltas
		assert.Nil(t, stats1.Delta)

		// Simulate time passage and update filesystem
		time.Sleep(150 * time.Millisecond) // Ensure we exceed MinInterval

		// Update mock files with new values
		snmpContent2 := `Ip: Forwarding DefaultTTL InReceives InHdrErrors InAddrErrors ForwDatagrams InUnknownProtos InDiscards InDelivers OutRequests
Ip: 1 64 2000 0 0 0 0 0 2000 2000
Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
Tcp: 1 200 120000 -1 1050 520 12 6 55 102000 96500 220 0 18 0
`
		require.NoError(t, os.WriteFile(snmpPath, []byte(snmpContent2), 0644))

		netstatContent2 := `TcpExt: SyncookiesSent SyncookiesRecv SyncookiesFailed ListenOverflows ListenDrops TCPLostRetransmit TCPFastRetrans TCPSlowStartRetrans TCPTimeouts
TcpExt: 7 4 3 10 15 30 45 35 110
`
		require.NoError(t, os.WriteFile(netstatPath, []byte(netstatContent2), 0644))

		// Second collection - should have deltas
		result2, err := collector.CollectWithDelta(context.Background(), config.Delta)
		require.NoError(t, err)

		stats2, ok := result2.(*performance.TCPStats)
		require.True(t, ok)

		// Verify updated stats
		assert.Equal(t, uint64(1050), stats2.ActiveOpens)
		assert.Equal(t, uint64(520), stats2.PassiveOpens)
		assert.Equal(t, uint64(102000), stats2.InSegs)
		assert.Equal(t, uint64(96500), stats2.OutSegs)
		assert.Equal(t, uint64(220), stats2.RetransSegs)

		// Verify deltas are calculated
		require.NotNil(t, stats2.Delta)
		assert.Equal(t, uint64(50), stats2.Delta.ActiveOpens)  // 1050 - 1000
		assert.Equal(t, uint64(20), stats2.Delta.PassiveOpens) // 520 - 500
		assert.Equal(t, uint64(2000), stats2.Delta.InSegs)     // 102000 - 100000
		assert.Equal(t, uint64(1500), stats2.Delta.OutSegs)    // 96500 - 95000
		assert.Equal(t, uint64(20), stats2.Delta.RetransSegs)  // 220 - 200

		// Extended stats deltas
		assert.Equal(t, uint64(2), stats2.Delta.SyncookiesSent)      // 7 - 5
		assert.Equal(t, uint64(1), stats2.Delta.SyncookiesRecv)      // 4 - 3
		assert.Equal(t, uint64(1), stats2.Delta.SyncookiesFailed)    // 3 - 2
		assert.Equal(t, uint64(2), stats2.Delta.ListenOverflows)     // 10 - 8
		assert.Equal(t, uint64(3), stats2.Delta.ListenDrops)         // 15 - 12
		assert.Equal(t, uint64(5), stats2.Delta.TCPLostRetransmit)   // 30 - 25
		assert.Equal(t, uint64(5), stats2.Delta.TCPFastRetrans)      // 45 - 40
		assert.Equal(t, uint64(5), stats2.Delta.TCPSlowStartRetrans) // 35 - 30
		assert.Equal(t, uint64(10), stats2.Delta.TCPTimeouts)        // 110 - 100

		// Verify rates are calculated (should be > 0 since we enabled rates mode)
		assert.Greater(t, stats2.Delta.ActiveOpensPerSec, uint64(0))
		assert.Greater(t, stats2.Delta.InSegsPerSec, uint64(0))

		// Verify metadata
		assert.False(t, stats2.Delta.IsFirstCollection)
		assert.False(t, stats2.Delta.CounterResetDetected)
		assert.Greater(t, stats2.Delta.CollectionInterval, 100*time.Millisecond)
		assert.Less(t, stats2.Delta.CollectionInterval, time.Second)
	})

	t.Run("delta calculation disabled", func(t *testing.T) {
		// Create minimal mock filesystem
		tempDir := t.TempDir()
		snmpContent := `Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
Tcp: 1 200 120000 -1 1000 500 10 5 50 100000 95000 200 0 15 0
`
		snmpPath := filepath.Join(tempDir, "net", "snmp")
		require.NoError(t, os.MkdirAll(filepath.Dir(snmpPath), 0755))
		require.NoError(t, os.WriteFile(snmpPath, []byte(snmpContent), 0644))

		logger := logr.Discard()
		config := performance.CollectionConfig{
			HostProcPath: tempDir,
			Delta: performance.DeltaConfig{
				Mode: performance.DeltaModeDisabled, // Disabled
			},
		}

		collector, err := NewTCPCollector(logger, config)
		require.NoError(t, err)

		// Multiple collections should never have deltas
		for i := 0; i < 3; i++ {
			result, err := collector.CollectWithDelta(context.Background(), config.Delta)
			require.NoError(t, err)

			stats, ok := result.(*performance.TCPStats)
			require.True(t, ok)

			// Basic stats should be present
			assert.Equal(t, uint64(1000), stats.ActiveOpens)

			// Delta should always be nil when disabled
			assert.Nil(t, stats.Delta)

			time.Sleep(10 * time.Millisecond)
		}
	})

	t.Run("counter reset detection in real collection", func(t *testing.T) {
		// Create mock filesystem
		tempDir := t.TempDir()
		snmpPath := filepath.Join(tempDir, "net", "snmp")
		require.NoError(t, os.MkdirAll(filepath.Dir(snmpPath), 0755))

		// Initial high values
		snmpContent1 := `Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
Tcp: 1 200 120000 -1 10000 5000 100 50 50 1000000 950000 2000 0 150 0
`
		require.NoError(t, os.WriteFile(snmpPath, []byte(snmpContent1), 0644))

		logger := logr.Discard()
		config := performance.CollectionConfig{
			HostProcPath: tempDir,
			Delta: performance.DeltaConfig{
				Mode:        performance.DeltaModeEnabled,
				MinInterval: 50 * time.Millisecond,
				MaxInterval: 5 * time.Minute,
				EnabledCollectors: map[performance.MetricType]bool{
					performance.MetricTypeTCP: true,
				},
			},
		}

		collector, err := NewTCPCollector(logger, config)
		require.NoError(t, err)

		// First collection
		_, err = collector.CollectWithDelta(context.Background(), config.Delta)
		require.NoError(t, err)

		time.Sleep(60 * time.Millisecond)

		// Second collection with reset values (much lower)
		snmpContent2 := `Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
Tcp: 1 200 120000 -1 100 50 5 2 25 5000 4500 10 0 8 0
`
		require.NoError(t, os.WriteFile(snmpPath, []byte(snmpContent2), 0644))

		result, err := collector.CollectWithDelta(context.Background(), config.Delta)
		require.NoError(t, err)

		stats, ok := result.(*performance.TCPStats)
		require.True(t, ok)
		require.NotNil(t, stats.Delta)

		// Deltas should be 0 due to reset detection
		assert.Equal(t, uint64(0), stats.Delta.ActiveOpens)
		assert.Equal(t, uint64(0), stats.Delta.InSegs)
		assert.Equal(t, uint64(0), stats.Delta.RetransSegs)

		// Reset should be detected
		assert.True(t, stats.Delta.CounterResetDetected)
	})

	t.Run("error handling during collection", func(t *testing.T) {
		// Create collector with non-existent filesystem
		logger := logr.Discard()
		config := performance.CollectionConfig{
			HostProcPath: "/nonexistent/path",
			Delta: performance.DeltaConfig{
				Mode: performance.DeltaModeEnabled,
				EnabledCollectors: map[performance.MetricType]bool{
					performance.MetricTypeTCP: true,
				},
			},
		}

		collector, err := NewTCPCollector(logger, config)
		require.NoError(t, err)

		// Collection should fail gracefully
		result, err := collector.CollectWithDelta(context.Background(), config.Delta)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no such file or directory")
	})

	t.Run("interval validation edge cases", func(t *testing.T) {
		// Create minimal mock filesystem
		tempDir := t.TempDir()
		snmpContent := `Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
Tcp: 1 200 120000 -1 1000 500 10 5 50 100000 95000 200 0 15 0
`
		snmpPath := filepath.Join(tempDir, "net", "snmp")
		require.NoError(t, os.MkdirAll(filepath.Dir(snmpPath), 0755))
		require.NoError(t, os.WriteFile(snmpPath, []byte(snmpContent), 0644))

		logger := logr.Discard()
		config := performance.CollectionConfig{
			HostProcPath: tempDir,
			Delta: performance.DeltaConfig{
				Mode:        performance.DeltaModeEnabled,
				MinInterval: 500 * time.Millisecond, // High min interval
				MaxInterval: 1 * time.Second,        // Low max interval
				EnabledCollectors: map[performance.MetricType]bool{
					performance.MetricTypeTCP: true,
				},
			},
		}

		collector, err := NewTCPCollector(logger, config)
		require.NoError(t, err)

		// First collection
		result1, err := collector.CollectWithDelta(context.Background(), config.Delta)
		require.NoError(t, err)
		stats1 := result1.(*performance.TCPStats)
		assert.Nil(t, stats1.Delta) // No delta on first collection

		// Second collection too soon (under MinInterval)
		time.Sleep(100 * time.Millisecond) // Less than MinInterval
		result2, err := collector.CollectWithDelta(context.Background(), config.Delta)
		require.NoError(t, err)
		stats2 := result2.(*performance.TCPStats)
		assert.Nil(t, stats2.Delta) // Should skip delta calculation

		// Third collection after too long (over MaxInterval)
		time.Sleep(1200 * time.Millisecond) // More than MaxInterval
		result3, err := collector.CollectWithDelta(context.Background(), config.Delta)
		require.NoError(t, err)
		stats3 := result3.(*performance.TCPStats)
		assert.Nil(t, stats3.Delta) // Should skip delta calculation due to large interval
	})

}
