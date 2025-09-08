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
	performance.Register(performance.MetricTypeMemory, performance.PartialNewContinuousPointCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewMemoryCollector(logger, config)
		},
	))
}

// Compile-time interface check
var _ performance.PointCollector = (*MemoryCollector)(nil)

// MemoryCollector collects runtime memory statistics from /proc/meminfo
//
// Purpose: Runtime memory monitoring and performance analysis
// This collector provides real-time memory usage statistics for operational
// monitoring, alerting, and performance analysis. It captures the current
// state of system memory allocation and usage.
//
// This collector reads 30 memory statistics fields from /proc/meminfo, including:
// - Basic memory usage (MemTotal, MemFree, MemAvailable)
// - Buffer and cache memory
// - Swap memory statistics
// - Kernel memory usage (Slab, KernelStack, PageTables)
// - Huge pages statistics
// - Virtual memory statistics
//
// Key Differences from MemoryInfoCollector:
// - MemoryCollector: Provides runtime statistics (dynamic, changes constantly)
// - MemoryInfoCollector: Provides hardware configuration (static NUMA topology)
// - This collector is for monitoring; MemoryInfoCollector is for inventory
//
// All memory values are converted from kilobytes (as reported by the kernel)
// to bytes for consistency. HugePages counts are converted to bytes using
// the reported Hugepagesize.
//
// Use Cases:
// - Monitor memory pressure and usage patterns
// - Detect memory leaks
// - Alert on low memory conditions
// - Analyze application memory behavior
// - Track swap usage and page cache efficiency
//
// Reference: https://www.kernel.org/doc/html/latest/filesystems/proc.html#meminfo
type MemoryCollector struct {
	performance.BaseDeltaCollector
	meminfoPath string
	vmstatPath  string
}

func NewMemoryCollector(logger logr.Logger, config performance.CollectionConfig) (*MemoryCollector, error) {
	if err := config.Validate(performance.ValidateOptions{RequireHostProcPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   false,
		RequiredCapabilities: nil,     // No special capabilities required
		MinKernelVersion:     "2.6.0", // /proc/meminfo has been around forever
	}

	return &MemoryCollector{
		BaseDeltaCollector: performance.NewBaseDeltaCollector(
			performance.MetricTypeMemory,
			"System Memory Collector",
			logger,
			config,
			capabilities,
		),
		meminfoPath: filepath.Join(config.HostProcPath, "meminfo"),
		vmstatPath:  filepath.Join(config.HostProcPath, "vmstat"),
	}, nil
}

// Collect performs a one-shot collection of memory statistics
func (c *MemoryCollector) Collect(ctx context.Context, receiver performance.Receiver) error {
	currentTime := time.Now()

	// Collect current statistics
	currentStats, err := c.collectMemoryStats()
	if err != nil {
		return fmt.Errorf("failed to collect memory stats: %w", err)
	}

	shouldCalc, reason := c.ShouldCalculateDeltas(currentTime)
	if !shouldCalc {
		c.Logger().V(2).Info("Skipping delta calculation", "reason", reason)
		if c.IsFirst {
			c.UpdateDeltaState(currentStats, currentTime)
		}
		c.Logger().V(1).Info("Collected memory statistics")
		return receiver.Accept(currentStats)
	}

	previousStats, ok := c.LastSnapshot.(*performance.MemoryStats)
	if !ok || previousStats == nil {
		c.UpdateDeltaState(currentStats, currentTime)
		c.Logger().V(1).Info("Collected memory statistics")
		return receiver.Accept(currentStats)
	}

	c.calculateMemoryDeltas(currentStats, previousStats, currentTime, c.Config)
	c.UpdateDeltaState(currentStats, currentTime)

	c.Logger().V(1).Info("Collected memory statistics with delta support")
	return receiver.Accept(currentStats)
}

// collectMemoryStats reads and parses runtime memory statistics from /proc/meminfo and /proc/vmstat
//
// This method collects current memory usage and state information for performance
// monitoring. Unlike MemoryInfoCollector which only reads MemTotal for hardware
// inventory, this reads all available memory statistics for operational monitoring.
//
// /proc/meminfo format:
//
//	FieldName:       value kB
//
// /proc/vmstat format:
//
//	field_name value
//
// Most meminfo fields are in kilobytes, except HugePages_* which are page counts.
// Vmstat fields like pswpin/pswpout are in pages.
// The collector converts all values to consistent units.
//
// Error handling:
// - /proc/meminfo read errors return an error (critical failure)
// - /proc/vmstat read errors are logged but don't fail collection (optional data)
// - Individual field parsing errors are logged but don't fail collection
// - Missing fields are left as zero (graceful degradation)
func (c *MemoryCollector) collectMemoryStats() (*performance.MemoryStats, error) {
	file, err := os.Open(c.meminfoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", c.meminfoPath, err)
	}
	defer file.Close()

	stats := &performance.MemoryStats{}
	scanner := bufio.NewScanner(file)

	// Map field names from /proc/meminfo to struct fields
	fieldMap := map[string]*uint64{
		"MemTotal":     &stats.MemTotal,
		"MemFree":      &stats.MemFree,
		"MemAvailable": &stats.MemAvailable,
		"Buffers":      &stats.Buffers,
		"Cached":       &stats.Cached,
		"SwapCached":   &stats.SwapCached,
		"Active":       &stats.Active,
		"Inactive":     &stats.Inactive,
		"SwapTotal":    &stats.SwapTotal,
		"SwapFree":     &stats.SwapFree,
		"Dirty":        &stats.Dirty,
		"Writeback":    &stats.Writeback,
		"AnonPages":    &stats.AnonPages,
		"Mapped":       &stats.Mapped,
		"Shmem":        &stats.Shmem,
		"Slab":         &stats.Slab,
		"SReclaimable": &stats.SReclaimable,
		"SUnreclaim":   &stats.SUnreclaim,
		"KernelStack":  &stats.KernelStack,
		"PageTables":   &stats.PageTables,
		"CommitLimit":  &stats.CommitLimit,
		"Committed_AS": &stats.CommittedAS,
		"VmallocTotal": &stats.VmallocTotal,
		"VmallocUsed":  &stats.VmallocUsed,
		// https://www.kernel.org/doc/html/latest/admin-guide/mm/hugetlbpage.html
		"HugePages_Total": &stats.HugePages_Total,
		"HugePages_Free":  &stats.HugePages_Free,
		"HugePages_Rsvd":  &stats.HugePages_Rsvd,
		"HugePages_Surp":  &stats.HugePages_Surp,
		"Hugepagesize":    &stats.HugePagesize,
		"Hugetlb":         &stats.Hugetlb,
	}

	// Track huge page counts and size for proper conversion
	hugePagesCountFields := map[string]bool{
		"HugePages_Total": true,
		"HugePages_Free":  true,
		"HugePages_Rsvd":  true,
		"HugePages_Surp":  true,
	}

	for scanner.Scan() {
		line := scanner.Text()
		// Lines are formatted as "FieldName:   value kB"
		// Some fields might have additional info after the unit
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		// Remove trailing colon from field name
		fieldName := strings.TrimSuffix(parts[0], ":")

		// Check if this is a field we're interested in
		fieldPtr, ok := fieldMap[fieldName]
		if !ok {
			continue
		}

		// Parse the value (second field is always the numeric value)
		value, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			// Log at debug level - some fields might have different formats on certain systems
			c.Logger().V(2).Info("Failed to parse memory field value",
				"field", fieldName, "value", parts[1], "error", err)
			continue
		}

		// Store raw value first
		*fieldPtr = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading %s: %w", c.meminfoPath, err)
	}

	// Collect swap activity from /proc/vmstat (optional)
	c.collectSwapActivity(stats)

	// Post-process: convert values to bytes
	c.convertToBytes(stats, hugePagesCountFields)

	return stats, nil
}

