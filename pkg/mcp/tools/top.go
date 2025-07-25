// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/antimetal/agent/pkg/mcp"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// TopTool implements the `top` command equivalent
// Shows top processes by CPU usage - eighth command in Brendan Gregg's 60s checklist
type TopTool struct {
	logger         logr.Logger
	config         performance.CollectionConfig
	processFactory performance.NewContinuousCollector
	loadFactory    performance.NewContinuousCollector
	memoryFactory  performance.NewContinuousCollector
}

// NewTopTool creates a new top tool
func NewTopTool(logger logr.Logger, config performance.CollectionConfig) (*TopTool, error) {
	processFactory, err := performance.GetCollector(performance.MetricTypeProcess)
	if err != nil {
		return nil, fmt.Errorf("failed to get process collector: %w", err)
	}

	loadFactory, err := performance.GetCollector(performance.MetricTypeLoad)
	if err != nil {
		return nil, fmt.Errorf("failed to get load collector: %w", err)
	}

	memoryFactory, err := performance.GetCollector(performance.MetricTypeMemory)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory collector: %w", err)
	}

	return &TopTool{
		logger:         logger.WithName("top"),
		config:         config,
		processFactory: processFactory,
		loadFactory:    loadFactory,
		memoryFactory:  memoryFactory,
	}, nil
}

// Name returns the tool name
func (t *TopTool) Name() string {
	return "top"
}

// Description returns the tool description
func (t *TopTool) Description() string {
	return "Show top processes by CPU usage (equivalent to `top` command)"
}

// InputSchema returns the tool's input schema
func (t *TopTool) InputSchema() mcp.ToolSchema {
	return mcp.ToolSchema{
		Type: "object",
		Properties: map[string]mcp.PropertySchema{
			"format": {
				Type:        "string",
				Description: "Output format (json, text, table)",
				Default:     "text",
				Enum:        []string{"json", "text", "table"},
			},
			"count": {
				Type:        "integer",
				Description: "Number of top processes to show (default: 10)",
				Default:     10,
			},
			"sort": {
				Type:        "string",
				Description: "Sort by field (cpu, memory, pid)",
				Default:     "cpu",
				Enum:        []string{"cpu", "memory", "pid"},
			},
		},
	}
}

// TopData combines system info and process data
type TopData struct {
	Load      *performance.LoadStats     `json:"load"`
	Memory    *performance.MemoryStats   `json:"memory"`
	Processes []performance.ProcessStats `json:"processes"`
}

