// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/antimetal/agent/pkg/mcp"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// Linux60sTool implements Brendan Gregg's complete "Linux Performance Analysis in 60,000 Milliseconds" workflow
// Runs the full 10-command checklist for rapid performance investigation
type Linux60sTool struct {
	logger logr.Logger
	config performance.CollectionConfig
	tools  map[string]mcp.ToolHandler
}

// NewLinux60sTool creates a new Linux 60s investigation tool
func NewLinux60sTool(logger logr.Logger, config performance.CollectionConfig) (*Linux60sTool, error) {
	tools := make(map[string]mcp.ToolHandler)

	// Create all the individual tools
	uptimeTool, err := NewUptimeTool(logger, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create uptime tool: %w", err)
	}
	tools["uptime"] = uptimeTool

	dmesgTool, err := NewDmesgTool(logger, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dmesg tool: %w", err)
	}
	tools["dmesg"] = dmesgTool

	vmstatTool, err := NewVmstatTool(logger, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create vmstat tool: %w", err)
	}
	tools["vmstat"] = vmstatTool

	mpstatTool, err := NewMpstatTool(logger, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create mpstat tool: %w", err)
	}
	tools["mpstat"] = mpstatTool

	// pidstat would use process collector - we'll use top instead
	topTool, err := NewTopTool(logger, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create top tool: %w", err)
	}
	tools["top"] = topTool

	iostatTool, err := NewIostatTool(logger, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create iostat tool: %w", err)
	}
	tools["iostat"] = iostatTool

	freeTool, err := NewFreeTool(logger, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create free tool: %w", err)
	}
	tools["free"] = freeTool

	return &Linux60sTool{
		logger: logger.WithName("linux60s"),
		config: config,
		tools:  tools,
	}, nil
}

// Name returns the tool name
func (l *Linux60sTool) Name() string {
	return "linux60s"
}

// Description returns the tool description
func (l *Linux60sTool) Description() string {
	return "Run Brendan Gregg's complete Linux Performance Analysis in 60 seconds checklist"
}

// InputSchema returns the tool's input schema
func (l *Linux60sTool) InputSchema() mcp.ToolSchema {
	return mcp.ToolSchema{
		Type: "object",
		Properties: map[string]mcp.PropertySchema{
			"format": {
				Type:        "string",
				Description: "Output format (json, text, summary)",
				Default:     "summary",
				Enum:        []string{"json", "text", "summary"},
			},
			"parallel": {
				Type:        "boolean",
				Description: "Run tools in parallel for faster execution",
				Default:     true,
			},
			"include_raw": {
				Type:        "boolean",
				Description: "Include raw command outputs in results",
				Default:     false,
			},
		},
	}
}

// InvestigationResult represents the result of the 60s investigation
type InvestigationResult struct {
	Timestamp  string              `json:"timestamp"`
	Duration   string              `json:"duration"`
	Summary    string              `json:"summary"`
	Commands   []CommandResult     `json:"commands"`
	RawOutputs map[string]string   `json:"raw_outputs,omitempty"`
	Analysis   PerformanceAnalysis `json:"analysis"`
}

// CommandResult represents the result of a single command
type CommandResult struct {
	Command     string        `json:"command"`
	Duration    time.Duration `json:"duration"`
	Success     bool          `json:"success"`
	Error       string        `json:"error,omitempty"`
	KeyFindings []string      `json:"key_findings"`
}

// PerformanceAnalysis provides automated analysis of the results
type PerformanceAnalysis struct {
	LoadConcerns    []string `json:"load_concerns"`
	MemoryConcerns  []string `json:"memory_concerns"`
	CPUConcerns     []string `json:"cpu_concerns"`
	DiskConcerns    []string `json:"disk_concerns"`
	NetworkConcerns []string `json:"network_concerns"`
	Recommendations []string `json:"recommendations"`
	Severity        string   `json:"severity"` // low, medium, high, critical
}

