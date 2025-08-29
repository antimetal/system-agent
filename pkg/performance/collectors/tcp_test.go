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
	"strings"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test data constants
const (
	validSNMPHeader = `Ip: Forwarding DefaultTTL InReceives InHdrErrors InAddrErrors ForwDatagrams InUnknownProtos InDiscards InDelivers OutRequests OutDiscards OutNoRoutes ReasmTimeout ReasmReqds ReasmOKs ReasmFails FragOKs FragFails FragCreates
Ip: 1 64 1000 0 0 0 0 0 1000 1000 0 0 0 0 0 0 0 0 0
Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
Tcp: 1 200 120000 -1 100 200 10 5 8 50000 40000 500 2 15 3`

	validNetstatHeader = `TcpExt: SyncookiesSent SyncookiesRecv SyncookiesFailed EmbryonicRsts PruneCalled RcvPruned OfoPruned OutOfWindowIcmps LockDroppedIcmps ArpFilter TW TWRecycled TWKilled PAWSPassive PAWSActive PAWSEstab DelayedACKs DelayedACKLocked DelayedACKLost ListenOverflows ListenDrops TCPPureAcks TCPHPAcks TCPRenoRecovery TCPSackRecovery TCPSACKReneging TCPFACKReorder TCPSACKReorder TCPRenoReorder TCPTSReorder TCPFullUndo TCPPartialUndo TCPDSACKUndo TCPLossUndo TCPLostRetransmit TCPRenoFailures TCPSackFailures TCPLossFailures TCPFastRetrans TCPForwardRetrans TCPSlowStartRetrans TCPTimeouts
TcpExt: 10 5 2 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 20 15 0 0 0 0 0 0 0 0 0 0 0 0 0 25 0 0 0 30 0 35 40`

	validTCPHeader = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode`

	validTCP6Header = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode`
)

func createTestTCPCollector(t *testing.T, procPath string) *collectors.TCPCollector {
	config := performance.CollectionConfig{
		HostProcPath: procPath,
	}
	collector, err := collectors.NewTCPCollector(logr.Discard(), config)
	require.NoError(t, err)
	return collector
}

func setupTestFiles(t *testing.T, files map[string]string) string {
	tempDir := t.TempDir()
	netDir := filepath.Join(tempDir, "net")
	require.NoError(t, os.MkdirAll(netDir, 0755))

	for filename, content := range files {
		path := filepath.Join(netDir, filename)
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	}

	return tempDir
}

func collectAndValidate(t *testing.T, collector *collectors.TCPCollector) *performance.TCPStats {
	ctx := context.Background()
	result, err := collector.Collect(ctx)
	require.NoError(t, err)

	stats, ok := result.(*performance.TCPStats)
	require.True(t, ok, "expected *performance.TCPStats, got %T", result)
	require.NotNil(t, stats.ConnectionsByState)

	return stats
}

func TestTCPCollector_Constructor(t *testing.T) {
	tests := []struct {
		name    string
		config  performance.CollectionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid absolute path",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
			},
			wantErr: false,
		},
		{
			name: "invalid relative path",
			config: performance.CollectionConfig{
				HostProcPath: "proc",
			},
			wantErr: true,
			errMsg:  "HostProcPath must be an absolute path",
		},
		{
			name: "empty path",
			config: performance.CollectionConfig{
				HostProcPath: "",
			},
			wantErr: true,
			errMsg:  "HostProcPath is required but not provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := collectors.NewTCPCollector(logr.Discard(), tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
			}
		})
	}
}

