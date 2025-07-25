// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tools

import (
	"context"
	"fmt"

	"github.com/antimetal/agent/pkg/mcp"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// VmstatTool implements the `vmstat` command equivalent
// Shows virtual memory statistics, system activity - second command in Brendan Gregg's 60s checklist
type VmstatTool struct {
	logger        logr.Logger
	config        performance.CollectionConfig
	memoryFactory performance.NewContinuousCollector
	cpuFactory    performance.NewContinuousCollector
}

// NewVmstatTool creates a new vmstat tool
func NewVmstatTool(logger logr.Logger, config performance.CollectionConfig) (*VmstatTool, error) {
	memoryFactory, err := performance.GetCollector(performance.MetricTypeMemory)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory collector: %w", err)
	}

	cpuFactory, err := performance.GetCollector(performance.MetricTypeCPU)
	if err != nil {
		return nil, fmt.Errorf("failed to get CPU collector: %w", err)
	}

	return &VmstatTool{
		logger:        logger.WithName("vmstat"),
		config:        config,
		memoryFactory: memoryFactory,
		cpuFactory:    cpuFactory,
	}, nil
}

// Name returns the tool name
func (v *VmstatTool) Name() string {
	return "vmstat"
}

// Description returns the tool description
func (v *VmstatTool) Description() string {
	return "Show virtual memory statistics (equivalent to `vmstat` command)"
}

// InputSchema returns the tool's input schema
func (v *VmstatTool) InputSchema() mcp.ToolSchema {
	return mcp.ToolSchema{
		Type: "object",
		Properties: map[string]mcp.PropertySchema{
			"format": {
				Type:        "string",
				Description: "Output format (json, text, table)",
				Default:     "text",
				Enum:        []string{"json", "text", "table"},
			},
			"interval": {
				Type:        "integer",
				Description: "Update interval in seconds (default: 1)",
				Default:     1,
			},
			"count": {
				Type:        "integer",
				Description: "Number of updates to show (default: 1)",
				Default:     1,
			},
		},
	}
}

// VmstatData combines memory and CPU stats for vmstat output
type VmstatData struct {
	Memory *performance.MemoryStats `json:"memory"`
	CPU    []performance.CPUStats   `json:"cpu"`
}