// convertToBytes converts memory values from their native units to bytes
//
// Most /proc/meminfo fields are in kilobytes and need to be multiplied by 1024.
// HugePages fields are special:
// - HugePages_Total, HugePages_Free, HugePages_Rsvd, HugePages_Surp are page counts
// - These counts are multiplied by Hugepagesize to get total bytes
// - Hugepagesize itself is in kB and converted to bytes
// - Hugetlb is already a memory amount in kB, not a page count
func (c *MemoryCollector) convertToBytes(stats *performance.MemoryStats, hugePagesCountFields map[string]bool) {
	// Convert regular kB fields to bytes
	stats.MemTotal *= 1024
	stats.MemFree *= 1024
	stats.MemAvailable *= 1024
	stats.Buffers *= 1024
	stats.Cached *= 1024
	stats.SwapCached *= 1024
	stats.Active *= 1024
	stats.Inactive *= 1024
	stats.SwapTotal *= 1024
	stats.SwapFree *= 1024
	stats.Dirty *= 1024
	stats.Writeback *= 1024
	stats.AnonPages *= 1024
	stats.Mapped *= 1024
	stats.Shmem *= 1024
	stats.Slab *= 1024
	stats.SReclaimable *= 1024
	stats.SUnreclaim *= 1024
	stats.KernelStack *= 1024
	stats.PageTables *= 1024
	stats.CommitLimit *= 1024
	stats.CommittedAS *= 1024
	stats.VmallocTotal *= 1024
	stats.VmallocUsed *= 1024

	// Convert Hugepagesize from kB to bytes
	stats.HugePagesize *= 1024

	// Convert Hugetlb from kB to bytes (this is already a total memory amount)
	stats.Hugetlb *= 1024

	// Convert huge page counts to bytes using the huge page size
	// HugePages_Total, HugePages_Free, HugePages_Rsvd, HugePages_Surp are counts, not sizes
	// They should be multiplied by the huge page size to get total memory
	//
	// Note: /proc/meminfo shows the default huge page size and counts.
	// To see all supported huge page sizes, check: /sys/kernel/mm/hugepages/
	// Each subdirectory (e.g., hugepages-2048kB) contains size-specific counts.
	if stats.HugePagesize > 0 {
		stats.HugePages_Total *= stats.HugePagesize
		stats.HugePages_Free *= stats.HugePagesize
		stats.HugePages_Rsvd *= stats.HugePagesize
		stats.HugePages_Surp *= stats.HugePagesize
	}
}