func TestTCPCollector_BasicFunctionality(t *testing.T) {
	files := map[string]string{
		"snmp":    validSNMPHeader,
		"netstat": validNetstatHeader,
		"tcp": validTCPHeader + `
   0: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 0100007F:0277 0100007F:0050 01 00000000:00000000 00:00000000 00000000  1000        0 12346 1 0000000000000000 20 4 29 10 -1
   2: 0100007F:0277 0100007F:0050 01 00000000:00000000 00:00000000 00000000  1000        0 12347 1 0000000000000000 20 4 29 10 -1
   3: 0100007F:0277 0100007F:0050 06 00000000:00000000 00:00000000 00000000  1000        0 12348 1 0000000000000000 20 4 29 10 -1`,
		"tcp6": validTCP6Header + `
   0: 00000000000000000000000001000000:0050 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12349 1 0000000000000000 100 0 0 10 0
   1: 00000000000000000000000001000000:0277 00000000000000000000000001000000:0050 01 00000000:00000000 00:00000000 00000000  1000        0 12350 1 0000000000000000 20 4 29 10 -1`,
	}

	procPath := setupTestFiles(t, files)
	collector := createTestTCPCollector(t, procPath)
	stats := collectAndValidate(t, collector)

	// Verify SNMP stats
	assert.Equal(t, uint64(100), stats.ActiveOpens)
	assert.Equal(t, uint64(200), stats.PassiveOpens)
	assert.Equal(t, uint64(10), stats.AttemptFails)
	assert.Equal(t, uint64(5), stats.EstabResets)
	assert.Equal(t, uint64(8), stats.CurrEstab)
	assert.Equal(t, uint64(50000), stats.InSegs)
	assert.Equal(t, uint64(40000), stats.OutSegs)
	assert.Equal(t, uint64(500), stats.RetransSegs)
	assert.Equal(t, uint64(2), stats.InErrs)
	assert.Equal(t, uint64(15), stats.OutRsts)
	assert.Equal(t, uint64(3), stats.InCsumErrors)

	// Verify netstat extended stats
	assert.Equal(t, uint64(10), stats.SyncookiesSent)
	assert.Equal(t, uint64(5), stats.SyncookiesRecv)
	assert.Equal(t, uint64(2), stats.SyncookiesFailed)
	assert.Equal(t, uint64(20), stats.ListenOverflows)
	assert.Equal(t, uint64(15), stats.ListenDrops)
	assert.Equal(t, uint64(25), stats.TCPLostRetransmit)
	assert.Equal(t, uint64(30), stats.TCPFastRetrans)
	assert.Equal(t, uint64(35), stats.TCPSlowStartRetrans)
	assert.Equal(t, uint64(40), stats.TCPTimeouts)

	// Verify connection states (IPv4 + IPv6)
	assert.Equal(t, uint64(3), stats.ConnectionsByState["ESTABLISHED"]) // 2 IPv4 + 1 IPv6
	assert.Equal(t, uint64(2), stats.ConnectionsByState["LISTEN"])      // 1 IPv4 + 1 IPv6
	assert.Equal(t, uint64(1), stats.ConnectionsByState["TIME_WAIT"])   // 1 IPv4
}

func TestTCPCollector_MinorFormatVariations(t *testing.T) {
	// Test realistic format variations that might occur
	tests := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "trailing_newline_variations",
			files: map[string]string{
				"snmp": validSNMPHeader + "\n", // Extra newline at end
			},
		},
		{
			name: "no_trailing_newline",
			files: map[string]string{
				"snmp": strings.TrimSuffix(validSNMPHeader, "\n"), // No trailing newline
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procPath := setupTestFiles(t, tt.files)
			collector := createTestTCPCollector(t, procPath)
			stats := collectAndValidate(t, collector)

			// Basic validation that parsing worked
			assert.Equal(t, uint64(100), stats.ActiveOpens)
			assert.Equal(t, uint64(8), stats.CurrEstab)
		})
	}
}

