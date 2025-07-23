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
	performance.Register(performance.MetricTypeMemoryInfo, performance.PartialNewOnceContinuousCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewMemoryInfoCollector(logger, config)
		},
	))
}

// MemoryInfoCollector collects memory hardware configuration and NUMA topology.
//
// Purpose: Hardware inventory and NUMA topology discovery
// This collector provides static memory hardware configuration for capacity
// planning, NUMA-aware scheduling, and hardware inventory. It discovers the
// physical memory architecture rather than runtime usage.
//
// Key Differences from MemoryCollector:
// - MemoryInfoCollector: Provides hardware configuration (static NUMA topology)
// - MemoryCollector: Provides runtime statistics (dynamic memory usage)
// - This collector is for inventory; MemoryCollector is for monitoring
// - Reads only MemTotal from /proc/meminfo, focuses on NUMA topology from sysfs
//
// Use Cases:
// - Hardware inventory and asset tracking
// - NUMA-aware application deployment
// - Capacity planning and sizing
// - Understanding system memory architecture
// - Optimizing memory access patterns
//
// Data Sources and Standardization:
//
// This collector reads memory information from multiple sources with varying levels of standardization:
//
// KERNEL-GUARANTEED DATA (standardized format):
// These files are managed by the kernel memory management subsystem with consistent formats:
//   - /proc/meminfo                                     - Total system memory (kernel-standardized)
//   - /sys/devices/system/node/nodeX/meminfo            - NUMA node memory (kernel-standardized)
//   - /sys/devices/system/node/nodeX/cpulist            - CPU affinity per NUMA node (kernel-standardized)
//   - /sys/devices/system/node/node* directory structure - NUMA topology (kernel-standardized)
//
// FIRMWARE/HARDWARE-DEPENDENT DATA (may vary or be unavailable):
// These data sources depend on firmware tables, hardware detection, or vendor-specific implementations:
//   - /sys/devices/system/memory/                       - Memory block information (architecture-dependent)
//   - /sys/devices/system/edac/                         - Error detection/correction (hardware-dependent)
//   - Memory module specifications (type/speed/timing) - Highly vendor/platform-specific, often unavailable
//
// Data Reliability by Source:
//
// 1. KERNEL-GUARANTEED (/proc/meminfo):
//   - Format: "MemTotal: XXXXX kB" (always in kilobytes)
//   - Always available on all Linux systems
//   - Represents total usable RAM as detected by kernel
//   - Excludes memory reserved for firmware, kernel, or hardware
//
// 2. KERNEL-GUARANTEED (NUMA sysfs):
//   - /sys/devices/system/node/nodeX/meminfo format: "Node X MemTotal: XXXXX kB"
//   - cpulist format: "0-3,8-11" (comma-separated ranges)
//   - Reflects kernel's NUMA topology detection
//   - Available on NUMA systems, gracefully degrades on UMA systems
//
// 3. HARDWARE-DEPENDENT (EDAC):
//   - Error correction capabilities and statistics
//   - Only available on hardware with ECC memory support
//   - Driver-dependent availability
//
// Important Notes:
// - Memory reported by kernel may be less than installed due to hardware reservations
// - NUMA node numbering is not guaranteed to be contiguous (e.g., node0, node2, node4)
// - Virtual machines may not expose accurate memory hardware information
// - Memory controllers don't expose detailed specification information to the OS
// - Memory module specifications (type, speed, timing) are typically not available through kernel interfaces
//
// References:
// - Linux memory management: https://www.kernel.org/doc/html/latest/admin-guide/mm/
// - NUMA topology: https://www.kernel.org/doc/html/latest/admin-guide/mm/numa_memory_policy.html
// - /proc/meminfo documentation: https://www.kernel.org/doc/Documentation/filesystems/proc.txt
// - NUMA sysfs ABI: https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-devices-system-node
type MemoryInfoCollector struct {
	performance.BaseCollector
	meminfoPath    string
	nodeSystemPath string
}

