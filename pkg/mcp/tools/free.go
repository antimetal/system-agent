// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tools

import (
	"fmt"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// FreeTool implements the `free` command equivalent
// Shows memory usage - sixth command in Brendan Gregg's 60s checklist
type FreeTool struct {
	*BaseCollectorTool
}

// NewFreeTool creates a new free tool
func NewFreeTool(logger logr.Logger, config performance.CollectionConfig) (*FreeTool, error) {
	base, err := NewBaseCollectorTool(
		"free",
		"Show memory usage statistics (equivalent to `free` command)",
		performance.MetricTypeMemory,
		logger,
		config,
	)
	if err != nil {
		return nil, err
	}

	return &FreeTool{
		BaseCollectorTool: base,
	}, nil
}

// formatAsText formats memory stats as free command output
func (f *FreeTool) formatAsText(data interface{}) (string, error) {
	memStats, ok := data.(*performance.MemoryStats)
	if !ok {
		return "", fmt.Errorf("expected *MemoryStats, got %T", data)
	}

	// Calculate used memory
	used := memStats.MemTotal - memStats.MemFree

	// Calculate buffer/cache
	bufferCache := memStats.Buffers + memStats.Cached

	// Calculate available memory (should use MemAvailable if available)
	available := memStats.MemAvailable
	if available == 0 {
		// Fallback calculation if MemAvailable not available
		available = memStats.MemFree + bufferCache
	}

	// Format like free command (values in kB)
	output := fmt.Sprintf(`               total        used        free      shared  buff/cache   available
Mem:        %8d    %8d    %8d    %8d    %8d    %8d
Swap:       %8d    %8d    %8d`,
		memStats.MemTotal,
		used,
		memStats.MemFree,
		memStats.Shmem,
		bufferCache,
		available,
		memStats.SwapTotal,
		memStats.SwapTotal-memStats.SwapFree,
		memStats.SwapFree,
	)

	return output, nil
}

// formatAsTable formats memory stats as a table
func (f *FreeTool) formatAsTable(data interface{}) (string, error) {
	memStats, ok := data.(*performance.MemoryStats)
	if !ok {
		return "", fmt.Errorf("expected *MemoryStats, got %T", data)
	}

	used := memStats.MemTotal - memStats.MemFree
	bufferCache := memStats.Buffers + memStats.Cached
	swapUsed := memStats.SwapTotal - memStats.SwapFree

	// Calculate percentages
	memUsedPct := float64(used) * 100.0 / float64(memStats.MemTotal)
	swapUsedPct := float64(0)
	if memStats.SwapTotal > 0 {
		swapUsedPct = float64(swapUsed) * 100.0 / float64(memStats.SwapTotal)
	}

	output := fmt.Sprintf(`Memory Usage Statistics:
┌─────────────────┬──────────────┬──────────────┬─────────┐
│ Type            │ Total        │ Used         │ Used %%  │
├─────────────────┼──────────────┼──────────────┼─────────┤
│ Memory          │ %s      │ %s      │ %6.1f%% │
│ Swap            │ %s      │ %s      │ %6.1f%% │
│ Buffers/Cache   │ %s      │ %s      │    -    │
│ Available       │ %s      │ %s      │    -    │
└─────────────────┴──────────────┴──────────────┴─────────┘`,
		formatBytes(memStats.MemTotal*1024),
		formatBytes(used*1024),
		memUsedPct,
		formatBytes(memStats.SwapTotal*1024),
		formatBytes(swapUsed*1024),
		swapUsedPct,
		formatBytes(bufferCache*1024),
		formatBytes(bufferCache*1024),
		formatBytes(memStats.MemAvailable*1024),
		formatBytes(memStats.MemAvailable*1024),
	)

	return output, nil
}