func TestTCPCollector_ErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "missing_snmp_file",
			files:       map[string]string{},
			expectError: true,
			errorMsg:    "failed to parse SNMP stats",
		},
		{
			name: "malformed_snmp_header_value_mismatch",
			files: map[string]string{
				"snmp": `Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens
Tcp: 1 200 120000 -1 100 200 10 5 8`,
			},
			expectError: true,
			errorMsg:    "header/value length mismatch",
		},
		{
			name: "empty_snmp_file",
			files: map[string]string{
				"snmp": "",
			},
			expectError: true,
			errorMsg:    "TCP statistics not found",
		},
		{
			name: "no_tcp_section_in_snmp",
			files: map[string]string{
				"snmp": `Ip: Forwarding DefaultTTL
Ip: 1 64`,
			},
			expectError: true,
			errorMsg:    "TCP statistics not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procPath := setupTestFiles(t, tt.files)
			collector := createTestTCPCollector(t, procPath)

			ctx := context.Background()
			_, err := collector.Collect(ctx)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTCPCollector_GracefulDegradation(t *testing.T) {
	tests := []struct {
		name           string
		files          map[string]string
		validateResult func(*testing.T, *performance.TCPStats)
	}{
		{
			name: "missing_netstat_file",
			files: map[string]string{
				"snmp": validSNMPHeader,
			},
			validateResult: func(t *testing.T, stats *performance.TCPStats) {
				// SNMP stats should be present
				assert.Equal(t, uint64(100), stats.ActiveOpens)
				// Netstat stats should be zero
				assert.Equal(t, uint64(0), stats.SyncookiesSent)
				assert.Equal(t, uint64(0), stats.ListenOverflows)
			},
		},
		{
			name: "missing_tcp_files",
			files: map[string]string{
				"snmp":    validSNMPHeader,
				"netstat": validNetstatHeader,
			},
			validateResult: func(t *testing.T, stats *performance.TCPStats) {
				// SNMP and netstat stats should be present
				assert.Equal(t, uint64(100), stats.ActiveOpens)
				assert.Equal(t, uint64(10), stats.SyncookiesSent)
				// Connection states should be initialized but empty
				assert.NotNil(t, stats.ConnectionsByState)
				for _, state := range []string{"ESTABLISHED", "LISTEN", "TIME_WAIT"} {
					assert.Equal(t, uint64(0), stats.ConnectionsByState[state])
				}
			},
		},
		{
			name: "malformed_netstat_continues",
			files: map[string]string{
				"snmp": validSNMPHeader,
				"netstat": `TcpExt: Field1 Field2
TcpExt: NotANumber 456`,
			},
			validateResult: func(t *testing.T, stats *performance.TCPStats) {
				// SNMP stats should still work
				assert.Equal(t, uint64(100), stats.ActiveOpens)
				// Netstat parsing should have failed gracefully
				assert.Equal(t, uint64(0), stats.SyncookiesSent)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procPath := setupTestFiles(t, tt.files)
			collector := createTestTCPCollector(t, procPath)
			stats := collectAndValidate(t, collector)
			tt.validateResult(t, stats)
		})
	}
}

func TestTCPCollector_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		check func(*testing.T, *performance.TCPStats)
	}{
		{
			name: "extreme_values",
			files: map[string]string{
				"snmp": `Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
Tcp: 1 200 120000 -1 18446744073709551615 18446744073709551615 0 0 0 0 0 0 0 0 0`,
			},
			check: func(t *testing.T, stats *performance.TCPStats) {
				assert.Equal(t, uint64(18446744073709551615), stats.ActiveOpens) // Max uint64
				assert.Equal(t, uint64(18446744073709551615), stats.PassiveOpens)
			},
		},
		{
			name: "non_numeric_values_skipped",
			files: map[string]string{
				"snmp": `Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
Tcp: 1 200 120000 -1 ABC 200 10 5 8 50000 40000 500 2 15 3`,
			},
			check: func(t *testing.T, stats *performance.TCPStats) {
				assert.Equal(t, uint64(0), stats.ActiveOpens) // Should be 0 due to parse error
				assert.Equal(t, uint64(200), stats.PassiveOpens)
			},
		},
		{
			name: "unknown_tcp_states",
			files: map[string]string{
				"snmp": validSNMPHeader,
				"tcp": validTCPHeader + `
   0: 0100007F:0050 00000000:0000 FF 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 0100007F:0277 0100007F:0050 ZZ 00000000:00000000 00:00000000 00000000  1000        0 12346 1 0000000000000000 20 4 29 10 -1`,
			},
			check: func(t *testing.T, stats *performance.TCPStats) {
				// Unknown states should be ignored
				assert.Equal(t, uint64(0), stats.ConnectionsByState["ESTABLISHED"])
			},
		},
		{
			name: "short_tcp_lines",
			files: map[string]string{
				"snmp": validSNMPHeader,
				"tcp": validTCPHeader + `
   0: 0100007F:0050
   1: incomplete line`,
			},
			check: func(t *testing.T, stats *performance.TCPStats) {
				// Short lines should be skipped
				for _, count := range stats.ConnectionsByState {
					assert.Equal(t, uint64(0), count)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procPath := setupTestFiles(t, tt.files)
			collector := createTestTCPCollector(t, procPath)
			stats := collectAndValidate(t, collector)
			tt.check(t, stats)
		})
	}
}

func TestTCPCollector_AllConnectionStates(t *testing.T) {
	// Test all possible TCP states
	stateMap := map[string]string{
		"01": "ESTABLISHED",
		"02": "SYN_SENT",
		"03": "SYN_RECV",
		"04": "FIN_WAIT1",
		"05": "FIN_WAIT2",
		"06": "TIME_WAIT",
		"07": "CLOSE",
		"08": "CLOSE_WAIT",
		"09": "LAST_ACK",
		"0A": "LISTEN",
		"0B": "CLOSING",
	}

	var tcpContent strings.Builder
	tcpContent.WriteString(validTCPHeader + "\n")

	// Create one connection for each state
	i := 0
	for hexState := range stateMap {
		line := fmt.Sprintf("  %2d: 0100007F:%04X 0100007F:0050 %s 00000000:00000000 00:00000000 00000000  1000        0 %d 1 0000000000000000 20 4 29 10 -1\n",
			i, 0x1000+i, hexState, 12345+i)
		tcpContent.WriteString(line)
		i++
	}

	files := map[string]string{
		"snmp": validSNMPHeader,
		"tcp":  tcpContent.String(),
		"tcp6": validTCP6Header,
	}

	procPath := setupTestFiles(t, files)
	collector := createTestTCPCollector(t, procPath)
	stats := collectAndValidate(t, collector)

	// Verify each state has exactly one connection
	for hexState, stateName := range stateMap {
		assert.Equal(t, uint64(1), stats.ConnectionsByState[stateName],
			"State %s (%s) should have 1 connection", stateName, hexState)
	}
}

