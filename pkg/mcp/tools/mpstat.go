// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tools

import (
	"fmt"
	"strings"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// MpstatTool implements the `mpstat` command equivalent
// Shows per-CPU performance - third command in Brendan Gregg's 60s checklist
type MpstatTool struct {
	*BaseCollectorTool
}

// NewMpstatTool creates a new mpstat tool
func NewMpstatTool(logger logr.Logger, config performance.CollectionConfig) (*MpstatTool, error) {
	base, err := NewBaseCollectorTool(
		"mpstat",
		"Show per-CPU processor statistics (equivalent to `mpstat` command)",
		performance.MetricTypeCPU,
		logger,
		config,
	)
	if err != nil {
		return nil, err
	}

	return &MpstatTool{
		BaseCollectorTool: base,
	}, nil
}

// formatAsText formats CPU stats as mpstat command output
func (m *MpstatTool) formatAsText(data interface{}) (string, error) {
	cpuStats, ok := data.([]performance.CPUStats)
	if !ok {
		return "", fmt.Errorf("expected []CPUStats, got %T", data)
	}

	var output strings.Builder

	// Header line like mpstat
	output.WriteString("CPU    %usr   %nice    %sys %iowait    %irq   %soft  %steal  %guest  %gnice   %idle\n")

	for _, cpu := range cpuStats {
		total := cpu.User + cpu.Nice + cpu.System + cpu.Idle + cpu.IOWait + cpu.IRQ + cpu.SoftIRQ + cpu.Steal + cpu.Guest + cpu.GuestNice

		if total == 0 {
			continue // Skip if no data
		}

		cpuName := "all"
		if cpu.CPUIndex >= 0 {
			cpuName = fmt.Sprintf("%3d", cpu.CPUIndex)
		}

		output.WriteString(fmt.Sprintf("%-6s %6.2f %6.2f %6.2f %7.2f %6.2f %6.2f %6.2f %6.2f %6.2f %6.2f\n",
			cpuName,
			float64(cpu.User)*100.0/float64(total),      // %usr
			float64(cpu.Nice)*100.0/float64(total),      // %nice
			float64(cpu.System)*100.0/float64(total),    // %sys
			float64(cpu.IOWait)*100.0/float64(total),    // %iowait
			float64(cpu.IRQ)*100.0/float64(total),       // %irq
			float64(cpu.SoftIRQ)*100.0/float64(total),   // %soft
			float64(cpu.Steal)*100.0/float64(total),     // %steal
			float64(cpu.Guest)*100.0/float64(total),     // %guest
			float64(cpu.GuestNice)*100.0/float64(total), // %gnice
			float64(cpu.Idle)*100.0/float64(total),      // %idle
		))
	}

	return output.String(), nil
}

// formatAsTable formats CPU stats as a table
func (m *MpstatTool) formatAsTable(data interface{}) (string, error) {
	cpuStats, ok := data.([]performance.CPUStats)
	if !ok {
		return "", fmt.Errorf("expected []CPUStats, got %T", data)
	}

	var output strings.Builder

	output.WriteString("Per-CPU Statistics:\n")
	output.WriteString("┌─────┬────────┬────────┬────────┬────────┬────────┬────────┐\n")
	output.WriteString("│ CPU │  User  │ System │  Idle  │IOWait  │  IRQ   │ Steal  │\n")
	output.WriteString("├─────┼────────┼────────┼────────┼────────┼────────┼────────┤\n")

	for _, cpu := range cpuStats {
		total := cpu.User + cpu.Nice + cpu.System + cpu.Idle + cpu.IOWait + cpu.IRQ + cpu.SoftIRQ + cpu.Steal + cpu.Guest + cpu.GuestNice

		if total == 0 {
			continue
		}

		cpuName := "all"
		if cpu.CPUIndex >= 0 {
			cpuName = fmt.Sprintf("%3d", cpu.CPUIndex)
		}

		output.WriteString(fmt.Sprintf("│ %-3s │ %5.1f%% │ %5.1f%% │ %5.1f%% │ %5.1f%% │ %5.1f%% │ %5.1f%% │\n",
			cpuName,
			float64(cpu.User)*100.0/float64(total),
			float64(cpu.System)*100.0/float64(total),
			float64(cpu.Idle)*100.0/float64(total),
			float64(cpu.IOWait)*100.0/float64(total),
			float64(cpu.IRQ)*100.0/float64(total),
			float64(cpu.Steal)*100.0/float64(total),
		))
	}

	output.WriteString("└─────┴────────┴────────┴────────┴────────┴────────┴────────┘\n")

	return output.String(), nil
}
