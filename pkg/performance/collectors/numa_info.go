// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
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
	performance.Register(performance.MetricTypeNUMAInfo, performance.PartialNewOnceContinuousCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewNUMAInfoCollector(logger, config)
		},
	))
}

// Compile-time interface check
var _ performance.PointCollector = (*NUMAInfoCollector)(nil)

// NUMAInfoCollector collects static NUMA hardware topology information
//
// This collector runs once at startup to determine the NUMA hardware layout.
// It provides static information about:
// - Number of NUMA nodes
// - CPU assignment to each node
// - Total memory per node
// - Distance matrix between nodes
// - NUMA balancing availability
//
// Unlike the runtime NUMA statistics collector, this focuses purely on
// hardware topology that doesn't change during system operation.
//
// Data sources:
// - /sys/devices/system/node/node*/cpulist - CPU assignments
// - /sys/devices/system/node/node*/meminfo - MemTotal for total memory
// - /sys/devices/system/node/node*/distance - Inter-node distances
// - /proc/sys/kernel/numa_balancing - Availability of auto-balancing
//
// Reference: https://www.kernel.org/doc/html/latest/admin-guide/mm/numa.html
type NUMAInfoCollector struct {
	performance.BaseCollector
	sysNodesPath string
	procPath     string
}

func NewNUMAInfoCollector(logger logr.Logger, config performance.CollectionConfig) (*NUMAInfoCollector, error) {
	// Validate configuration - NUMA info collector requires both proc and sys paths
	if err := config.Validate(performance.ValidateOptions{
		RequireHostProcPath: true,
		RequireHostSysPath:  true,
	}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    true,
		SupportsContinuous: false,
		RequiresRoot:       false,
		RequiresEBPF:       false,
		MinKernelVersion:   "2.6.7", // NUMA support in /sys
	}

	return &NUMAInfoCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeNUMAInfo,
			"NUMA Hardware Info Collector",
			logger,
			config,
			capabilities,
		),
		sysNodesPath: filepath.Join(config.HostSysPath, "devices", "system", "node"),
		procPath:     config.HostProcPath,
	}, nil
}

func (c *NUMAInfoCollector) Collect(ctx context.Context) (any, error) {
	// First check if this is a NUMA system
	nodes, err := c.detectNUMANodes()
	if err != nil {
		return nil, fmt.Errorf("failed to detect NUMA nodes: %w", err)
	}

	info := &performance.NUMAInfo{
		Enabled:   len(nodes) > 1,
		NodeCount: len(nodes),
		Nodes:     make([]performance.NUMANodeInfo, 0, len(nodes)),
	}

	// Check if NUMA balancing is available (not necessarily enabled)
	balancingPath := filepath.Join(c.procPath, "sys", "kernel", "numa_balancing")
	if _, err := os.Stat(balancingPath); err == nil {
		info.BalancingAvailable = true
	}

	// If not a NUMA system, return basic info
	if !info.Enabled {
		return info, nil
	}

	// Collect static topology information for each node
	for _, nodeID := range nodes {
		node, err := c.collectNodeInfo(nodeID)
		if err != nil {
			c.Logger().Error(err, "failed to collect NUMA node info", "node", nodeID)
			continue
		}
		info.Nodes = append(info.Nodes, node)
	}

	return info, nil
}

