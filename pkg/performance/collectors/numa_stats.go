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
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

func init() {
	performance.Register(performance.MetricTypeNUMAStats, performance.PartialNewContinuousPointCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewNUMAStatsCollector(logger, config)
		},
	))
}

// Compile-time interface check
var _ performance.DeltaAwareCollector = (*NUMAStatsCollector)(nil)

// NUMAStatsCollector collects runtime NUMA performance statistics
//
// This collector runs continuously to monitor NUMA allocation patterns
// and memory usage. It focuses on runtime performance data that changes
// during system operation:
// - Allocation counters (hits, misses, foreign allocations)
// - Current memory usage per node
// - Auto-balancing status
//
// Key performance indicators:
// - High numa_hit and low numa_miss values indicate good NUMA locality
// - High numa_foreign values suggest memory pressure causing cross-node allocations
// - Remote memory access can be 2-3x slower than local access
//
// Data sources:
// - /sys/devices/system/node/node*/numastat - Allocation counters (CRITICAL)
// - /sys/devices/system/node/node*/meminfo - Current memory usage (IMPORTANT)
// - /proc/sys/kernel/numa_balancing - Current balancing state (OPTIONAL)
//
// Reference: https://www.kernel.org/doc/html/latest/admin-guide/mm/numa.html
type NUMAStatsCollector struct {
	performance.BaseDeltaCollector
	sysNodesPath string
	procPath     string
}

func NewNUMAStatsCollector(logger logr.Logger, config performance.CollectionConfig) (*NUMAStatsCollector, error) {
	// Validate configuration - NUMA stats collector requires both proc and sys paths
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

	return &NUMAStatsCollector{
		BaseDeltaCollector: performance.NewBaseDeltaCollector(
			performance.MetricTypeNUMAStats,
			"NUMA Runtime Stats Collector",
			logger,
			config,
			capabilities,
		),
		sysNodesPath: filepath.Join(config.HostSysPath, "devices", "system", "node"),
		procPath:     config.HostProcPath,
	}, nil
}

func (c *NUMAStatsCollector) Collect(ctx context.Context) (any, error) {
	// First check if this is a NUMA system
	nodes, err := c.detectNUMANodes()
	if err != nil {
		return nil, fmt.Errorf("failed to detect NUMA nodes: %w", err)
	}

	stats := &performance.NUMAStatistics{
		Enabled:   len(nodes) > 1,
		NodeCount: len(nodes),
		Nodes:     make([]performance.NUMANodeStatistics, 0, len(nodes)),
	}

	// Check current auto-balancing state
	balancingPath := filepath.Join(c.procPath, "sys", "kernel", "numa_balancing")
	if data, err := os.ReadFile(balancingPath); err == nil {
		stats.AutoBalanceEnabled = strings.TrimSpace(string(data)) == "1"
	}

	// If not a NUMA system, return basic info
	if !stats.Enabled {
		return stats, nil
	}

	// Collect runtime statistics for each node
	for _, nodeID := range nodes {
		node, err := c.collectNodeStats(nodeID)
		if err != nil {
			c.Logger().Error(err, "failed to collect NUMA node stats", "node", nodeID)
			continue
		}
		stats.Nodes = append(stats.Nodes, node)
	}

	return stats, nil
}

// detectNUMANodes returns a list of NUMA node IDs by scanning the sysfs directory
//
// Error handling strategy:
// - Missing /sys/devices/system/node directory means no NUMA support - returns empty slice
// - Invalid node directory names are skipped
// - At least one valid node directory is required for NUMA systems
func (c *NUMAStatsCollector) detectNUMANodes() ([]int, error) {
	entries, err := os.ReadDir(c.sysNodesPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No NUMA support in kernel
			return []int{}, nil
		}
		return nil, fmt.Errorf("failed to read NUMA nodes directory %s: %w", c.sysNodesPath, err)
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
			c.Logger().V(2).Info("Skipping invalid node directory", "name", entry.Name())
			continue
		}
		nodes = append(nodes, nodeID)
	}

	return nodes, nil
}

// collectNodeStats collects runtime statistics for a specific NUMA node
//
// Error handling strategy:
//   - numastat is critical - returns error if unavailable (contains allocation counters)
//   - meminfo is important - logs warning but continues (memory usage data)
//   - This ensures we always get the critical performance counters while gracefully
//     degrading for memory usage information
func (c *NUMAStatsCollector) collectNodeStats(nodeID int) (performance.NUMANodeStatistics, error) {
	node := performance.NUMANodeStatistics{
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

	// Collect current memory usage (IMPORTANT but not critical)
	meminfo, err := c.readNodeMeminfo(filepath.Join(nodePath, "meminfo"))
	if err != nil {
		c.Logger().V(1).Info("Failed to read node meminfo", "node", nodeID, "error", err)
	} else {
		node.MemFree = meminfo.memFree
		node.MemUsed = meminfo.memUsed
		node.FilePages = meminfo.filePages
		node.AnonPages = meminfo.anonPages
	}

	return node, nil
}

// runtimeNodeMeminfo holds parsed runtime memory data from node*/meminfo
// Different from numa.go's nodeMeminfo to avoid naming conflicts
type runtimeNodeMeminfo struct {
	memFree   uint64
	memUsed   uint64
	filePages uint64
	anonPages uint64
}

// readNodeMeminfo parses runtime memory fields from /sys/devices/system/node/node*/meminfo
//
// Format is similar to /proc/meminfo but with per-node values:
//
//	Node 0 MemTotal:       67108864 kB
//	Node 0 MemFree:        12345678 kB
//	Node 0 MemUsed:        54763186 kB
//	Node 0 FilePages:      12345678 kB
//	Node 0 AnonPages:      42417508 kB
//
// We skip MemTotal since that's static topology information handled by NUMAInfoCollector.
//
// Error handling:
// - File not found returns error (caller decides if critical)
// - Malformed lines are skipped with debug logging
// - Returns partial data if some fields are missing
func (c *NUMAStatsCollector) readNodeMeminfo(path string) (*runtimeNodeMeminfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer file.Close()

	info := &runtimeNodeMeminfo{}
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
			c.Logger().V(2).Info("Skipping invalid meminfo field", "line", line, "error", err)
			continue
		}

		// Convert from kB to bytes
		value *= 1024

		switch fieldName {
		case "MemFree":
			info.memFree = value
		case "MemUsed":
			info.memUsed = value
		case "FilePages":
			info.filePages = value
		case "AnonPages":
			info.anonPages = value
			// Skip MemTotal - that's handled by NUMAInfoCollector
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading %s: %w", path, err)
	}

	return info, nil
}

