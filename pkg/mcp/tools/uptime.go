// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/antimetal/agent/pkg/mcp"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// UptimeTool implements the `uptime` command equivalent
// Shows load averages and system uptime - first command in Brendan Gregg's 60s checklist
type UptimeTool struct {
	*BaseCollectorTool
}

// NewUptimeTool creates a new uptime tool
func NewUptimeTool(logger logr.Logger, config performance.CollectionConfig) (*UptimeTool, error) {
	base, err := NewBaseCollectorTool(
		"uptime",
		"Show system uptime and load averages (equivalent to `uptime` command)",
		performance.MetricTypeLoad,
		logger,
		config,
	)
	if err != nil {
		return nil, err
	}

	return &UptimeTool{
		BaseCollectorTool: base,
	}, nil
}

// Execute overrides the base Execute to provide custom JSON formatting
func (u *UptimeTool) Execute(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResponse, error) {
	// Parse format argument
	format := "json"
	if f, ok := args["format"].(string); ok {
		format = f
	}

	// For non-JSON formats, use the base implementation
	if format != "json" {
		return u.BaseCollectorTool.Execute(ctx, args)
	}

	// For JSON, we need to intercept and reformat
	// Create collector instance
	collector, err := u.collectorFactory(u.logger, u.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create collector: %w", err)
	}

	// Start collector
	ch, err := collector.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start collector: %w", err)
	}
	defer collector.Stop()

	// Wait for result
	select {
	case data := <-ch:
		// Use our custom JSON formatter
		jsonStr, err := u.formatAsJSON(data)
		if err != nil {
			return nil, fmt.Errorf("failed to format JSON: %w", err)
		}

		return &mcp.CallToolResponse{
			Content: []mcp.ToolResult{
				{
					Type: "text",
					Text: jsonStr,
				},
			},
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// formatAsText formats load stats as uptime command output
func (u *UptimeTool) formatAsText(data interface{}) (string, error) {
	loadStats, ok := data.(*performance.LoadStats)
	if !ok {
		return "", fmt.Errorf("expected LoadStats, got %T", data)
	}

	// Format similar to uptime command:
	// 14:25:12 up 8 days, 4:32, 2 users, load average: 0.52, 0.47, 0.45

	// Calculate uptime parts
	days := int(loadStats.Uptime.Hours()) / 24
	hours := int(loadStats.Uptime.Hours()) % 24
	minutes := int(loadStats.Uptime.Minutes()) % 60

	var uptimeParts []string
	if days > 0 {
		if days == 1 {
			uptimeParts = append(uptimeParts, "1 day")
		} else {
			uptimeParts = append(uptimeParts, fmt.Sprintf("%d days", days))
		}
	}
	if hours > 0 || days > 0 {
		uptimeParts = append(uptimeParts, fmt.Sprintf("%d:%02d", hours, minutes))
	} else {
		uptimeParts = append(uptimeParts, fmt.Sprintf("%d min", minutes))
	}

	uptimeStr := strings.Join(uptimeParts, ", ")

	return fmt.Sprintf("up %s, %d/%d processes running, load average: %.2f, %.2f, %.2f",
		uptimeStr,
		loadStats.RunningProcs,
		loadStats.TotalProcs,
		loadStats.Load1Min,
		loadStats.Load5Min,
		loadStats.Load15Min,
	), nil
}

// formatAsTable formats load stats as a table
func (u *UptimeTool) formatAsTable(data interface{}) (string, error) {
	loadStats, ok := data.(*performance.LoadStats)
	if !ok {
		return "", fmt.Errorf("expected LoadStats, got %T", data)
	}

	return fmt.Sprintf(`System Load Information:
┌─────────────────┬──────────┐
│ Metric          │ Value    │
├─────────────────┼──────────┤
│ Uptime          │ %s       │
│ Running Procs   │ %d       │
│ Total Procs     │ %d       │
│ Load 1min       │ %.2f     │
│ Load 5min       │ %.2f     │
│ Load 15min      │ %.2f     │
│ Last PID        │ %d       │
└─────────────────┴──────────┘`,
		formatDuration(loadStats.Uptime),
		loadStats.RunningProcs,
		loadStats.TotalProcs,
		loadStats.Load1Min,
		loadStats.Load5Min,
		loadStats.Load15Min,
		loadStats.LastPID,
	), nil
}

// formatAsJSON formats load stats as JSON with proper units
func (u *UptimeTool) formatAsJSON(data interface{}) (string, error) {
	loadStats, ok := data.(*performance.LoadStats)
	if !ok {
		return "", fmt.Errorf("expected LoadStats, got %T", data)
	}

	// Convert to a more user-friendly structure
	output := map[string]interface{}{
		"load_1min":      loadStats.Load1Min,
		"load_5min":      loadStats.Load5Min,
		"load_15min":     loadStats.Load15Min,
		"running_procs":  loadStats.RunningProcs,
		"total_procs":    loadStats.TotalProcs,
		"last_pid":       loadStats.LastPID,
		"uptime_seconds": loadStats.Uptime.Seconds(),
		"uptime_human":   formatDuration(loadStats.Uptime),
		"_units": map[string]string{
			"load_*min":      "system load average (runnable processes)",
			"running_procs":  "count of currently running processes",
			"total_procs":    "total count of processes",
			"last_pid":       "most recently assigned process ID",
			"uptime_seconds": "seconds since system boot",
			"uptime_human":   "human-readable uptime",
		},
	}

	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return string(jsonData), nil
}