// detectNUMANodes returns a list of NUMA node IDs by scanning the sysfs directory
//
// Error handling strategy:
// - Missing /sys/devices/system/node directory means no NUMA support - returns empty slice
// - Invalid node directory names are skipped
// - At least one valid node directory is required for NUMA systems
func (c *NUMAInfoCollector) detectNUMANodes() ([]int, error) {
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

// collectNodeInfo collects static topology information for a specific NUMA node
//
// Error handling strategy:
// - cpulist is important for topology - logs warning but continues if missing
// - meminfo MemTotal is important - logs warning but continues if missing
// - distance is optional - logs debug if missing
// - Never fails completely - returns partial information when possible
func (c *NUMAInfoCollector) collectNodeInfo(nodeID int) (performance.NUMANodeInfo, error) {
	node := performance.NUMANodeInfo{
		ID: nodeID,
	}

	nodePath := filepath.Join(c.sysNodesPath, fmt.Sprintf("node%d", nodeID))

	// Collect CPU list for this node (IMPORTANT for topology)
	cpus, err := c.readNodeCPUs(filepath.Join(nodePath, "cpulist"))
	if err != nil {
		c.Logger().V(1).Info("Failed to read node CPU list", "node", nodeID, "error", err)
	} else {
		node.CPUs = cpus
	}

	// Collect total memory for this node (IMPORTANT for capacity planning)
	totalMem, err := c.readNodeTotalMemory(filepath.Join(nodePath, "meminfo"))
	if err != nil {
		c.Logger().V(1).Info("Failed to read node total memory", "node", nodeID, "error", err)
	} else {
		node.TotalMemory = totalMem
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

// readNodeCPUs parses /sys/devices/system/node/node*/cpulist
// Format can be: "0-3,8-11" or "0,2,4,6" or just "0"
//
// Error handling:
// - File not found returns error
// - Empty file returns empty slice (valid for nodes with no CPUs)
// - Malformed ranges are skipped with debug logging
func (c *NUMAInfoCollector) readNodeCPUs(path string) ([]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read CPU list from %s: %w", path, err)
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
				c.Logger().V(2).Info("Skipping invalid CPU range", "range", part)
				continue
			}
			start, err1 := strconv.Atoi(rangeParts[0])
			end, err2 := strconv.Atoi(rangeParts[1])
			if err1 != nil || err2 != nil {
				c.Logger().V(2).Info("Skipping invalid CPU range", "range", part, "error", fmt.Sprintf("%v/%v", err1, err2))
				continue
			}
			for i := start; i <= end; i++ {
				cpus = append(cpus, i)
			}
		} else {
			// Single CPU number
			cpu, err := strconv.Atoi(part)
			if err != nil {
				c.Logger().V(2).Info("Skipping invalid CPU number", "cpu", part, "error", err)
				continue
			}
			cpus = append(cpus, cpu)
		}
	}

	return cpus, nil
}

// readNodeTotalMemory reads the MemTotal field from /sys/devices/system/node/node*/meminfo
//
// Format is similar to /proc/meminfo but with per-node values:
//
//	Node 0 MemTotal:       67108864 kB
//	Node 0 MemFree:        12345678 kB
//	...
//
// We only need MemTotal for static topology information.
//
// Error handling:
// - File not found returns error
// - MemTotal field missing returns error
// - Malformed lines are skipped
func (c *NUMAInfoCollector) readNodeTotalMemory(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("failed to read meminfo from %s: %w", path, err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		// Format: "Node N FieldName: value kB"
		fieldName := strings.TrimSuffix(fields[2], ":")
		if fieldName == "MemTotal" {
			value, err := strconv.ParseUint(fields[3], 10, 64)
			if err != nil {
				continue
			}
			// Convert from kB to bytes
			return value * 1024, nil
		}
	}

	return 0, fmt.Errorf("MemTotal not found in %s", path)
}

// readNodeDistances parses /sys/devices/system/node/node*/distance
// Format: space-separated distances to all nodes (including self)
// Example for node0 in a 2-node system: "10 21" (10 to self, 21 to node1)
//
// Error handling:
// - File not found returns error
// - Empty file returns empty slice
// - Invalid distance values return error
func (c *NUMAInfoCollector) readNodeDistances(path string) ([]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read distances from %s: %w", path, err)
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
			return nil, fmt.Errorf("invalid distance value %q in %s: %w", part, path, err)
		}
		distances[i] = distance
	}

	return distances, nil
}
