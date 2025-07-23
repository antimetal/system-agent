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
	performance.BaseCollector
	meminfoPath string
}

func NewMemoryCollector(logger logr.Logger, config performance.CollectionConfig) (*MemoryCollector, error) {
	if err := config.Validate(performance.ValidateOption{RequireHostProcPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    true,
		SupportsContinuous: false,
		RequiresRoot:       false,
		RequiresEBPF:       false,
		MinKernelVersion:   "2.6.0", // /proc/meminfo has been around forever
	}

	return &MemoryCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeMemory,
			"System Memory Collector",
			logger,
			config,
			capabilities,
		),
		meminfoPath: filepath.Join(config.HostProcPath, "meminfo"),
	}, nil
}

// Collect performs a one-shot collection of memory statistics
func (c *MemoryCollector) Collect(ctx context.Context) (any, error) {
	stats, err := c.collectMemoryStats()
	if err != nil {
		return nil, fmt.Errorf("failed to collect memory stats: %w", err)
	}

	c.Logger().V(1).Info("Collected memory statistics")
	return stats, nil
}

// collectMemoryStats reads and parses runtime memory statistics from /proc/meminfo
//
// This method collects current memory usage and state information for performance
// monitoring. Unlike MemoryInfoCollector which only reads MemTotal for hardware
// inventory, this reads all available memory statistics for operational monitoring.
//
// /proc/meminfo format:
//
//	FieldName:       value kB
//
// Most fields are in kilobytes, except HugePages_* which are page counts.
// The collector converts all values to bytes for consistency.
//
// Error handling:
// - File read errors return an error (critical failure)
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
