// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/antimetal/agent/pkg/mcp"
	"github.com/antimetal/agent/pkg/mcp/tools"
	"github.com/antimetal/agent/pkg/performance"
	// Import collectors to register them
	_ "github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
)

func main() {
	var (
		procPath = flag.String("proc-path", "/proc", "Path to /proc filesystem")
		sysPath  = flag.String("sys-path", "/sys", "Path to /sys filesystem")
		devPath  = flag.String("dev-path", "/dev", "Path to /dev filesystem")
		interval = flag.Duration("interval", time.Second, "Collection interval")
		verbose  = flag.Bool("verbose", false, "Enable verbose logging")
	)

	flag.Parse()

	// Create logger that writes to stderr
	var logger logr.Logger
	if *verbose {
		logger = funcr.New(func(prefix, args string) {
			fmt.Fprintf(os.Stderr, "%s [%s] %s\n", time.Now().Format(time.RFC3339), prefix, args)
		}, funcr.Options{
			Verbosity: 2,
		})
	} else {
		logger = funcr.New(func(prefix, args string) {
			// Silent logger for stdio mode
		}, funcr.Options{
			Verbosity: 0,
		})
	}

	// Create performance collection config
	config := performance.CollectionConfig{
		Interval:        *interval,
		HostProcPath:    *procPath,
		HostSysPath:     *sysPath,
		HostDevPath:     *devPath,
		TopProcessCount: 20,
		EnabledCollectors: map[performance.MetricType]bool{
			performance.MetricTypeLoad:        true,
			performance.MetricTypeMemory:      true,
			performance.MetricTypeCPU:         true,
			performance.MetricTypeProcess:     true,
			performance.MetricTypeDisk:        true,
			performance.MetricTypeNetwork:     true,
			performance.MetricTypeTCP:         true,
			performance.MetricTypeKernel:      true,
			performance.MetricTypeCPUInfo:     true,
			performance.MetricTypeMemoryInfo:  true,
			performance.MetricTypeDiskInfo:    true,
			performance.MetricTypeNetworkInfo: true,
			performance.MetricTypeNUMA:        true,
		},
	}

	// Validate configuration
	validateOpts := performance.ValidateOptions{
		RequireHostProcPath: true,
		RequireHostSysPath:  true,
		RequireHostDevPath:  true,
	}
	if err := config.Validate(validateOpts); err != nil {
		logger.Error(err, "Invalid configuration")
		os.Exit(1)
	}

	// Create MCP server
	server := mcp.NewServer(logger)

	// Register all tools
	if err := tools.RegisterAllTools(server, logger, config); err != nil {
		logger.Error(err, "Failed to register tools")
		os.Exit(1)
	}

	// Start stdio server
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for scanner.Scan() {
		line := scanner.Text()
		
		var req mcp.Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			resp := mcp.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &mcp.Error{
					Code:    -32700,
					Message: "Parse error",
				},
			}
			encoder.Encode(resp)
			continue
		}

		// Handle the request
		resp := server.HandleRequest(req)
		encoder.Encode(resp)
	}

	if err := scanner.Err(); err != nil {
		logger.Error(err, "Scanner error")
		os.Exit(1)
	}
}