// Execute performs the tool execution
func (t *TopTool) Execute(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResponse, error) {
	format := "text"
	if f, ok := args["format"].(string); ok {
		format = f
	}

	count := 10
	if c, ok := args["count"].(float64); ok {
		count = int(c)
	}

	sortBy := "cpu"
	if s, ok := args["sort"].(string); ok {
		sortBy = s
	}

	// Collect process data
	processCollector, err := t.processFactory(t.logger, t.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create process collector: %w", err)
	}

	processCh, err := processCollector.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start process collector: %w", err)
	}
	defer processCollector.Stop()

	// Collect load data
	loadCollector, err := t.loadFactory(t.logger, t.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create load collector: %w", err)
	}

	loadCh, err := loadCollector.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start load collector: %w", err)
	}
	defer loadCollector.Stop()

	// Collect memory data
	memoryCollector, err := t.memoryFactory(t.logger, t.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create memory collector: %w", err)
	}

	memoryCh, err := memoryCollector.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start memory collector: %w", err)
	}
	defer memoryCollector.Stop()

	// Collect data
	var processes []performance.ProcessStats
	var loadStats *performance.LoadStats
	var memoryStats *performance.MemoryStats

	select {
	case data := <-processCh:
		if ps, ok := data.([]performance.ProcessStats); ok {
			processes = ps
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case data := <-loadCh:
		if ls, ok := data.(*performance.LoadStats); ok {
			loadStats = ls
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case data := <-memoryCh:
		if ms, ok := data.(*performance.MemoryStats); ok {
			memoryStats = ms
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Sort processes
	switch sortBy {
	case "cpu":
		sort.Slice(processes, func(i, j int) bool {
			return processes[i].CPUPercent > processes[j].CPUPercent
		})
	case "memory":
		sort.Slice(processes, func(i, j int) bool {
			return processes[i].MemoryRSS > processes[j].MemoryRSS
		})
	case "pid":
		sort.Slice(processes, func(i, j int) bool {
			return processes[i].PID < processes[j].PID
		})
	}

	// Limit to top N processes
	if count > 0 && count < len(processes) {
		processes = processes[:count]
	}

	topData := &TopData{
		Load:      loadStats,
		Memory:    memoryStats,
		Processes: processes,
	}

	// Format output
	switch format {
	case "json":
		return t.formatAsJSON(topData)
	case "text":
		return t.formatAsText(topData)
	case "table":
		return t.formatAsTable(topData)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// formatAsJSON formats data as JSON
func (t *TopTool) formatAsJSON(data *TopData) (*mcp.CallToolResponse, error) {
	result := map[string]interface{}{
		"type":      "top",
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

// formatAsText formats data as top command output
func (t *TopTool) formatAsText(data *TopData) (*mcp.CallToolResponse, error) {
	var output strings.Builder

	// Top header with system info
	if data.Load != nil {
		output.WriteString(fmt.Sprintf("Tasks: %d total, %d running\n",
			data.Load.TotalProcs, data.Load.RunningProcs))
		output.WriteString(fmt.Sprintf("Load average: %.2f, %.2f, %.2f\n",
			data.Load.Load1Min, data.Load.Load5Min, data.Load.Load15Min))
	}

	if data.Memory != nil {
		used := data.Memory.MemTotal - data.Memory.MemFree
		output.WriteString(fmt.Sprintf("Memory: %s total, %s used, %s free, %s buff/cache\n",
			formatBytes(data.Memory.MemTotal*1024),
			formatBytes(used*1024),
			formatBytes(data.Memory.MemFree*1024),
			formatBytes((data.Memory.Buffers+data.Memory.Cached)*1024)))

		swapUsed := data.Memory.SwapTotal - data.Memory.SwapFree
		output.WriteString(fmt.Sprintf("Swap:   %s total, %s used, %s free\n\n",
			formatBytes(data.Memory.SwapTotal*1024),
			formatBytes(swapUsed*1024),
			formatBytes(data.Memory.SwapFree*1024)))
	}

	// Process header
	output.WriteString("  PID USER      PR  NI    VIRT    RES    SHR S  %CPU %MEM     TIME+ COMMAND\n")

	// Process list
	for _, proc := range data.Processes {
		// Calculate memory percentage
		memPercent := float64(0)
		if data.Memory != nil && data.Memory.MemTotal > 0 {
			memPercent = float64(proc.MemoryRSS*4) * 100.0 / float64(data.Memory.MemTotal) // RSS is in pages, convert to kB
		}

		output.WriteString(fmt.Sprintf("%5d %-8s %2d %3d %7s %6s %6s %s %5.1f %4.1f %8s %s\n",
			proc.PID,
			"root", // We don't have user info in ProcessStats
			proc.Priority,
			proc.Nice,
			formatBytes(proc.MemoryVSZ),        // VIRT
			formatBytes(proc.MemoryRSS*4*1024), // RES (convert pages to bytes)
			"0",                                // SHR (we don't have this)
			proc.State,                         // S
			proc.CPUPercent,                    // %CPU
			memPercent,                         // %MEM
			"0:00.00",                          // TIME+ (we don't track this)
			proc.Command,                       // COMMAND
		))
	}

	return &mcp.CallToolResponse{
		Content: []mcp.ToolResult{{
			Type: "text",
			Text: output.String(),
		}},
	}, nil
}

// formatAsTable formats data as a table
func (t *TopTool) formatAsTable(data *TopData) (*mcp.CallToolResponse, error) {
	var output strings.Builder

	// System summary
	if data.Load != nil && data.Memory != nil {
		used := data.Memory.MemTotal - data.Memory.MemFree
		output.WriteString(fmt.Sprintf("System Summary: %d tasks, %d running, Load: %.2f/%.2f/%.2f, Mem: %s/%s\n\n",
			data.Load.TotalProcs, data.Load.RunningProcs,
			data.Load.Load1Min, data.Load.Load5Min, data.Load.Load15Min,
			formatBytes(used*1024), formatBytes(data.Memory.MemTotal*1024)))
	}

	// Process table
	output.WriteString("Top Processes:\n")
	output.WriteString("┌───────┬──────────────────┬────────┬────────┬────────────┬──────────┬─────────┐\n")
	output.WriteString("│  PID  │     Command      │  CPU%  │  MEM%  │    VSZ     │   RSS    │  State  │\n")
	output.WriteString("├───────┼──────────────────┼────────┼────────┼────────────┼──────────┼─────────┤\n")

	for _, proc := range data.Processes {
		memPercent := float64(0)
		if data.Memory != nil && data.Memory.MemTotal > 0 {
			memPercent = float64(proc.MemoryRSS*4) * 100.0 / float64(data.Memory.MemTotal)
		}

		// Truncate command if too long
		command := proc.Command
		if len(command) > 16 {
			command = command[:13] + "..."
		}

		output.WriteString(fmt.Sprintf("│ %5d │ %-16s │ %5.1f%% │ %5.1f%% │ %10s │ %8s │   %-3s   │\n",
			proc.PID,
			command,
			proc.CPUPercent,
			memPercent,
			formatBytes(proc.MemoryVSZ),
			formatBytes(proc.MemoryRSS*4*1024),
			proc.State,
		))
	}

	output.WriteString("└───────┴──────────────────┴────────┴────────┴────────────┴──────────┴─────────┘\n")

	return &mcp.CallToolResponse{
		Content: []mcp.ToolResult{{
			Type: "text",
			Text: output.String(),
		}},
	}, nil
}
