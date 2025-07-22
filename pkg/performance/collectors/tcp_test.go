// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func createTestTCPCollector(procPath string) *collectors.TCPCollector {
	config := performance.CollectionConfig{
		HostProcPath: procPath,
		EnabledCollectors: map[performance.MetricType]bool{
			performance.MetricTypeTCP: true,
		},
	}
	return collectors.NewTCPCollector(logr.Discard(), config)
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
	collector := createTestTCPCollector(procPath)
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
			collector := createTestTCPCollector(procPath)
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
			collector := createTestTCPCollector(procPath)

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
			collector := createTestTCPCollector(procPath)
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
			collector := createTestTCPCollector(procPath)
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
	collector := createTestTCPCollector(procPath)
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
	collector := createTestTCPCollector(procPath)
	stats := collectAndValidate(t, collector)

	assert.Equal(t, uint64(expectedEstablished), stats.ConnectionsByState["ESTABLISHED"])
}