// Compile-time interface check
var _ performance.PointCollector = (*MemoryInfoCollector)(nil)

func NewMemoryInfoCollector(logger logr.Logger, config performance.CollectionConfig) (*MemoryInfoCollector, error) {
	if err := config.Validate(performance.ValidateOption{RequireHostProcPath: true, RequireHostSysPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    true,
		SupportsContinuous: false,
		RequiresRoot:       false,
		RequiresEBPF:       false,
		MinKernelVersion:   "2.6.0",
	}

	return &MemoryInfoCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeMemoryInfo,
			"Memory Hardware Info Collector",
			logger,
			config,
			capabilities,
		),
		meminfoPath:    filepath.Join(config.HostProcPath, "meminfo"),
		nodeSystemPath: filepath.Join(config.HostSysPath, "devices", "system", "node"),
	}, nil
}

func (c *MemoryInfoCollector) Collect(ctx context.Context) (any, error) {
	return c.collectMemoryInfo()
}

// collectMemoryInfo discovers and collects memory hardware configuration.
//
// This method implements the core collection logic:
// 1. Reads total system memory from /proc/meminfo (kernel-guaranteed)
// 2. Discovers NUMA topology from /sys/devices/system/node/ (kernel-guaranteed)
// 3. Gracefully degrades to single-node configuration if NUMA is unavailable
//
// Collection Strategy:
// - Always start with kernel-guaranteed data sources (/proc/meminfo, NUMA sysfs)
// - Use graceful fallback for systems without NUMA support
// - Future enhancement: Add DMI/SMBIOS memory module information for detailed hardware specs
//
// Error Handling:
// - /proc/meminfo parsing failures are treated as fatal (memory info is essential)
// - NUMA parsing failures result in graceful degradation to single-node assumption
// - Missing NUMA nodes are handled by creating a synthetic node with all CPUs
//
// See: https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-devices-system-node
func (c *MemoryInfoCollector) collectMemoryInfo() (*performance.MemoryInfo, error) {
	info := &performance.MemoryInfo{
		NUMANodes: make([]performance.NUMANode, 0),
	}

	// KERNEL-GUARANTEED: Get total system memory from /proc/meminfo
	// This is the most reliable source - always available on Linux systems
	if err := c.parseTotalMemory(info); err != nil {
		return nil, fmt.Errorf("failed to parse meminfo: %w", err)
	}

	// KERNEL-GUARANTEED: Get NUMA configuration from sysfs
	// Gracefully degrades to single-node if NUMA is unavailable
	c.parseNUMAInfo(info)

	return info, nil
}

// parseTotalMemory reads total system memory from /proc/meminfo.
//
// Data Source: /proc/meminfo (KERNEL-GUARANTEED)
//
// This method reads ONLY the MemTotal field from /proc/meminfo for hardware
// inventory purposes. This is the only field MemoryInfoCollector reads from
// /proc/meminfo, as it focuses on hardware configuration rather than runtime
// statistics (which are handled by MemoryCollector).
//
// The MemTotal value represents the total amount of usable RAM as detected
// and managed by the kernel memory management system.
//
// Format Specification:
// - File format: "MemTotal: XXXXX kB" (always in kilobytes)
// - Always present on all Linux systems (kernel-guaranteed)
// - Value represents usable RAM excluding memory reserved for:
//   - Firmware/BIOS (e.g., SMM, ACPI tables)
//   - Kernel code and data structures
//   - Hardware reservations (e.g., graphics memory)
//
// Important Notes:
// - The value may be less than physically installed memory due to hardware reservations
// - On virtual machines, this reflects the assigned memory, not physical host memory
// - The kernel always reports this value in kilobytes (multiply by 1024 for bytes)
// - This is the most reliable source for total system memory across all Linux distributions
//
// References:
// - /proc/meminfo documentation: https://www.kernel.org/doc/Documentation/filesystems/proc.txt
// - Linux memory management: https://www.kernel.org/doc/html/latest/admin-guide/mm/
func (c *MemoryInfoCollector) parseTotalMemory(info *performance.MemoryInfo) error {
	file, err := os.Open(c.meminfoPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				memKB, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil {
					info.TotalBytes = memKB * 1024 // Convert KB to bytes
					return nil
				}
			}
		}
	}

	if scanner.Err() != nil {
		return scanner.Err()
	}

	return fmt.Errorf("MemTotal not found in %s", c.meminfoPath)
}

