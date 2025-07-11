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

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTCPCollector(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := t.TempDir()

	// Create test SNMP file
	snmpContent := `Ip: Forwarding DefaultTTL InReceives InHdrErrors InAddrErrors ForwDatagrams InUnknownProtos InDiscards InDelivers OutRequests OutDiscards OutNoRoutes ReasmTimeout ReasmReqds ReasmOKs ReasmFails FragOKs FragFails FragCreates
Ip: 1 64 1000 0 0 0 0 0 1000 1000 0 0 0 0 0 0 0 0 0
Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
Tcp: 1 200 120000 -1 100 200 10 5 8 50000 40000 500 2 15 3
`
	snmpPath := filepath.Join(tempDir, "snmp")
	require.NoError(t, os.WriteFile(snmpPath, []byte(snmpContent), 0644))

	// Create test netstat file
	netstatContent := `TcpExt: SyncookiesSent SyncookiesRecv SyncookiesFailed EmbryonicRsts PruneCalled RcvPruned OfoPruned OutOfWindowIcmps LockDroppedIcmps ArpFilter TW TWRecycled TWKilled PAWSPassive PAWSActive PAWSEstab DelayedACKs DelayedACKLocked DelayedACKLost ListenOverflows ListenDrops TCPPureAcks TCPHPAcks TCPRenoRecovery TCPSackRecovery TCPSACKReneging TCPFACKReorder TCPSACKReorder TCPRenoReorder TCPTSReorder TCPFullUndo TCPPartialUndo TCPDSACKUndo TCPLossUndo TCPLostRetransmit TCPRenoFailures TCPSackFailures TCPLossFailures TCPFastRetrans TCPForwardRetrans TCPSlowStartRetrans TCPTimeouts
TcpExt: 10 5 2 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 20 15 0 0 0 0 0 0 0 0 0 0 0 0 0 25 0 0 0 30 0 35 40
`
	netstatPath := filepath.Join(tempDir, "netstat")
	require.NoError(t, os.WriteFile(netstatPath, []byte(netstatContent), 0644))

	// Create test tcp file
	tcpContent := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 0100007F:0277 0100007F:0050 01 00000000:00000000 00:00000000 00000000  1000        0 12346 1 0000000000000000 20 4 29 10 -1
   2: 0100007F:0277 0100007F:0050 01 00000000:00000000 00:00000000 00000000  1000        0 12347 1 0000000000000000 20 4 29 10 -1
   3: 0100007F:0277 0100007F:0050 06 00000000:00000000 00:00000000 00000000  1000        0 12348 1 0000000000000000 20 4 29 10 -1
`
	tcpPath := filepath.Join(tempDir, "tcp")
	require.NoError(t, os.WriteFile(tcpPath, []byte(tcpContent), 0644))

	// Create test tcp6 file (empty for this test)
	tcp6Path := filepath.Join(tempDir, "tcp6")
	require.NoError(t, os.WriteFile(tcp6Path, []byte("  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"), 0644))

	// Create test directories
	netDir := filepath.Join(tempDir, "net")
	require.NoError(t, os.MkdirAll(netDir, 0755))

	// Move files to net directory
	require.NoError(t, os.Rename(snmpPath, filepath.Join(netDir, "snmp")))
	require.NoError(t, os.Rename(netstatPath, filepath.Join(netDir, "netstat")))
	require.NoError(t, os.Rename(tcpPath, filepath.Join(netDir, "tcp")))
	require.NoError(t, os.Rename(tcp6Path, filepath.Join(netDir, "tcp6")))

	// Create collector with test config
	config := performance.CollectionConfig{
		HostProcPath: tempDir,
		EnabledCollectors: map[performance.MetricType]bool{
			performance.MetricTypeTCP: true,
		},
	}

	logger := logr.Discard()
	collector := NewTCPCollector(logger, config)

	// Test collector properties
	assert.Equal(t, performance.MetricTypeTCP, collector.Type())
	assert.Equal(t, "TCP Statistics Collector", collector.Name())
	assert.False(t, collector.Capabilities().RequiresRoot)
	assert.True(t, collector.Capabilities().SupportsOneShot)

	// Collect metrics
	ctx := context.Background()
	result, err := collector.Collect(ctx)
	require.NoError(t, err)

	stats, ok := result.(*performance.TCPStats)
	require.True(t, ok, "expected *performance.TCPStats, got %T", result)

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

	// Verify connection states
	assert.NotNil(t, stats.ConnectionsByState)
	assert.Equal(t, uint64(2), stats.ConnectionsByState["ESTABLISHED"]) // 2 connections in state 01
	assert.Equal(t, uint64(1), stats.ConnectionsByState["LISTEN"])      // 1 connection in state 0A
	assert.Equal(t, uint64(1), stats.ConnectionsByState["TIME_WAIT"])   // 1 connection in state 06
}

func TestTCPCollectorMissingFiles(t *testing.T) {
	// Test with non-existent directory
	config := performance.CollectionConfig{
		HostProcPath: "/non/existent/path",
		EnabledCollectors: map[performance.MetricType]bool{
			performance.MetricTypeTCP: true,
		},
	}

	logger := logr.Discard()
	collector := NewTCPCollector(logger, config)

	ctx := context.Background()
	_, err := collector.Collect(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse SNMP stats")
}

func TestTCPCollectorPartialData(t *testing.T) {
	// Create a temporary directory with only SNMP file
	tempDir := t.TempDir()
	netDir := filepath.Join(tempDir, "net")
	require.NoError(t, os.MkdirAll(netDir, 0755))

	// Create minimal SNMP file
	snmpContent := `Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts
Tcp: 1 200 120000 -1 100 200 10 5 8 50000 40000 500 2 15
`
	snmpPath := filepath.Join(netDir, "snmp")
	require.NoError(t, os.WriteFile(snmpPath, []byte(snmpContent), 0644))

	config := performance.CollectionConfig{
		HostProcPath: tempDir,
		EnabledCollectors: map[performance.MetricType]bool{
			performance.MetricTypeTCP: true,
		},
	}

	logger := logr.Discard()
	collector := NewTCPCollector(logger, config)

	ctx := context.Background()
	result, err := collector.Collect(ctx)
	require.NoError(t, err)

	stats, ok := result.(*performance.TCPStats)
	require.True(t, ok)

	// Should have SNMP stats but no extended stats or connection counts
	assert.Equal(t, uint64(100), stats.ActiveOpens)
	assert.Equal(t, uint64(0), stats.SyncookiesSent) // Should be zero as netstat is missing
	assert.NotNil(t, stats.ConnectionsByState)
	assert.Equal(t, uint64(0), stats.ConnectionsByState["ESTABLISHED"]) // Should be zero as tcp files are missing
}
