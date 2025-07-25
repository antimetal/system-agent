// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tools

import (
	"github.com/antimetal/agent/pkg/mcp"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// RegisterAllTools registers all available performance analysis tools with the MCP server
func RegisterAllTools(server *mcp.Server, logger logr.Logger, config performance.CollectionConfig) error {
	logger.Info("Registering performance analysis tools")

	// Individual Brendan Gregg checklist tools
	tools := []struct {
		name    string
		factory func(logr.Logger, performance.CollectionConfig) (mcp.ToolHandler, error)
	}{
		{"uptime", func(l logr.Logger, c performance.CollectionConfig) (mcp.ToolHandler, error) {
			return NewUptimeTool(l, c)
		}},
		{"dmesg", func(l logr.Logger, c performance.CollectionConfig) (mcp.ToolHandler, error) {
			return NewDmesgTool(l, c)
		}},
		{"vmstat", func(l logr.Logger, c performance.CollectionConfig) (mcp.ToolHandler, error) {
			return NewVmstatTool(l, c)
		}},
		{"mpstat", func(l logr.Logger, c performance.CollectionConfig) (mcp.ToolHandler, error) {
			return NewMpstatTool(l, c)
		}},
		{"iostat", func(l logr.Logger, c performance.CollectionConfig) (mcp.ToolHandler, error) {
			return NewIostatTool(l, c)
		}},
		{"free", func(l logr.Logger, c performance.CollectionConfig) (mcp.ToolHandler, error) {
			return NewFreeTool(l, c)
		}},
		{"top", func(l logr.Logger, c performance.CollectionConfig) (mcp.ToolHandler, error) {
			return NewTopTool(l, c)
		}},
		{"linux60s", func(l logr.Logger, c performance.CollectionConfig) (mcp.ToolHandler, error) {
			return NewLinux60sTool(l, c)
		}},
	}

	// Register each tool
	for _, tool := range tools {
		handler, err := tool.factory(logger, config)
		if err != nil {
			logger.Error(err, "Failed to create tool", "tool", tool.name)
			// Continue with other tools instead of failing completely
			continue
		}

		server.RegisterTool(handler)
		logger.Info("Registered tool", "name", tool.name)
	}

	// Register raw collector tools
	collectorTools := []struct {
		name       string
		metricType performance.MetricType
		desc       string
	}{
		{"load", performance.MetricTypeLoad, "System load averages and uptime"},
		{"memory", performance.MetricTypeMemory, "Memory usage statistics"},
		{"cpu", performance.MetricTypeCPU, "CPU utilization statistics"},
		{"process", performance.MetricTypeProcess, "Process information and statistics"},
		{"disk", performance.MetricTypeDisk, "Disk I/O statistics"},
		{"network", performance.MetricTypeNetwork, "Network interface statistics"},
		{"tcp", performance.MetricTypeTCP, "TCP connection statistics"},
		{"kernel", performance.MetricTypeKernel, "Kernel messages and logs"},
		{"cpu_info", performance.MetricTypeCPUInfo, "CPU hardware information"},
		{"memory_info", performance.MetricTypeMemoryInfo, "Memory hardware information"},
		{"disk_info", performance.MetricTypeDiskInfo, "Disk hardware information"},
		{"network_info", performance.MetricTypeNetworkInfo, "Network hardware information"},
		{"numa", performance.MetricTypeNUMA, "NUMA topology and statistics"},
	}

	for _, collector := range collectorTools {
		handler, err := NewBaseCollectorTool(
			collector.name,
			collector.desc,
			collector.metricType,
			logger,
			config,
		)
		if err != nil {
			logger.Error(err, "Failed to create collector tool", "collector", collector.name)
			continue
		}

		server.RegisterTool(handler)
		logger.Info("Registered collector tool", "name", collector.name, "type", collector.metricType)
	}

	logger.Info("Completed tool registration")
	return nil
}

// GetToolCapabilities returns a summary of all available tools and their capabilities
func GetToolCapabilities(config performance.CollectionConfig) (map[string]interface{}, error) {
	capabilities := map[string]interface{}{
		"server_info": map[string]interface{}{
			"name":        "Antimetal Performance Analysis MCP Server",
			"version":     "1.0.0",
			"description": "MCP server exposing Linux performance analysis tools based on Brendan Gregg's methodology",
		},
		"investigation_tools": []map[string]interface{}{
			{
				"name":        "linux60s",
				"description": "Complete Brendan Gregg 60-second Linux performance investigation",
				"type":        "workflow",
				"category":    "investigation",
			},
		},
		"system_tools": []map[string]interface{}{
			{
				"name":        "uptime",
				"description": "System uptime and load averages",
				"equivalent":  "uptime command",
				"category":    "system",
			},
			{
				"name":        "dmesg",
				"description": "Kernel ring buffer messages",
				"equivalent":  "dmesg command",
				"category":    "kernel",
			},
			{
				"name":        "vmstat",
				"description": "Virtual memory statistics",
				"equivalent":  "vmstat command",
				"category":    "memory",
			},
			{
				"name":        "mpstat",
				"description": "Per-CPU processor statistics",
				"equivalent":  "mpstat command",
				"category":    "cpu",
			},
			{
				"name":        "iostat",
				"description": "Block device I/O statistics",
				"equivalent":  "iostat command",
				"category":    "disk",
			},
			{
				"name":        "free",
				"description": "Memory usage statistics",
				"equivalent":  "free command",
				"category":    "memory",
			},
			{
				"name":        "top",
				"description": "Top processes by resource usage",
				"equivalent":  "top command",
				"category":    "process",
			},
		},
		"raw_collectors": []string{
			"load", "memory", "cpu", "process", "disk", "network", "tcp",
			"kernel", "cpu_info", "memory_info", "disk_info", "network_info", "numa",
		},
		"config": map[string]interface{}{
			"host_proc_path": config.HostProcPath,
			"host_sys_path":  config.HostSysPath,
			"host_dev_path":  config.HostDevPath,
			"interval":       config.Interval.String(),
		},
	}

	return capabilities, nil
}