// parseNUMAInfo discovers and parses NUMA (Non-Uniform Memory Access) topology.
//
// Data Sources: /sys/devices/system/node/ (KERNEL-GUARANTEED)
//
// This method reads NUMA topology information from the Linux sysfs, which reflects
// the kernel's detection and management of NUMA hardware configurations.
//
// NUMA Discovery Process:
// 1. Enumerate NUMA nodes from /sys/devices/system/node/node[0-9]*
// 2. For each node, read memory and CPU affinity information
// 3. Gracefully degrade to single-node configuration if no NUMA nodes found
//
// Data Sources per Node:
// - /sys/devices/system/node/nodeX/meminfo     - Per-node memory information (kernel-guaranteed)
// - /sys/devices/system/node/nodeX/cpulist     - CPU affinity for node (kernel-guaranteed)
// - Directory existence indicates NUMA topology as detected by kernel
//
// Format Specifications:
// - Node directories: node0, node1, node2, etc. (numbering may not be contiguous)
// - meminfo format: "Node X MemTotal: XXXXX kB" (always in kilobytes)
// - cpulist format: "0-3,8-11" (comma-separated ranges, kernel-standardized)
//
// Graceful Degradation:
// - If no NUMA nodes found: Create synthetic single node with all CPUs
// - If individual node parsing fails: Skip that node, continue with others
// - If CPU enumeration fails: Create node with empty CPU list
//
// Important Notes:
// - NUMA node numbering is not guaranteed to be contiguous (e.g., node0, node2, node4)
// - Virtual machines may not expose NUMA topology even if host has NUMA
// - Some systems disable NUMA in firmware, resulting in single-node configuration
// - Memory amounts per node may not sum to total system memory due to kernel reservations
//
// References:
// - NUMA memory policy: https://www.kernel.org/doc/html/latest/admin-guide/mm/numa_memory_policy.html
// - NUMA sysfs ABI: https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-devices-system-node
func (c *MemoryInfoCollector) parseNUMAInfo(info *performance.MemoryInfo) {
	// KERNEL-GUARANTEED: Enumerate NUMA nodes from sysfs
	// Pattern matches node0, node1, node2, etc.
	nodePattern := filepath.Join(c.nodeSystemPath, "node[0-9]*")
	nodeMatches, err := filepath.Glob(nodePattern)
	if err != nil || len(nodeMatches) == 0 {
		// GRACEFUL DEGRADATION: No NUMA nodes found - assume single node (UMA system)
		// This handles systems without NUMA support or where NUMA is disabled
		if info.TotalBytes > 0 {
			info.NUMANodes = append(info.NUMANodes, performance.NUMANode{
				NodeID:     0,
				TotalBytes: info.TotalBytes,
				CPUs:       c.getAllCPUs(),
			})
		}
		return
	}

	// KERNEL-GUARANTEED: Parse each discovered NUMA node
	for _, nodePath := range nodeMatches {
		nodeID := c.extractNodeID(nodePath)
		if nodeID < 0 {
			continue // Skip invalid node directories
		}

		node := performance.NUMANode{
			NodeID: nodeID,
			CPUs:   make([]int32, 0),
		}

		// KERNEL-GUARANTEED: Get per-node memory information
		c.parseNodeMemory(&node, nodePath)

		// KERNEL-GUARANTEED: Get CPU affinity for this node
		c.parseNodeCPUs(&node, nodePath)

		info.NUMANodes = append(info.NUMANodes, node)
	}
}