// runtimeNodeNumaStat holds parsed allocation counter data from node*/numastat
// Different from numa.go's nodeNumaStat to avoid naming conflicts
type runtimeNodeNumaStat struct {
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
// - Validates that at least some data was found
func (c *NUMAStatsCollector) readNodeNumaStat(path string) (*runtimeNodeNumaStat, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer file.Close()

	stats := &runtimeNodeNumaStat{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			c.Logger().V(2).Info("Skipping invalid numastat field", "line", line, "error", err)
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
		return nil, fmt.Errorf("error reading %s: %w", path, err)
	}

	// Validate we got at least some data
	if stats.numaHit == 0 && stats.numaMiss == 0 && stats.numaForeign == 0 &&
		stats.interleaveHit == 0 && stats.localNode == 0 && stats.otherNode == 0 {
		return nil, fmt.Errorf("no valid NUMA statistics found in %s", path)
	}

	return stats, nil
}

func (c *NUMAStatsCollector) CollectWithDelta(ctx context.Context, config performance.DeltaConfig) (any, error) {
	stats, err := c.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to collect NUMA stats: %w", err)
	}

	numaStats := stats.(*performance.NUMAStatistics)
	currentTime := time.Now()

	if c.HasDeltaState() {
		if should, reason := c.ShouldCalculateDeltas(currentTime); should {
			previous := c.LastSnapshot.(*performance.NUMAStatistics)
			c.calculateNUMADeltas(numaStats, previous, currentTime, config)
		} else {
			c.Logger().V(2).Info("Skipping delta calculation", "reason", reason)
		}
	}

	c.UpdateDeltaState(numaStats, currentTime)
	c.Logger().V(1).Info("Collected NUMA statistics with delta support", "nodes", len(numaStats.Nodes))
	return numaStats, nil
}

func (c *NUMAStatsCollector) calculateNUMADeltas(
	current, previous *performance.NUMAStatistics,
	currentTime time.Time,
	config performance.DeltaConfig,
) {
	interval := currentTime.Sub(c.LastTime)

	// Create a map of previous nodes by ID for efficient lookup
	prevNodesMap := make(map[int]*performance.NUMANodeStatistics)
	for i, prevNode := range previous.Nodes {
		prevNodesMap[prevNode.ID] = &previous.Nodes[i]
	}

	// Calculate deltas for each current node
	for i := range current.Nodes {
		currentNode := &current.Nodes[i]
		prevNode, exists := prevNodesMap[currentNode.ID]
		if !exists {
			// New node - skip delta calculation
			c.Logger().V(2).Info("New NUMA node detected, skipping delta calculation", "nodeID", currentNode.ID)
			continue
		}

		c.calculateNUMANodeDeltas(currentNode, prevNode, interval, config)
	}
}

func (c *NUMAStatsCollector) calculateNUMANodeDeltas(
	current, previous *performance.NUMANodeStatistics,
	interval time.Duration,
	config performance.DeltaConfig,
) {
	var resetDetected bool

	delta := &performance.NUMADeltaData{}

	calculateField := func(currentVal, previousVal uint64) uint64 {
		deltaVal, reset := c.CalculateUint64Delta(currentVal, previousVal, interval)
		resetDetected = resetDetected || reset
		return deltaVal
	}

	delta.NumaHit = calculateField(current.NumaHit, previous.NumaHit)
	delta.NumaMiss = calculateField(current.NumaMiss, previous.NumaMiss)
	delta.NumaForeign = calculateField(current.NumaForeign, previous.NumaForeign)
	delta.InterleaveHit = calculateField(current.InterleaveHit, previous.InterleaveHit)
	delta.LocalNode = calculateField(current.LocalNode, previous.LocalNode)
	delta.OtherNode = calculateField(current.OtherNode, previous.OtherNode)

	if !resetDetected {
		intervalSecs := interval.Seconds()
		if intervalSecs > 0 {
			delta.NumaHitPerSec = uint64(float64(delta.NumaHit) / intervalSecs)
			delta.NumaMissPerSec = uint64(float64(delta.NumaMiss) / intervalSecs)
			delta.NumaForeignPerSec = uint64(float64(delta.NumaForeign) / intervalSecs)
			delta.InterleaveHitPerSec = uint64(float64(delta.InterleaveHit) / intervalSecs)
			delta.LocalNodePerSec = uint64(float64(delta.LocalNode) / intervalSecs)
			delta.OtherNodePerSec = uint64(float64(delta.OtherNode) / intervalSecs)
		}
	}

	// Use composition helper to set metadata
	c.PopulateMetadata(delta, time.Now(), resetDetected)
	current.Delta = delta

	if resetDetected {
		c.Logger().V(1).Info("Counter reset detected in NUMA statistics", "nodeID", current.ID)
	}
}
