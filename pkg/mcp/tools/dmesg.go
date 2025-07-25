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
	"time"

	"github.com/antimetal/agent/pkg/mcp"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// DmesgTool implements the `dmesg` command equivalent
// Shows kernel ring buffer messages - second command in Brendan Gregg's 60s checklist
type DmesgTool struct {
	*BaseCollectorTool
}

// NewDmesgTool creates a new dmesg tool
func NewDmesgTool(logger logr.Logger, config performance.CollectionConfig) (*DmesgTool, error) {
	base, err := NewBaseCollectorTool(
		"dmesg",
		"Show kernel ring buffer messages (equivalent to `dmesg` command)",
		performance.MetricTypeKernel,
		logger,
		config,
	)
	if err != nil {
		return nil, err
	}

	return &DmesgTool{
		BaseCollectorTool: base,
	}, nil
}

// InputSchema returns the tool's input schema
func (d *DmesgTool) InputSchema() mcp.ToolSchema {
	return mcp.ToolSchema{
		Type: "object",
		Properties: map[string]mcp.PropertySchema{
			"format": {
				Type:        "string",
				Description: "Output format (json, text, table)",
				Default:     "text",
				Enum:        []string{"json", "text", "table"},
			},
			"tail": {
				Type:        "integer",
				Description: "Show last N messages (default: 50)",
				Default:     50,
			},
			"level": {
				Type:        "string",
				Description: "Minimum log level to show (emerg, alert, crit, err, warn, notice, info, debug)",
				Default:     "info",
				Enum:        []string{"emerg", "alert", "crit", "err", "warn", "notice", "info", "debug"},
			},
			"grep": {
				Type:        "string",
				Description: "Filter messages containing this string",
			},
		},
	}
}

// Execute performs the tool execution with additional filtering
func (d *DmesgTool) Execute(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResponse, error) {
	// Get base arguments
	format := "text"
	if f, ok := args["format"].(string); ok {
		format = f
	}

	tail := 50
	if t, ok := args["tail"].(float64); ok {
		tail = int(t)
	}

	level := "info"
	if l, ok := args["level"].(string); ok {
		level = l
	}

	grep := ""
	if g, ok := args["grep"].(string); ok {
		grep = g
	}

	// Create collector instance
	collector, err := d.collectorFactory(d.logger, d.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create collector: %w", err)
	}

	// Start collector with a timeout
	ch, err := collector.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start collector: %w", err)
	}
	defer collector.Stop()

	// Create a timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Wait for result with timeout
	select {
	case data := <-ch:
		if data == nil {
			return nil, fmt.Errorf("received nil data from collector")
		}
		
		// Handle both pointer and non-pointer slices
		var msgs []performance.KernelMessage
		switch v := data.(type) {
		case []performance.KernelMessage:
			msgs = v
		case []*performance.KernelMessage:
			msgs = make([]performance.KernelMessage, len(v))
			for i, msg := range v {
				if msg != nil {
					msgs[i] = *msg
				}
			}
		default:
			return nil, fmt.Errorf("expected []KernelMessage or []*KernelMessage, got %T", data)
		}

		// Filter messages
		filteredMsgs := d.filterMessages(msgs, level, grep, tail)

		// Format output
		return d.formatOutput(filteredMsgs, format, collector.Capabilities())
	case <-timeoutCtx.Done():
		return nil, fmt.Errorf("timeout waiting for kernel messages")
	}
}

// filterMessages filters kernel messages based on criteria
func (d *DmesgTool) filterMessages(messages []performance.KernelMessage, minLevel, grep string, tail int) []performance.KernelMessage {
	// Convert level string to numeric
	levelMap := map[string]uint8{
		"emerg": 0, "alert": 1, "crit": 2, "err": 3,
		"warn": 4, "notice": 5, "info": 6, "debug": 7,
	}

	minLevelNum, exists := levelMap[minLevel]
	if !exists {
		minLevelNum = 6 // default to info
	}

	var filtered []performance.KernelMessage

	for _, msg := range messages {
		// Filter by log level
		if msg.Severity > minLevelNum {
			continue
		}

		// Filter by grep pattern
		if grep != "" && !strings.Contains(strings.ToLower(msg.Message), strings.ToLower(grep)) {
			continue
		}

		filtered = append(filtered, msg)
	}

	// Sort by timestamp (most recent first for dmesg style)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})

	// Limit to tail count
	if tail > 0 && tail < len(filtered) {
		filtered = filtered[:tail]
	}

	return filtered
}