// collectSwapActivity reads swap activity counters from /proc/vmstat
//
// This method reads cumulative counters for swap in/out activity that are used
// to calculate vmstat-compatible swap rates. Fields are in pages and left as raw
// counts for higher-level rate computation.
//
// /proc/vmstat format:
//
//	pswpin value
//	pswpout value
//
// Performance Investigation Context:
//
// Swap In (si in vmstat):
// - Pages swapped from disk back into RAM since boot
// - Rate indicates memory pressure requiring disk access for reclaimed pages
// - High swap-in rates (>1000 pages/sec) indicate:
//   - Insufficient RAM for current workload
//   - Memory-intensive applications competing for physical memory
//   - System accessing previously swapped-out data
//
// - Causes severe performance degradation (disk is ~1000x slower than RAM)
//
// Swap Out (so in vmstat):
// - Pages moved from RAM to swap space on disk since boot
// - Rate indicates kernel reclaiming memory under pressure
// - High swap-out rates (>1000 pages/sec) indicate:
//   - Memory pressure forcing kernel to free RAM
//   - Applications allocating more memory than physically available
//   - Kernel choosing to swap out inactive pages
//
// - Often precedes swap-in activity as applications later access swapped data
//
// Diagnostic Patterns:
// - si=0, so=0: No swap activity (healthy system with adequate RAM)
// - si=0, so>0: Memory pressure causing swapping but no page faults yet
// - si>0, so>0: Active swapping (severe memory pressure, major performance impact)
// - si>0, so=0: Accessing previously swapped data (recovery from memory pressure)
//
// Investigation Actions:
// - Any sustained swap activity indicates need for more RAM or workload optimization
// - Identify memory-intensive processes with high RSS/PSS values
// - Consider increasing swap space as temporary mitigation
// - Monitor with memory stats to correlate with available/free memory levels
//
// Note: Values are in pages (typically 4KB), multiply by page size for bytes.
//
// Error handling strategy:
// - /proc/vmstat is optional - logs warnings but continues if unavailable
// - Parse errors for specific fields are logged but don't fail collection
// - Missing fields are left as zero (graceful degradation)
func (c *MemoryCollector) collectSwapActivity(stats *performance.MemoryStats) {
	file, err := os.Open(c.vmstatPath)
	if err != nil {
		c.Logger().V(2).Info("Optional file not available", "path", c.vmstatPath, "error", err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}

		fieldName := parts[0]
		var fieldPtr *uint64

		switch fieldName {
		case "pswpin":
			fieldPtr = &stats.SwapIn
		case "pswpout":
			fieldPtr = &stats.SwapOut
		default:
			continue
		}

		value, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			c.Logger().V(2).Info("Failed to parse vmstat field",
				"field", fieldName, "value", parts[1], "error", err)
			continue
		}

		*fieldPtr = value
	}

	if err := scanner.Err(); err != nil {
		c.Logger().V(2).Info("Error reading vmstat file", "path", c.vmstatPath, "error", err)
	}
}

func (c *MemoryCollector) calculateMemoryDeltas(
	current, previous *performance.MemoryStats,
	currentTime time.Time,
	config performance.DeltaConfig,
) {
	interval := currentTime.Sub(c.LastTime)
	var resetDetected bool

	// Create nested delta data structure
	delta := &performance.MemoryDeltaData{}

	calculateField := func(currentVal, previousVal uint64) uint64 {
		deltaVal, reset := c.CalculateUint64Delta(currentVal, previousVal, interval)
		resetDetected = resetDetected || reset
		return deltaVal
	}

	// Calculate delta values
	delta.SwapIn = calculateField(current.SwapIn, previous.SwapIn)
	delta.SwapOut = calculateField(current.SwapOut, previous.SwapOut)

	// Calculate rates if no reset detected
	if !resetDetected {
		intervalSecs := interval.Seconds()
		if intervalSecs > 0 {
			delta.SwapInPerSec = uint64(float64(delta.SwapIn) / intervalSecs)
			delta.SwapOutPerSec = uint64(float64(delta.SwapOut) / intervalSecs)
		}
	}

	// Use composition helper to set metadata
	c.PopulateMetadata(delta, currentTime, resetDetected)
	current.Delta = delta

	if resetDetected {
		c.Logger().V(1).Info("Counter reset detected in memory statistics")
	}
}