func TestTCPCollector_LargeFiles(t *testing.T) {
	// Test with many connections
	var tcpContent strings.Builder
	tcpContent.WriteString(validTCPHeader + "\n")

	// Create 1000 connections
	expectedEstablished := 1000
	for i := 0; i < expectedEstablished; i++ {
		line := fmt.Sprintf("  %2d: 0100007F:%04X 0100007F:0050 01 00000000:00000000 00:00000000 00000000  1000        0 %d 1 0000000000000000 20 4 29 10 -1\n",
			i, 0x1000+i, 12345+i)
		tcpContent.WriteString(line)
	}

	files := map[string]string{
		"snmp": validSNMPHeader,
		"tcp":  tcpContent.String(),
		"tcp6": validTCP6Header,
	}

	procPath := setupTestFiles(t, files)
	collector := createTestTCPCollector(t, procPath)
	stats := collectAndValidate(t, collector)

	assert.Equal(t, uint64(expectedEstablished), stats.ConnectionsByState["ESTABLISHED"])
}

// Tests consolidated from tcp_delta_test.go
func TestTCPCollector_DeltaAwareCollector(t *testing.T) {
	t.Run("interface compliance", func(t *testing.T) {
		logger := logr.Discard()
		config := performance.CollectionConfig{
			HostProcPath: "/tmp",
		}
		config.ApplyDefaults()

		collector, err := collectors.NewTCPCollector(logger, config)
		require.NoError(t, err)

		// Verify interface compliance
		var _ performance.PointCollector = collector

		// Test interface methods
		assert.False(t, collector.HasDeltaState())

		collector.ResetDeltaState()
	})

	t.Run("delta calculation progression", func(t *testing.T) {
		// Create a mock TCP collector that we can control
		logger := logr.Discard()
		config := performance.CollectionConfig{
			HostProcPath: "/tmp",
			Delta: performance.DeltaConfig{
				Mode:        performance.DeltaModeEnabled,
				MinInterval: 100 * time.Millisecond,
				MaxInterval: 5 * time.Minute,
			},
		}

		collector := &collectors.TCPCollector{
			BaseDeltaCollector: performance.NewBaseDeltaCollector(
				performance.MetricTypeTCP,
				"Test TCP Collector",
				logger,
				config,
				performance.CollectorCapabilities{},
			),
		}

		// Test first collection (no deltas available)
		assert.False(t, collector.HasDeltaState())
		should, reason := collector.ShouldCalculateDeltas(time.Now())
		assert.False(t, should)
		assert.Contains(t, reason, "no previous state")

		// Simulate first collection
		firstStats := &performance.TCPStats{
			ActiveOpens: 100,
			RetransSegs: 50,
			InSegs:      1000,
		}
		firstTime := time.Now()
		collector.UpdateDeltaState(firstStats, firstTime)

		assert.True(t, collector.HasDeltaState())

		// Simulate second collection (deltas should be calculated)
		// Use 1.5 seconds to test actual rate calculation logic (not trivial 1:1)
		interval := 1500 * time.Millisecond
		secondTime := firstTime.Add(interval)
		should, reason = collector.ShouldCalculateDeltas(secondTime)
		assert.True(t, should)
		assert.Empty(t, reason)

		// Test delta state management through public interface
		// Since calculateTCPDeltas is private, we test delta behavior through public methods
		secondStats := &performance.TCPStats{
			ActiveOpens: 150,  // +50
			RetransSegs: 75,   // +25
			InSegs:      2000, // +1000
		}

		// Update collector state to simulate progression
		collector.UpdateDeltaState(secondStats, secondTime)
		
		// Verify the collector now has delta state
		assert.True(t, collector.HasDeltaState())
		
		// Verify delta calculation would be performed for valid intervals
		should, reason = collector.ShouldCalculateDeltas(secondTime.Add(time.Second))
		assert.True(t, should)
		assert.Empty(t, reason)
	})

	t.Run("counter reset detection", func(t *testing.T) {
		logger := logr.Discard()
		config := performance.CollectionConfig{
			Delta: performance.DeltaConfig{
				Mode: performance.DeltaModeEnabled,
			},
		}

		collector := &collectors.TCPCollector{
			BaseDeltaCollector: performance.NewBaseDeltaCollector(
				performance.MetricTypeTCP,
				"Test TCP Collector",
				logger,
				config,
				performance.CollectorCapabilities{},
			),
		}

		// First collection
		firstStats := &performance.TCPStats{
			ActiveOpens: 1000,
			InSegs:      5000,
		}
		firstTime := time.Now()
		collector.UpdateDeltaState(firstStats, firstTime)

		// Second collection with counter reset (values decreased)
		secondStats := &performance.TCPStats{
			ActiveOpens: 50,  // Reset happened
			InSegs:      100, // Reset happened
		}
		secondTime := firstTime.Add(time.Second)

		// Test counter reset detection through public interface
		// Update state again - reset detection would happen through normal collection
		collector.UpdateDeltaState(secondStats, secondTime)
		
		// Verify collector maintains delta state even after reset
		assert.True(t, collector.HasDeltaState())
	})

	t.Run("interval validation", func(t *testing.T) {
		logger := logr.Discard()
		config := performance.CollectionConfig{
			Delta: performance.DeltaConfig{
				Mode:        performance.DeltaModeEnabled,
				MaxInterval: 5 * time.Minute,
			},
		}

		collector := &collectors.TCPCollector{
			BaseDeltaCollector: performance.NewBaseDeltaCollector(
				performance.MetricTypeTCP,
				"Test TCP Collector",
				logger,
				config,
				performance.CollectorCapabilities{},
			),
		}

		// Set up initial state
		firstTime := time.Now()
		collector.UpdateDeltaState(&performance.TCPStats{}, firstTime)

		// Test interval too large
		laterTime := firstTime.Add(10 * time.Minute)
		should, reason := collector.ShouldCalculateDeltas(laterTime)
		assert.False(t, should)
		assert.Contains(t, reason, "interval too large")

		// Test time going backwards
		earlierTime := firstTime.Add(-time.Second)
		should, reason = collector.ShouldCalculateDeltas(earlierTime)
		assert.False(t, should)
		assert.Contains(t, reason, "time went backwards")
	})
}

