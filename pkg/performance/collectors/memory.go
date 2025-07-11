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

// MemoryCollector collects system memory statistics from /proc/meminfo
type MemoryCollector struct {
	performance.BaseCollector
	meminfoPath string
}

func NewMemoryCollector(logger logr.Logger, config performance.CollectionConfig) *MemoryCollector {
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
	}
}

func (c *MemoryCollector) Collect(ctx context.Context) (any, error) {
	return c.collectMemoryStats()
}

// collectMemoryStats reads and parses /proc/meminfo
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
		"MemTotal":        &stats.MemTotal,
		"MemFree":         &stats.MemFree,
		"MemAvailable":    &stats.MemAvailable,
		"Buffers":         &stats.Buffers,
		"Cached":          &stats.Cached,
		"SwapCached":      &stats.SwapCached,
		"Active":          &stats.Active,
		"Inactive":        &stats.Inactive,
		"SwapTotal":       &stats.SwapTotal,
		"SwapFree":        &stats.SwapFree,
		"Dirty":           &stats.Dirty,
		"Writeback":       &stats.Writeback,
		"AnonPages":       &stats.AnonPages,
		"Mapped":          &stats.Mapped,
		"Shmem":           &stats.Shmem,
		"Slab":            &stats.Slab,
		"SReclaimable":    &stats.SReclaimable,
		"SUnreclaim":      &stats.SUnreclaim,
		"KernelStack":     &stats.KernelStack,
		"PageTables":      &stats.PageTables,
		"CommitLimit":     &stats.CommitLimit,
		"Committed_AS":    &stats.CommittedAS,
		"VmallocTotal":    &stats.VmallocTotal,
		"VmallocUsed":     &stats.VmallocUsed,
		"HugePages_Total": &stats.HugePages_Total,
		"HugePages_Free":  &stats.HugePages_Free,
		"Hugepagesize":    &stats.HugePagesize,
	}

	for scanner.Scan() {
		line := scanner.Text()
		// Lines are formatted as "FieldName:   value kB"
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

		// Parse the value
		value, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			// Log at debug level - some fields might have different formats on certain systems
			c.Logger().V(2).Info("Failed to parse memory field value", 
				"field", fieldName, "value", parts[1], "error", err)
			continue
		}

		// Convert from kB to bytes (multiply by 1024)
		// Note: /proc/meminfo values are in kB by default
		*fieldPtr = value * 1024
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading %s: %w", c.meminfoPath, err)
	}

	return stats, nil
}