// Execute performs the tool execution
func (v *VmstatTool) Execute(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResponse, error) {
	format := "text"
	if f, ok := args["format"].(string); ok {
		format = f
	}

	// Note: vmstat in this implementation does single collection
	// count parameter is parsed but not used as we collect once

	// Collect memory and CPU stats
	memoryCollector, err := v.memoryFactory(v.logger, v.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create memory collector: %w", err)
	}

	cpuCollector, err := v.cpuFactory(v.logger, v.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create CPU collector: %w", err)
	}

	// Start collectors
	memoryCh, err := memoryCollector.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start memory collector: %w", err)
	}
	defer memoryCollector.Stop()

	cpuCh, err := cpuCollector.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start CPU collector: %w", err)
	}
	defer cpuCollector.Stop()

	// Collect the first sample
	var memoryStats *performance.MemoryStats
	var cpuStats []performance.CPUStats

	select {
	case data := <-memoryCh:
		if ms, ok := data.(*performance.MemoryStats); ok {
			memoryStats = ms
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case data := <-cpuCh:
		if cs, ok := data.([]performance.CPUStats); ok {
			cpuStats = cs
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	vmstatData := &VmstatData{
		Memory: memoryStats,
		CPU:    cpuStats,
	}

	// Format output
	switch format {
	case "json":
		return v.formatAsJSON(vmstatData)
	case "text":
		return v.formatAsText(vmstatData)
	case "table":
		return v.formatAsTable(vmstatData)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// formatAsJSON formats data as JSON
func (v *VmstatTool) formatAsJSON(data *VmstatData) (*mcp.CallToolResponse, error) {
	result := map[string]interface{}{
		"type":      "vmstat",
		"timestamp": getCurrentTimestamp(),
		"data":      data,
	}

	return &mcp.CallToolResponse{
		Content: []mcp.ToolResult{{
			Type: "text",
			Data: result,
		}},
	}, nil
}

// formatAsText formats data as vmstat command output
func (v *VmstatTool) formatAsText(data *VmstatData) (*mcp.CallToolResponse, error) {
	if data.Memory == nil || len(data.CPU) == 0 {
		return nil, fmt.Errorf("insufficient data for vmstat output")
	}

	// Calculate aggregate CPU stats
	var totalUser, totalSystem, totalIdle, totalIOWait uint64
	for _, cpu := range data.CPU {
		if cpu.CPUIndex == -1 { // aggregate CPU line
			totalUser = cpu.User
			totalSystem = cpu.System
			totalIdle = cpu.Idle
			totalIOWait = cpu.IOWait
			break
		}
	}

	// Convert memory from kB to pages (assuming 4KB pages)
	const pageSize = 4

	// Format like vmstat output:
	// procs -----------memory---------- ---swap-- -----io---- -system-- ------cpu-----
	//  r  b   swpd   free   buff  cache   si   so    bi    bo   in   cs us sy id wa st
	text := fmt.Sprintf(`procs -----------memory---------- ---swap-- -----io---- -system-- ------cpu-----
 r  b   swpd   free   buff  cache   si   so    bi    bo   in   cs us sy id wa st
%2d %2d %6d %6d %6d %6d    0    0     0     0    0    0 %2d %2d %2d %2d  0`,
		0, // running processes (approximation - we don't have exact count)
		0, // blocked processes (we don't have this exact stat)
		(data.Memory.SwapTotal-data.Memory.SwapFree)/pageSize, // swap used in pages
		data.Memory.MemFree/pageSize,                          // free memory in pages
		data.Memory.Buffers/pageSize,                          // buffers in pages
		data.Memory.Cached/pageSize,                           // cache in pages
		// IO stats not available from current collectors (si, so, bi, bo, in, cs)
		calculateCPUPercent(totalUser, totalUser+totalSystem+totalIdle+totalIOWait),   // user %
		calculateCPUPercent(totalSystem, totalUser+totalSystem+totalIdle+totalIOWait), // system %
		calculateCPUPercent(totalIdle, totalUser+totalSystem+totalIdle+totalIOWait),   // idle %
		calculateCPUPercent(totalIOWait, totalUser+totalSystem+totalIdle+totalIOWait), // wait %
	)

	return &mcp.CallToolResponse{
		Content: []mcp.ToolResult{{
			Type: "text",
			Text: text,
		}},
	}, nil
}

// formatAsTable formats data as a table
func (v *VmstatTool) formatAsTable(data *VmstatData) (*mcp.CallToolResponse, error) {
	if data.Memory == nil {
		return nil, fmt.Errorf("no memory data available")
	}

	text := fmt.Sprintf(`Virtual Memory Statistics:
┌─────────────────┬──────────────┐
│ Memory Metric   │ Value        │
├─────────────────┼──────────────┤
│ Total Memory    │ %s           │
│ Free Memory     │ %s           │
│ Available Mem   │ %s           │
│ Buffers         │ %s           │
│ Cached          │ %s           │
│ Swap Total      │ %s           │
│ Swap Free       │ %s           │
│ Dirty Pages     │ %s           │
│ Active Memory   │ %s           │
│ Inactive Memory │ %s           │
└─────────────────┴──────────────┘`,
		formatBytes(data.Memory.MemTotal*1024),
		formatBytes(data.Memory.MemFree*1024),
		formatBytes(data.Memory.MemAvailable*1024),
		formatBytes(data.Memory.Buffers*1024),
		formatBytes(data.Memory.Cached*1024),
		formatBytes(data.Memory.SwapTotal*1024),
		formatBytes(data.Memory.SwapFree*1024),
		formatBytes(data.Memory.Dirty*1024),
		formatBytes(data.Memory.Active*1024),
		formatBytes(data.Memory.Inactive*1024),
	)

	return &mcp.CallToolResponse{
		Content: []mcp.ToolResult{{
			Type: "text",
			Text: text,
		}},
	}, nil
}

// calculateCPUPercent calculates CPU percentage
func calculateCPUPercent(value, total uint64) int {
	if total == 0 {
		return 0
	}
	return int((value * 100) / total)
}