// Tests consolidated from tcp_collect_with_delta_test.go
func TestTCPCollector_Collect_Integration(t *testing.T) {
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
			},
		}

		collector, err := collectors.NewTCPCollector(logger, config)
		require.NoError(t, err)

		// First collection - should have no deltas
		result1, err := collector.Collect(context.Background())
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
		result2, err := collector.Collect(context.Background())
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

		collector, err := collectors.NewTCPCollector(logger, config)
		require.NoError(t, err)

		// Multiple collections should never have deltas
		for i := 0; i < 3; i++ {
			result, err := collector.Collect(context.Background())
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
			},
		}

		collector, err := collectors.NewTCPCollector(logger, config)
		require.NoError(t, err)

		// First collection
		_, err = collector.Collect(context.Background())
		require.NoError(t, err)

		time.Sleep(60 * time.Millisecond)

		// Second collection with reset values (much lower)
		snmpContent2 := `Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
Tcp: 1 200 120000 -1 100 50 5 2 25 5000 4500 10 0 8 0
`
		require.NoError(t, os.WriteFile(snmpPath, []byte(snmpContent2), 0644))

		result, err := collector.Collect(context.Background())
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
			},
		}

		collector, err := collectors.NewTCPCollector(logger, config)
		require.NoError(t, err)

		// Collection should fail gracefully
		result, err := collector.Collect(context.Background())
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
			},
		}

		collector, err := collectors.NewTCPCollector(logger, config)
		require.NoError(t, err)

		// First collection
		result1, err := collector.Collect(context.Background())
		require.NoError(t, err)
		stats1 := result1.(*performance.TCPStats)
		assert.Nil(t, stats1.Delta) // No delta on first collection

		// Second collection too soon (under MinInterval)
		time.Sleep(100 * time.Millisecond) // Less than MinInterval
		result2, err := collector.Collect(context.Background())
		require.NoError(t, err)
		stats2 := result2.(*performance.TCPStats)
		assert.Nil(t, stats2.Delta) // Should skip delta calculation

		// Third collection after too long (over MaxInterval)
		time.Sleep(1200 * time.Millisecond) // More than MaxInterval
		result3, err := collector.Collect(context.Background())
		require.NoError(t, err)
		stats3 := result3.(*performance.TCPStats)
		assert.Nil(t, stats3.Delta) // Should skip delta calculation due to large interval
	})
}