func (c *MemoryInfoCollector) extractNodeID(nodePath string) int32 {
	base := filepath.Base(nodePath)
	if strings.HasPrefix(base, "node") {
		idStr := strings.TrimPrefix(base, "node")
		if id, err := strconv.ParseInt(idStr, 10, 32); err == nil {
			return int32(id)
		}
	}
	return -1
}

// parseNodeMemory reads memory information for a specific NUMA node.
//
// Data Source: /sys/devices/system/node/nodeX/meminfo (KERNEL-GUARANTEED)
//
// This method reads per-node memory information from the NUMA node's meminfo file,
// which contains memory statistics specific to that NUMA node as managed by the kernel.
//
// Format Specification:
// - File format: "Node X MemTotal: XXXXX kB" (always in kilobytes)
// - Kernel-guaranteed format across all Linux distributions
// - Contains various memory statistics, but we focus on MemTotal
// - Memory amount reflects what the kernel allocates to this NUMA node
//
// Parsing Strategy:
// - Search for "MemTotal:" line within the node's meminfo file
// - Extract the kilobyte value and convert to bytes
// - Gracefully handle missing or malformed files (leave TotalBytes as 0)
//
// Important Notes:
// - Per-node memory totals may not sum exactly to system total due to kernel reservations
// - Virtual machines may report synthetic NUMA nodes that don't reflect true hardware
// - Some memory may be reserved for hardware/firmware and not appear in any node
// - The kernel may dynamically rebalance memory between nodes
//
// References:
// - NUMA sysfs ABI: https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-devices-system-node
func (c *MemoryInfoCollector) parseNodeMemory(node *performance.NUMANode, nodePath string) {
	// KERNEL-GUARANTEED: Read per-node memory information
	nodeMeminfoPath := filepath.Join(nodePath, "meminfo")
	data, err := os.ReadFile(nodeMeminfoPath)
	if err != nil {
		return // Gracefully handle missing or inaccessible meminfo file
	}

	// Parse for MemTotal in node's meminfo
	// Format: "Node X MemTotal: XXXXX kB" (kernel-guaranteed format)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.Contains(line, "MemTotal:") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if field == "MemTotal:" && i+1 < len(fields) {
					if memKB, err := strconv.ParseUint(fields[i+1], 10, 64); err == nil {
						node.TotalBytes = memKB * 1024 // Convert KB to bytes
						break
					}
				}
			}
		}
	}
}

// parseNodeCPUs reads CPU affinity information for a specific NUMA node.
//
// Data Source: /sys/devices/system/node/nodeX/cpulist (KERNEL-GUARANTEED)
//
// This method reads the CPU affinity list for a NUMA node, which indicates which
// CPU cores are associated with this memory node for optimal memory access performance.
//
// Format Specification:
// - File format: "0-3,8-11" (comma-separated ranges, kernel-standardized)
// - Individual CPUs: "0,1,2,3" or ranges: "0-3" or mixed: "0-3,8-11"
// - Kernel-guaranteed format representing physical CPU to NUMA node mapping
// - Reflects actual hardware topology as detected by the kernel
//
// Parsing Algorithm:
// 1. Split comma-separated entries
// 2. For each entry, check if it's a range (contains "-") or single CPU
// 3. For ranges, enumerate all CPUs in the range
// 4. For single CPUs, add directly to the node's CPU list
//
// NUMA CPU Affinity Significance:
// - CPUs listed here have the fastest memory access to this node's memory
// - Cross-node memory access incurs performance penalties
// - Kernel scheduler uses this information for CPU and memory placement
// - Applications can use this for NUMA-aware optimization
//
// Important Notes:
// - CPU numbering follows the kernel's logical CPU numbering (0-based)
// - CPU lists may not be contiguous (e.g., "0-3,8-11" with gaps)
// - Virtual machines may present synthetic NUMA topologies
// - CPU hotplug operations can change these mappings dynamically
// - Some systems have asymmetric NUMA configurations
//
// References:
// - NUMA sysfs ABI: https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-devices-system-node
// - CPU topology: https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-devices-system-cpu
func (c *MemoryInfoCollector) parseNodeCPUs(node *performance.NUMANode, nodePath string) {
	// KERNEL-GUARANTEED: Read CPU affinity list for this NUMA node
	cpulistPath := filepath.Join(nodePath, "cpulist")
	data, err := os.ReadFile(cpulistPath)
	if err != nil {
		return // Gracefully handle missing or inaccessible cpulist file
	}

	// Parse CPU ranges from kernel-standardized format
	// Examples: "0-3,8-11", "0,1,2,3", "0-3", "5"
	cpuList := strings.TrimSpace(string(data))
	if cpuList == "" {
		return
	}

	ranges := strings.Split(cpuList, ",")
	for _, r := range ranges {
		r = strings.TrimSpace(r)
		if strings.Contains(r, "-") {
			// Range format: "0-3" means CPUs 0, 1, 2, 3
			parts := strings.Split(r, "-")
			if len(parts) == 2 {
				start, err1 := strconv.ParseInt(parts[0], 10, 32)
				end, err2 := strconv.ParseInt(parts[1], 10, 32)
				if err1 == nil && err2 == nil {
					for cpu := start; cpu <= end; cpu++ {
						node.CPUs = append(node.CPUs, int32(cpu))
					}
				}
			}
		} else {
			// Single CPU format: "5" means CPU 5
			if cpu, err := strconv.ParseInt(r, 10, 32); err == nil {
				node.CPUs = append(node.CPUs, int32(cpu))
			}
		}
	}
}

