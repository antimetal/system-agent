// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// Compile-time interface check
var _ performance.PointCollector = (*TCPCollector)(nil)

// TCPCollector collects TCP connection statistics from /proc/net/snmp, /proc/net/netstat, /proc/net/tcp*
//
// This collector reads TCP statistics from multiple proc files:
// - /proc/net/snmp: Basic TCP statistics (RFC 1213 MIB-II)
// - /proc/net/netstat: Extended TCP statistics (Linux-specific)
// - /proc/net/tcp: IPv4 TCP connection states
// - /proc/net/tcp6: IPv6 TCP connection states
//
// The statistics help monitor TCP performance, connection health, and
// potential issues like SYN floods, connection drops, and retransmissions.
//
// References:
// - https://www.kernel.org/doc/html/latest/networking/proc_net_tcp.html
// - https://www.kernel.org/doc/html/latest/networking/snmp_counter.html
type TCPCollector struct {
	performance.BaseCollector
	snmpPath    string
	netstatPath string
	tcpPath     string
	tcp6Path    string
}

// TCP connection state mappings from kernel
// These hexadecimal values correspond to the TCP state enum in the kernel
// Reference: include/net/tcp_states.h
var tcpStates = map[string]string{
	"01": "ESTABLISHED", // Connection established
	"02": "SYN_SENT",    // Sent SYN, waiting for SYN-ACK
	"03": "SYN_RECV",    // Received SYN, sent SYN-ACK
	"04": "FIN_WAIT1",   // Sent FIN, waiting for ACK or FIN
	"05": "FIN_WAIT2",   // Received ACK of FIN, waiting for FIN
	"06": "TIME_WAIT",   // Waiting for 2*MSL timeout
	"07": "CLOSE",       // Connection closed
	"08": "CLOSE_WAIT",  // Received FIN, waiting for app to close
	"09": "LAST_ACK",    // Sent FIN after receiving FIN, waiting for ACK
	"0A": "LISTEN",      // Listening for incoming connections
	"0B": "CLOSING",     // Simultaneous close, waiting for ACK
}

func NewTCPCollector(logger logr.Logger, config performance.CollectionConfig) (*TCPCollector, error) {
	// Validate paths are absolute
	if !filepath.IsAbs(config.HostProcPath) {
		return nil, fmt.Errorf("HostProcPath must be an absolute path, got: %q", config.HostProcPath)
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    true,
		SupportsContinuous: false,
		RequiresRoot:       false,
		RequiresEBPF:       false,
		MinKernelVersion:   "2.6.0", // /proc/net/snmp has been around for a long time
	}

	return &TCPCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeTCP,
			"TCP Statistics Collector",
			logger,
			config,
			capabilities,
		),
		snmpPath:    filepath.Join(config.HostProcPath, "net", "snmp"),
		netstatPath: filepath.Join(config.HostProcPath, "net", "netstat"),
		tcpPath:     filepath.Join(config.HostProcPath, "net", "tcp"),
		tcp6Path:    filepath.Join(config.HostProcPath, "net", "tcp6"),
	}, nil
}

func (c *TCPCollector) Collect(ctx context.Context) (any, error) {
	return c.collectTCPStats()
}

// collectTCPStats gathers TCP statistics from multiple proc files
func (c *TCPCollector) collectTCPStats() (*performance.TCPStats, error) {
	stats := &performance.TCPStats{
		ConnectionsByState: make(map[string]uint64),
	}

	// Parse /proc/net/snmp for basic TCP statistics
	if err := c.parseSNMP(stats); err != nil {
		return nil, fmt.Errorf("failed to parse SNMP stats: %w", err)
	}

	// Parse /proc/net/netstat for extended TCP statistics
	if err := c.parseNetstat(stats); err != nil {
		// This is optional, so just log the error
		c.Logger().V(1).Info("failed to parse netstat (continuing)", "error", err)
	}

	// Count TCP connections by state
	if err := c.countConnectionStates(stats); err != nil {
		// This is optional, so just log the error
		c.Logger().V(1).Info("failed to count connection states (continuing)", "error", err)
	}

	return stats, nil
}

// parseSNMP parses /proc/net/snmp for TCP statistics
//
// /proc/net/snmp format:
//
//	Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
//	Tcp: 1 200 120000 -1 12345 67890 123 456 789 1234567 7654321 12345 0 123 0
//
// The file contains pairs of lines for each protocol:
// - First line: Field names
// - Second line: Corresponding values
//
// These are standard TCP MIB-II counters defined in RFC 1213.
func (c *TCPCollector) parseSNMP(stats *performance.TCPStats) error {
	file, err := os.Open(c.snmpPath)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", c.snmpPath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var tcpHeader []string
	var tcpValues []string

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)

		if len(fields) < 2 {
			continue
		}

		// Look for TCP statistics
		if fields[0] == "Tcp:" {
			if tcpHeader == nil {
				// This is the header line
				tcpHeader = fields[1:]
			} else {
				// This is the values line
				tcpValues = fields[1:]
				break
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading %s: %w", c.snmpPath, err)
	}

	if len(tcpHeader) == 0 || len(tcpValues) == 0 {
		return fmt.Errorf("TCP statistics not found in %s", c.snmpPath)
	}

	if len(tcpHeader) != len(tcpValues) {
		return fmt.Errorf("TCP header/value length mismatch: %d != %d", len(tcpHeader), len(tcpValues))
	}

	// Map the values to our struct fields
	for i, header := range tcpHeader {
		value, err := strconv.ParseUint(tcpValues[i], 10, 64)
		if err != nil {
			continue // Skip fields we can't parse
		}

		switch header {
		case "ActiveOpens":
			stats.ActiveOpens = value
		case "PassiveOpens":
			stats.PassiveOpens = value
		case "AttemptFails":
			stats.AttemptFails = value
		case "EstabResets":
			stats.EstabResets = value
		case "CurrEstab":
			stats.CurrEstab = value
		case "InSegs":
			stats.InSegs = value
		case "OutSegs":
			stats.OutSegs = value
		case "RetransSegs":
			stats.RetransSegs = value
		case "InErrs":
			stats.InErrs = value
		case "OutRsts":
			stats.OutRsts = value
		case "InCsumErrors":
			stats.InCsumErrors = value
		default:
			// Log unknown fields at debug level
			c.Logger().V(2).Info("Unknown TCP field in SNMP", "field", header, "value", value)
		}
	}

	return nil
}

