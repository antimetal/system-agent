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

func init() {
	performance.Register(performance.MetricTypeNUMA, performance.PartialNewContinuousPointCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewNUMACollector(logger, config)
		},
	))
}

// Compile-time interface check
var _ performance.PointCollector = (*NUMACollector)(nil)

// NUMACollector collects NUMA (Non-Uniform Memory Access) statistics
//
// NUMA is a computer memory design where memory access time depends on the
// memory location relative to the processor. In NUMA systems, processors
// can access their own local memory faster than non-local memory.
//
// This collector monitors:
// - NUMA topology (nodes, CPUs per node, memory per node)
// - Memory allocation statistics (hits, misses, foreign allocations)
// - Cross-node memory access patterns
// - Per-node memory usage
//
// Key performance indicators:
// - High numa_hit and low numa_miss values indicate good NUMA locality
// - High numa_foreign values suggest memory pressure causing cross-node allocations
// - Remote memory access can be 2-3x slower than local access
//
// Data sources:
// - /sys/devices/system/node/node*/numastat - Per-node allocation statistics
// - /sys/devices/system/node/node*/meminfo - Per-node memory usage
// - /sys/devices/system/node/node*/cpulist - CPUs assigned to each node
// - /proc/buddyinfo - Memory fragmentation info per node
//
// Reference: https://www.kernel.org/doc/html/latest/admin-guide/mm/numa.html
type NUMACollector struct {
	performance.BaseCollector
	sysNodesPath string
	procPath     string
}

func NewNUMACollector(logger logr.Logger, config performance.CollectionConfig) (*NUMACollector, error) {
	// Validate configuration - NUMA collector requires both proc and sys paths
	if err := config.Validate(performance.ValidateOptions{
		RequireHostProcPath: true,
		RequireHostSysPath:  true,
	}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   false,
		RequiredCapabilities: nil,     // No special capabilities required
		MinKernelVersion:     "2.6.7", // NUMA support in /sys
	}

	return &NUMACollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeNUMA,
			"NUMA Memory Collector",
			logger,
			config,
			capabilities,
		),
		sysNodesPath: filepath.Join(config.HostSysPath, "devices", "system", "node"),
		procPath:     config.HostProcPath,
	}, nil
}

func (c *NUMACollector) Collect(ctx context.Context) (any, error) {
	// First check if this is a NUMA system
	nodes, err := c.detectNUMANodes()
	if err != nil {
		return nil, fmt.Errorf("failed to detect NUMA nodes: %w", err)
	}

	if len(nodes) <= 1 {
		// Not a NUMA system or NUMA disabled
		return &performance.NUMAStats{
			Enabled:   false,
			NodeCount: len(nodes),
		}, nil
	}

	stats := &performance.NUMAStats{
		Enabled:   true,
		NodeCount: len(nodes),
		Nodes:     make([]performance.NUMANodeStats, 0, len(nodes)),
	}

	// Collect per-node statistics
	for _, nodeID := range nodes {
		node, err := c.collectNodeStats(nodeID)
		if err != nil {
			c.Logger().Error(err, "failed to collect NUMA node stats", "node", nodeID)
			continue
		}
		stats.Nodes = append(stats.Nodes, node)
	}

	// Collect system-wide NUMA info if available
	if err := c.collectSystemNUMAInfo(stats); err != nil {
		// Non-fatal: log and continue
		c.Logger().V(1).Info("failed to collect system NUMA info", "error", err)
	}

	return stats, nil
}

// detectNUMANodes returns a list of NUMA node IDs
func (c *NUMACollector) detectNUMANodes() ([]int, error) {
	entries, err := os.ReadDir(c.sysNodesPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No NUMA support in kernel
			return []int{}, nil
		}
		return nil, err
	}

	nodes := make([]int, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "node") {
			continue
		}

		// Extract node ID from "nodeN" directory name
		nodeIDStr := strings.TrimPrefix(entry.Name(), "node")
		nodeID, err := strconv.Atoi(nodeIDStr)
		if err != nil {
			continue
		}
		nodes = append(nodes, nodeID)
	}

	return nodes, nil
}