// formatOutput formats the kernel messages according to the requested format
func (d *DmesgTool) formatOutput(messages []performance.KernelMessage, format string, caps performance.CollectorCapabilities) (*mcp.CallToolResponse, error) {
	switch format {
	case "json":
		return d.formatAsJSON(messages, caps)
	case "text":
		return d.formatAsText(messages)
	case "table":
		return d.formatAsTable(messages)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// formatAsJSON formats messages as JSON
func (d *DmesgTool) formatAsJSON(messages []performance.KernelMessage, caps performance.CollectorCapabilities) (*mcp.CallToolResponse, error) {
	result := map[string]interface{}{
		"type":         "dmesg",
		"timestamp":    getCurrentTimestamp(),
		"capabilities": caps,
		"count":        len(messages),
		"messages":     messages,
	}

	return &mcp.CallToolResponse{
		Content: []mcp.ToolResult{{
			Type: "text",
			Data: result,
		}},
	}, nil
}

// formatAsText formats messages as dmesg command output
func (d *DmesgTool) formatAsText(messages []performance.KernelMessage) (*mcp.CallToolResponse, error) {
	var output strings.Builder

	severityNames := []string{"emerg", "alert", "crit", "err", "warn", "notice", "info", "debug"}

	for _, msg := range messages {
		severityName := "unknown"
		if int(msg.Severity) < len(severityNames) {
			severityName = severityNames[msg.Severity]
		}

		// Format like dmesg output: [timestamp] message
		output.WriteString(fmt.Sprintf("[%s] <%s> %s",
			msg.Timestamp.Format("Jan 02 15:04:05"),
			severityName,
			msg.Message))

		if msg.Subsystem != "" {
			output.WriteString(fmt.Sprintf(" [%s]", msg.Subsystem))
		}
		if msg.Device != "" {
			output.WriteString(fmt.Sprintf(" (%s)", msg.Device))
		}

		output.WriteString("\n")
	}

	return &mcp.CallToolResponse{
		Content: []mcp.ToolResult{{
			Type: "text",
			Text: output.String(),
		}},
	}, nil
}

// formatAsTable formats messages as a table
func (d *DmesgTool) formatAsTable(messages []performance.KernelMessage) (*mcp.CallToolResponse, error) {
	var output strings.Builder

	severityNames := []string{"emerg", "alert", "crit", "err", "warn", "notice", "info", "debug"}

	output.WriteString("Kernel Messages:\n")
	output.WriteString("┌──────────────────┬──────────┬────────────┬────────────────────────────────────────────────┐\n")
	output.WriteString("│ Timestamp        │ Severity │ Subsystem  │ Message                                        │\n")
	output.WriteString("├──────────────────┼──────────┼────────────┼────────────────────────────────────────────────┤\n")

	for _, msg := range messages {
		severityName := "unknown"
		if int(msg.Severity) < len(severityNames) {
			severityName = severityNames[msg.Severity]
		}

		subsystem := msg.Subsystem
		if subsystem == "" {
			subsystem = "-"
		}
		if len(subsystem) > 10 {
			subsystem = subsystem[:10]
		}

		message := msg.Message
		if len(message) > 46 {
			message = message[:43] + "..."
		}

		output.WriteString(fmt.Sprintf("│ %-16s │ %-8s │ %-10s │ %-46s │\n",
			msg.Timestamp.Format("Jan 02 15:04:05"),
			severityName,
			subsystem,
			message,
		))
	}

	output.WriteString("└──────────────────┴──────────┴────────────┴────────────────────────────────────────────────┘\n")

	return &mcp.CallToolResponse{
		Content: []mcp.ToolResult{{
			Type: "text",
			Text: output.String(),
		}},
	}, nil
}