// parseNetstat parses /proc/net/netstat for extended TCP statistics
//
// /proc/net/netstat format is similar to /proc/net/snmp:
//
//	TcpExt: SyncookiesSent SyncookiesRecv SyncookiesFailed ... TCPTimeouts ...
//	TcpExt: 123 456 789 ... 12345 ...
//
// These are Linux-specific TCP extensions not covered by standard MIB-II.
// They provide detailed insights into TCP behavior and performance issues.
func (c *TCPCollector) parseNetstat(stats *performance.TCPStats) error {
	file, err := os.Open(c.netstatPath)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", c.netstatPath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var tcpExtHeader []string
	var tcpExtValues []string

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)

		if len(fields) < 2 {
			continue
		}

		// Look for TcpExt statistics
		if fields[0] == "TcpExt:" {
			if tcpExtHeader == nil {
				// This is the header line
				tcpExtHeader = fields[1:]
			} else {
				// This is the values line
				tcpExtValues = fields[1:]
				break
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading %s: %w", c.netstatPath, err)
	}

	if len(tcpExtHeader) == 0 || len(tcpExtValues) == 0 {
		// TcpExt might not be present on all systems
		return nil
	}

	if len(tcpExtHeader) != len(tcpExtValues) {
		return fmt.Errorf("TcpExt header/value length mismatch: %d != %d", len(tcpExtHeader), len(tcpExtValues))
	}

	// Map the values to our struct fields
	for i, header := range tcpExtHeader {
		value, err := strconv.ParseUint(tcpExtValues[i], 10, 64)
		if err != nil {
			continue // Skip fields we can't parse
		}

		switch header {
		case "SyncookiesSent":
			stats.SyncookiesSent = value
		case "SyncookiesRecv":
			stats.SyncookiesRecv = value
		case "SyncookiesFailed":
			stats.SyncookiesFailed = value
		case "ListenOverflows":
			stats.ListenOverflows = value
		case "ListenDrops":
			stats.ListenDrops = value
		case "TCPLostRetransmit":
			stats.TCPLostRetransmit = value
		case "TCPFastRetrans":
			stats.TCPFastRetrans = value
		case "TCPSlowStartRetrans":
			stats.TCPSlowStartRetrans = value
		case "TCPTimeouts":
			stats.TCPTimeouts = value
		}
	}

	return nil
}

// countConnectionStates counts TCP connections by state from /proc/net/tcp and /proc/net/tcp6
func (c *TCPCollector) countConnectionStates(stats *performance.TCPStats) error {
	// Initialize all known states to 0
	for _, state := range tcpStates {
		stats.ConnectionsByState[state] = 0
	}

	// Count IPv4 connections
	if err := c.countConnectionsFromFile(c.tcpPath, stats); err != nil {
		c.Logger().V(1).Info("failed to count IPv4 connections", "error", err)
	}

	// Count IPv6 connections
	if err := c.countConnectionsFromFile(c.tcp6Path, stats); err != nil {
		c.Logger().V(1).Info("failed to count IPv6 connections", "error", err)
	}

	return nil
}

// countConnectionsFromFile counts connections by state from a single tcp file
//
// /proc/net/tcp format (each line represents one connection):
//
//	sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode
//	0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000   1000  0 12345 1 0000000000000000 100 0 0 10 0
//
// Fields we care about:
// - sl: Entry number
// - local_address: Local IP:Port in hex
// - rem_address: Remote IP:Port in hex
// - st: Connection state in hex (see tcpStates map)
//
// The same format applies to /proc/net/tcp6 for IPv6 connections.
func (c *TCPCollector) countConnectionsFromFile(path string, stats *performance.TCPStats) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		if lineNum == 1 {
			// Skip header line
			continue
		}

		line := scanner.Text()
		fields := strings.Fields(line)

		// Format: sl local_address rem_address st tx_queue:rx_queue ...
		// We need at least 4 fields to get the state
		if len(fields) < 4 {
			continue
		}

		// State is in the 4th field (index 3)
		stateHex := fields[3]
		if stateName, ok := tcpStates[stateHex]; ok {
			stats.ConnectionsByState[stateName]++
		}
	}

	return scanner.Err()
}