// collectNodeStats collects statistics for a specific NUMA node
//
// Error handling strategy:
// - numastat is critical - returns error if unavailable (contains allocation counters)
// - meminfo is important - logs warning but continues (memory usage data)
// - cpulist is optional - logs debug but continues (topology information)
// - distance is optional - logs debug but continues (performance hints)
//
// This ensures we always get the critical performance counters while gracefully
// degrading for optional topology and memory usage information.
func (c *NUMACollector) collectNodeStats(nodeID int) (performance.NUMANodeStats, error) {
	node := performance.NUMANodeStats{
		ID: nodeID,
	}

	nodePath := filepath.Join(c.sysNodesPath, fmt.Sprintf("node%d", nodeID))

	// Collect NUMA statistics (CRITICAL - these are the main performance counters)
	numastat, err := c.readNodeNumaStat(filepath.Join(nodePath, "numastat"))
	if err != nil {
		return node, fmt.Errorf("failed to read critical numastat for node %d: %w", nodeID, err)
	}
	node.NumaHit = numastat.numaHit
	node.NumaMiss = numastat.numaMiss
	node.NumaForeign = numastat.numaForeign
	node.InterleaveHit = numastat.interleaveHit
	node.LocalNode = numastat.localNode
	node.OtherNode = numastat.otherNode

	// Collect memory info (IMPORTANT but not critical)
	meminfo, err := c.readNodeMeminfo(filepath.Join(nodePath, "meminfo"))
	if err != nil {
		c.Logger().V(1).Info("Failed to read node meminfo", "node", nodeID, "error", err)
	} else {
		node.MemTotal = meminfo.memTotal
		node.MemFree = meminfo.memFree
		node.MemUsed = meminfo.memUsed
		node.FilePages = meminfo.filePages
		node.AnonPages = meminfo.anonPages
	}

	// Collect CPU list for this node (OPTIONAL - topology information)
	cpus, err := c.readNodeCPUs(filepath.Join(nodePath, "cpulist"))
	if err != nil {
		c.Logger().V(2).Info("Failed to read node CPU list", "node", nodeID, "error", err)
	} else {
		node.CPUs = cpus
	}

	// Read distance to other nodes (OPTIONAL - performance hints)
	distances, err := c.readNodeDistances(filepath.Join(nodePath, "distance"))
	if err != nil {
		c.Logger().V(2).Info("Failed to read node distances", "node", nodeID, "error", err)
	} else {
		node.Distances = distances
	}

	return node, nil
}

// nodeMeminfo holds parsed data from node*/meminfo
type nodeMeminfo struct {
	memTotal  uint64
	memFree   uint64
	memUsed   uint64
	filePages uint64
	anonPages uint64
}

// readNodeMeminfo parses /sys/devices/system/node/node*/meminfo
//
// Format is similar to /proc/meminfo but with per-node values:
//
//	Node 0 MemTotal:       67108864 kB
//	Node 0 MemFree:        12345678 kB
//	Node 0 MemUsed:        54763186 kB
//
// Error handling:
// - File not found returns error (caller decides if critical)
// - Malformed lines are skipped with debug logging
// - Returns partial data if some fields are missing
func (c *NUMACollector) readNodeMeminfo(path string) (*nodeMeminfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info := &nodeMeminfo{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		// Format: "Node N FieldName: value kB"
		fieldName := strings.TrimSuffix(fields[2], ":")
		value, err := strconv.ParseUint(fields[3], 10, 64)
		if err != nil {
			continue
		}

		// Convert from kB to bytes
		value *= 1024

		switch fieldName {
		case "MemTotal":
			info.memTotal = value
		case "MemFree":
			info.memFree = value
		case "MemUsed":
			info.memUsed = value
		case "FilePages":
			info.filePages = value
		case "AnonPages":
			info.anonPages = value
		}
	}

	return info, scanner.Err()
}