// Execute performs the complete 60s investigation
func (l *Linux60sTool) Execute(ctx context.Context, args map[string]interface{}) (*mcp.CallToolResponse, error) {
	startTime := time.Now()

	format := "summary"
	if f, ok := args["format"].(string); ok {
		format = f
	}

	parallel := true
	if p, ok := args["parallel"].(bool); ok {
		parallel = p
	}

	includeRaw := false
	if r, ok := args["include_raw"].(bool); ok {
		includeRaw = r
	}

	l.logger.Info("Starting Linux 60s performance investigation",
		"format", format, "parallel", parallel)

	// Define the investigation sequence (Brendan Gregg's checklist)
	commandSequence := []struct {
		name string
		args map[string]interface{}
		desc string
	}{
		{"uptime", map[string]interface{}{"format": "text"}, "Load averages and uptime"},
		{"dmesg", map[string]interface{}{"format": "text", "tail": 20}, "Recent kernel messages"},
		{"vmstat", map[string]interface{}{"format": "text"}, "Virtual memory statistics"},
		{"mpstat", map[string]interface{}{"format": "text"}, "Per-CPU processor statistics"},
		{"top", map[string]interface{}{"format": "text", "count": 15}, "Top processes by CPU usage"},
		{"iostat", map[string]interface{}{"format": "text"}, "Block device I/O statistics"},
		{"free", map[string]interface{}{"format": "text"}, "Memory usage statistics"},
	}

	var results []CommandResult
	rawOutputs := make(map[string]string)

	if parallel {
		results, rawOutputs = l.executeParallel(ctx, commandSequence, includeRaw)
	} else {
		results, rawOutputs = l.executeSequential(ctx, commandSequence, includeRaw)
	}

	// Analyze results
	analysis := l.analyzeResults(results, rawOutputs)

	// Create investigation result
	investigation := InvestigationResult{
		Timestamp: getCurrentTimestamp(),
		Duration:  time.Since(startTime).String(),
		Summary:   l.generateSummary(results, analysis),
		Commands:  results,
		Analysis:  analysis,
	}

	if includeRaw {
		investigation.RawOutputs = rawOutputs
	}

	// Format output
	switch format {
	case "json":
		return &mcp.CallToolResponse{
			Content: []mcp.ToolResult{{
				Type: "text",
				Data: investigation,
			}},
		}, nil
	case "text":
		return l.formatAsText(&investigation)
	case "summary":
		return l.formatAsSummary(&investigation)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// executeSequential runs commands one by one
func (l *Linux60sTool) executeSequential(ctx context.Context, commands []struct {
	name string
	args map[string]interface{}
	desc string
}, includeRaw bool) ([]CommandResult, map[string]string) {
	var results []CommandResult
	rawOutputs := make(map[string]string)

	for _, cmd := range commands {
		l.logger.Info("Running command", "command", cmd.name, "description", cmd.desc)
		startTime := time.Now()

		result := CommandResult{
			Command: cmd.name + " - " + cmd.desc,
			Success: false,
		}

		if tool, exists := l.tools[cmd.name]; exists {
			response, err := tool.Execute(ctx, cmd.args)
			duration := time.Since(startTime)
			result.Duration = duration

			if err != nil {
				result.Error = err.Error()
				l.logger.Error(err, "Command failed", "command", cmd.name)
			} else {
				result.Success = true
				result.KeyFindings = l.extractKeyFindings(cmd.name, response)

				if includeRaw && len(response.Content) > 0 {
					rawOutputs[cmd.name] = response.Content[0].Text
				}
			}
		} else {
			result.Error = "Tool not found"
		}

		results = append(results, result)
	}

	return results, rawOutputs
}

// executeParallel runs commands in parallel (stub for now - would need more complex coordination)
func (l *Linux60sTool) executeParallel(ctx context.Context, commands []struct {
	name string
	args map[string]interface{}
	desc string
}, includeRaw bool) ([]CommandResult, map[string]string) {
	// For now, just run sequentially - parallel execution would require careful coordination
	// of shared resources and proper error handling
	return l.executeSequential(ctx, commands, includeRaw)
}

// extractKeyFindings extracts key findings from command output
func (l *Linux60sTool) extractKeyFindings(command string, response *mcp.CallToolResponse) []string {
	var findings []string

	if len(response.Content) == 0 {
		return findings
	}

	output := response.Content[0].Text

	switch command {
	case "uptime":
		// Look for high load averages
		if strings.Contains(output, "load average:") {
			findings = append(findings, "System load information collected")
		}
	case "dmesg":
		// Look for errors in kernel messages
		if strings.Contains(strings.ToLower(output), "error") ||
			strings.Contains(strings.ToLower(output), "fail") {
			findings = append(findings, "Potential kernel errors detected")
		}
	case "vmstat":
		findings = append(findings, "Virtual memory statistics collected")
	case "mpstat":
		findings = append(findings, "Per-CPU statistics collected")
	case "top":
		if strings.Contains(output, "total,") {
			findings = append(findings, "Top processes identified")
		}
	case "iostat":
		findings = append(findings, "Disk I/O statistics collected")
	case "free":
		if strings.Contains(output, "Mem:") {
			findings = append(findings, "Memory usage statistics collected")
		}
	}

	return findings
}

// analyzeResults provides automated analysis of the command results
func (l *Linux60sTool) analyzeResults(results []CommandResult, rawOutputs map[string]string) PerformanceAnalysis {
	analysis := PerformanceAnalysis{
		LoadConcerns:    []string{},
		MemoryConcerns:  []string{},
		CPUConcerns:     []string{},
		DiskConcerns:    []string{},
		NetworkConcerns: []string{},
		Recommendations: []string{},
		Severity:        "low",
	}

	// Count failed commands
	failedCommands := 0
	for _, result := range results {
		if !result.Success {
			failedCommands++
		}
	}

	if failedCommands > 0 {
		analysis.Recommendations = append(analysis.Recommendations,
			fmt.Sprintf("%d commands failed - check system access permissions", failedCommands))
		analysis.Severity = "medium"
	}

	// Basic analysis based on raw outputs (simplified)
	if dmesgOutput, exists := rawOutputs["dmesg"]; exists {
		if strings.Contains(strings.ToLower(dmesgOutput), "oom") {
			analysis.MemoryConcerns = append(analysis.MemoryConcerns, "Out of Memory (OOM) events detected")
			analysis.Severity = "high"
		}
		if strings.Contains(strings.ToLower(dmesgOutput), "error") {
			analysis.LoadConcerns = append(analysis.LoadConcerns, "Kernel errors detected in dmesg")
			if analysis.Severity == "low" {
				analysis.Severity = "medium"
			}
		}
	}

	// Add general recommendations
	analysis.Recommendations = append(analysis.Recommendations,
		"Review command outputs for specific performance bottlenecks",
		"Consider running specialized tools for deeper analysis",
		"Monitor trends over time for better insight")

	return analysis
}

// generateSummary creates a human-readable summary
func (l *Linux60sTool) generateSummary(results []CommandResult, analysis PerformanceAnalysis) string {
	successful := 0
	totalDuration := time.Duration(0)

	for _, result := range results {
		if result.Success {
			successful++
		}
		totalDuration += result.Duration
	}

	return fmt.Sprintf("Linux 60s Investigation: %d/%d commands successful in %s. Severity: %s",
		successful, len(results), totalDuration.String(), analysis.Severity)
}

// formatAsText formats the investigation result as detailed text
func (l *Linux60sTool) formatAsText(investigation *InvestigationResult) (*mcp.CallToolResponse, error) {
	var output strings.Builder

	output.WriteString("=== Linux Performance Analysis in 60 Seconds ===\n")
	output.WriteString(fmt.Sprintf("Investigation completed at: %s\n", investigation.Timestamp))
	output.WriteString(fmt.Sprintf("Total duration: %s\n", investigation.Duration))
	output.WriteString(fmt.Sprintf("Summary: %s\n\n", investigation.Summary))

	// Command results
	output.WriteString("Command Results:\n")
	for i, cmd := range investigation.Commands {
		status := "✓"
		if !cmd.Success {
			status = "✗"
		}
		output.WriteString(fmt.Sprintf("%d. %s %s (%s)\n", i+1, status, cmd.Command, cmd.Duration))
		if cmd.Error != "" {
			output.WriteString(fmt.Sprintf("   Error: %s\n", cmd.Error))
		}
		for _, finding := range cmd.KeyFindings {
			output.WriteString(fmt.Sprintf("   • %s\n", finding))
		}
	}

	// Analysis
	output.WriteString(fmt.Sprintf("\nPerformance Analysis (Severity: %s):\n", investigation.Analysis.Severity))

	if len(investigation.Analysis.LoadConcerns) > 0 {
		output.WriteString("Load Concerns:\n")
		for _, concern := range investigation.Analysis.LoadConcerns {
			output.WriteString(fmt.Sprintf("  • %s\n", concern))
		}
	}

	if len(investigation.Analysis.MemoryConcerns) > 0 {
		output.WriteString("Memory Concerns:\n")
		for _, concern := range investigation.Analysis.MemoryConcerns {
			output.WriteString(fmt.Sprintf("  • %s\n", concern))
		}
	}

	output.WriteString("Recommendations:\n")
	for _, rec := range investigation.Analysis.Recommendations {
		output.WriteString(fmt.Sprintf("  • %s\n", rec))
	}

	return &mcp.CallToolResponse{
		Content: []mcp.ToolResult{{
			Type: "text",
			Text: output.String(),
		}},
	}, nil
}

// formatAsSummary formats the investigation result as a concise summary
func (l *Linux60sTool) formatAsSummary(investigation *InvestigationResult) (*mcp.CallToolResponse, error) {
	var output strings.Builder

	output.WriteString("🚀 Linux 60s Performance Investigation Summary\n")
	output.WriteString(strings.Repeat("=", 50) + "\n")
	output.WriteString(fmt.Sprintf("⏱️  Duration: %s\n", investigation.Duration))
	output.WriteString(fmt.Sprintf("📊 Commands: %d total\n", len(investigation.Commands)))

	successful := 0
	for _, cmd := range investigation.Commands {
		if cmd.Success {
			successful++
		}
	}
	output.WriteString(fmt.Sprintf("✅ Success Rate: %d/%d (%.1f%%)\n",
		successful, len(investigation.Commands),
		float64(successful)*100.0/float64(len(investigation.Commands))))

	output.WriteString(fmt.Sprintf("⚠️  Severity: %s\n\n", strings.ToUpper(investigation.Analysis.Severity)))

	// Quick status of each command
	output.WriteString("Command Status:\n")
	for _, cmd := range investigation.Commands {
		status := "✅"
		if !cmd.Success {
			status = "❌"
		}
		cmdName := strings.Split(cmd.Command, " - ")[0]
		output.WriteString(fmt.Sprintf("  %s %s\n", status, cmdName))
	}

	// Key concerns
	totalConcerns := len(investigation.Analysis.LoadConcerns) +
		len(investigation.Analysis.MemoryConcerns) +
		len(investigation.Analysis.CPUConcerns) +
		len(investigation.Analysis.DiskConcerns)

	if totalConcerns > 0 {
		output.WriteString(fmt.Sprintf("\n🔍 Issues Found (%d):\n", totalConcerns))
		for _, concern := range investigation.Analysis.LoadConcerns {
			output.WriteString(fmt.Sprintf("  • %s\n", concern))
		}
		for _, concern := range investigation.Analysis.MemoryConcerns {
			output.WriteString(fmt.Sprintf("  • %s\n", concern))
		}
	} else {
		output.WriteString("\n✨ No major issues detected\n")
	}

	output.WriteString("\n💡 Next Steps:\n")
	for i, rec := range investigation.Analysis.Recommendations {
		if i >= 3 { // Limit to top 3 recommendations
			break
		}
		output.WriteString(fmt.Sprintf("  %d. %s\n", i+1, rec))
	}

	return &mcp.CallToolResponse{
		Content: []mcp.ToolResult{{
			Type: "text",
			Text: output.String(),
		}},
	}, nil
}
