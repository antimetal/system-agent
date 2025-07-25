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

	"github.com/antimetal/agent/pkg/mcp"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// BaseCollectorTool provides common functionality for collector-based MCP tools
type BaseCollectorTool struct {
	name             string
	description      string
	metricType       performance.MetricType
	logger           logr.Logger
	config           performance.CollectionConfig
	collectorFactory performance.NewContinuousCollector
}

// NewBaseCollectorTool creates a new base collector tool
func NewBaseCollectorTool(
	name, description string,
	metricType performance.MetricType,
	logger logr.Logger,
	config performance.CollectionConfig,
) (*BaseCollectorTool, error) {
	factory, err := performance.GetCollector(metricType)
	if err != nil {
		return nil, fmt.Errorf("failed to get collector for %s: %w", metricType, err)
	}

	return &BaseCollectorTool{
		name:             name,
		description:      description,
		metricType:       metricType,
		logger:           logger.WithName(name),
		config:           config,
		collectorFactory: factory,
	}, nil
}

// Name returns the tool name
func (b *BaseCollectorTool) Name() string {
	return b.name
}

// Description returns the tool description
func (b *BaseCollectorTool) Description() string {
	return b.description
}

// InputSchema returns the tool's input schema
func (b *BaseCollectorTool) InputSchema() mcp.ToolSchema {
	return mcp.ToolSchema{
		Type: "object",
		Properties: map[string]mcp.PropertySchema{
			"format": {
				Type:        "string",
				Description: "Output format (json, text, table)",
				Default:     "json",
				Enum:        []string{"json", "text", "table"},
			},
			"continuous": {
				Type:        "boolean",
				Description: "Run continuously instead of one-shot",
				Default:     false,
			},
			"duration": {
				Type:        "integer",
				Description: "Duration in seconds for continuous collection (default: 60)",
				Default:     60,
			},
		},
	}
}

// Execute performs the tool execution
func (b *BaseCollectorTool) Execute(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResponse, error) {
	// Parse arguments
	format := "json"
	if f, ok := args["format"].(string); ok {
		format = f
	}

	continuous := false
	if c, ok := args["continuous"].(bool); ok {
		continuous = c
	}

	// Create collector instance
	collector, err := b.collectorFactory(b.logger, b.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create collector: %w", err)
	}

	// Get capabilities for metadata
	caps := collector.Capabilities()

	if continuous {
		return b.executeContinuous(ctx, collector, format, args)
	} else {
		return b.executeOneShot(ctx, collector, format, caps)
	}
}

// executeOneShot performs a single collection
func (b *BaseCollectorTool) executeOneShot(ctx context.Context, collector performance.ContinuousCollector, format string, caps performance.CollectorCapabilities) (*mcp.CallToolResponse, error) {
	// Start collector
	ch, err := collector.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start collector: %w", err)
	}
	defer collector.Stop()

	// Wait for result
	select {
	case data := <-ch:
		result, err := b.formatOutput(data, format, caps)
		if err != nil {
			return nil, fmt.Errorf("failed to format output: %w", err)
		}
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// executeContinuous performs continuous collection
func (b *BaseCollectorTool) executeContinuous(ctx context.Context, collector performance.ContinuousCollector, format string, args map[string]interface{}) (*mcp.CallToolResponse, error) {
	duration := 60
	if d, ok := args["duration"].(float64); ok {
		duration = int(d)
	}

	// Start collector
	ch, err := collector.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start collector: %w", err)
	}
	defer collector.Stop()

	// Collect data for specified duration
	var results []interface{}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, getTimeout(duration))
	defer cancel()

	for {
		select {
		case data := <-ch:
			results = append(results, data)
		case <-timeoutCtx.Done():
			// Format and return collected results
			return b.formatContinuousOutput(results, format, collector.Capabilities())
		}
	}
}

// formatOutput formats the collected data according to the requested format
func (b *BaseCollectorTool) formatOutput(data interface{}, format string, caps performance.CollectorCapabilities) (*mcp.CallToolResponse, error) {
	switch format {
	case "json":
		jsonData, err := json.MarshalIndent(map[string]interface{}{
			"type":         string(b.metricType),
			"timestamp":    getCurrentTimestamp(),
			"capabilities": caps,
			"data":         data,
		}, "", "  ")
		if err != nil {
			return nil, err
		}
		return &mcp.CallToolResponse{
			Content: []mcp.ToolResult{{
				Type: "text",
				Text: string(jsonData),
			}},
		}, nil
	case "text":
		text, err := b.formatAsText(data)
		if err != nil {
			return nil, err
		}
		return &mcp.CallToolResponse{
			Content: []mcp.ToolResult{{
				Type: "text",
				Text: text,
			}},
		}, nil
	case "table":
		table, err := b.formatAsTable(data)
		if err != nil {
			return nil, err
		}
		return &mcp.CallToolResponse{
			Content: []mcp.ToolResult{{
				Type: "text",
				Text: table,
			}},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// formatContinuousOutput formats continuous collection results
func (b *BaseCollectorTool) formatContinuousOutput(results []interface{}, format string, caps performance.CollectorCapabilities) (*mcp.CallToolResponse, error) {
	switch format {
	case "json":
		jsonData, err := json.MarshalIndent(map[string]interface{}{
			"type":         string(b.metricType),
			"timestamp":    getCurrentTimestamp(),
			"capabilities": caps,
			"count":        len(results),
			"data":         results,
		}, "", "  ")
		if err != nil {
			return nil, err
		}
		return &mcp.CallToolResponse{
			Content: []mcp.ToolResult{{
				Type: "text",
				Text: string(jsonData),
			}},
		}, nil
	default:
		// For continuous collection, default to JSON
		return b.formatContinuousOutput(results, "json", caps)
	}
}

// formatAsText formats data as human-readable text (to be implemented by specific tools)
func (b *BaseCollectorTool) formatAsText(data interface{}) (string, error) {
	// Default implementation - specific tools can override
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(jsonData), nil
}

// formatAsTable formats data as a table (to be implemented by specific tools)
func (b *BaseCollectorTool) formatAsTable(data interface{}) (string, error) {
	// Default implementation - specific tools can override
	return b.formatAsText(data)
}