// nodeNumaStat holds parsed data from node*/numastat
type nodeNumaStat struct {
	numaHit       uint64
	numaMiss      uint64
	numaForeign   uint64
	interleaveHit uint64
	localNode     uint64
	otherNode     uint64
}

// readNodeNumaStat parses /sys/devices/system/node/node*/numastat
//
// Format: "stat_name value" (values are in pages, not kB)
//
//	numa_hit 1234567890
//	numa_miss 12345
//	numa_foreign 54321
//	interleave_hit 9876
//	local_node 1234567000
//	other_node 890
//
// These are monotonically increasing counters since boot.
//
// Error handling:
// - File not found returns error (critical file)
// - Malformed lines are skipped
// - Returns all available statistics
func (c *NUMACollector) readNodeNumaStat(path string) (*nodeNumaStat, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stats := &nodeNumaStat{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		switch fields[0] {
		case "numa_hit":
			stats.numaHit = value
		case "numa_miss":
			stats.numaMiss = value
		case "numa_foreign":
			stats.numaForeign = value
		case "interleave_hit":
			stats.interleaveHit = value
		case "local_node":
			stats.localNode = value
		case "other_node":
			stats.otherNode = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Validate we got at least some data
	if stats.numaHit == 0 && stats.numaMiss == 0 && stats.numaForeign == 0 &&
		stats.interleaveHit == 0 && stats.localNode == 0 && stats.otherNode == 0 {
		return nil, fmt.Errorf("no valid NUMA statistics found in %s", path)
	}

	return stats, nil
}

// readNodeCPUs parses /sys/devices/system/node/node*/cpulist
// Format can be: "0-3,8-11" or "0,2,4,6" or just "0"
func (c *NUMACollector) readNodeCPUs(path string) ([]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cpuList := strings.TrimSpace(string(data))
	if cpuList == "" {
		return []int{}, nil
	}

	cpus := make([]int, 0)

	// Parse comma-separated ranges
	for _, part := range strings.Split(cpuList, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			// Parse range like "0-3"
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				continue
			}
			start, err1 := strconv.Atoi(rangeParts[0])
			end, err2 := strconv.Atoi(rangeParts[1])
			if err1 != nil || err2 != nil {
				continue
			}
			for i := start; i <= end; i++ {
				cpus = append(cpus, i)
			}
		} else {
			// Single CPU number
			cpu, err := strconv.Atoi(part)
			if err == nil {
				cpus = append(cpus, cpu)
			}
		}
	}

	return cpus, nil
}

// readNodeDistances parses /sys/devices/system/node/node*/distance
// Format: space-separated distances to all nodes (including self)
// Example for node0 in a 2-node system: "10 21" (10 to self, 21 to node1)
func (c *NUMACollector) readNodeDistances(path string) ([]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	distanceStr := strings.TrimSpace(string(data))
	if distanceStr == "" {
		return []int{}, nil
	}

	parts := strings.Fields(distanceStr)
	distances := make([]int, len(parts))

	for i, part := range parts {
		distance, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid distance value: %s", part)
		}
		distances[i] = distance
	}

	return distances, nil
}

// collectSystemNUMAInfo collects system-wide NUMA information
func (c *NUMACollector) collectSystemNUMAInfo(stats *performance.NUMAStats) error {
	// Check if NUMA balancing is enabled
	balancingPath := filepath.Join(c.procPath, "sys", "kernel", "numa_balancing")
	if data, err := os.ReadFile(balancingPath); err == nil {
		stats.AutoBalance = strings.TrimSpace(string(data)) == "1"
	}

	// TODO: Add more system-wide NUMA metrics if needed:
	// - /proc/zoneinfo for per-zone, per-node memory info
	// - /proc/pagetypeinfo for memory fragmentation per node
	// - /proc/buddyinfo for free memory fragmentation

	return nil
}