// getAllCPUs enumerates all available CPUs when NUMA information is unavailable.
//
// Data Source: /sys/devices/system/cpu/cpu[0-9]* (KERNEL-GUARANTEED)
//
// This method serves as a fallback when NUMA topology is not available or accessible,
// providing a complete list of all CPUs in the system for synthetic single-node configuration.
//
// Fallback Scenarios:
// - Systems without NUMA support (UMA - Uniform Memory Access)
// - Systems with NUMA disabled in firmware/kernel
// - Virtual machines without NUMA topology exposure
// - Systems where NUMA sysfs files are inaccessible
//
// Enumeration Process:
// 1. Scan /sys/devices/system/cpu/ for cpu[0-9]* directories
// 2. Extract CPU numbers from directory names
// 3. Return sorted list of all available CPU IDs
//
// CPU Directory Structure:
// - /sys/devices/system/cpu/cpu0, cpu1, cpu2, etc.
// - Each directory represents a logical CPU core
// - Directory names follow kernel-standardized naming (cpu + number)
// - Numbering is 0-based and may have gaps due to CPU hotplug
//
// Important Notes:
// - CPU numbering reflects logical CPU IDs, not physical core arrangement
// - CPU hotplug operations can create gaps in numbering
// - Virtual machines may present different CPU topologies than host
// - This method provides no NUMA affinity information
// - Result is used to create synthetic single-node NUMA configuration
//
// References:
// - CPU sysfs ABI: https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-devices-system-cpu
// - CPU topology: https://www.kernel.org/doc/html/latest/admin-guide/pm/cpufreq.html
func (c *MemoryInfoCollector) getAllCPUs() []int32 {
	// KERNEL-GUARANTEED: Fallback method to enumerate all CPUs
	// Used when NUMA topology is unavailable or inaccessible
	cpuPath := filepath.Join(c.nodeSystemPath, "..", "cpu")
	pattern := filepath.Join(cpuPath, "cpu[0-9]*")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return []int32{} // Gracefully handle systems with no CPU enumeration
	}

	cpus := make([]int32, 0, len(matches))
	for _, match := range matches {
		base := filepath.Base(match)
		if strings.HasPrefix(base, "cpu") {
			cpuStr := strings.TrimPrefix(base, "cpu")
			if cpu, err := strconv.ParseInt(cpuStr, 10, 32); err == nil {
				cpus = append(cpus, int32(cpu))
			}
		}
	}
	return cpus
}
