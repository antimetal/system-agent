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

// IostatTool implements the `iostat` command equivalent
// Shows disk I/O statistics - fifth command in Brendan Gregg's 60s checklist
type IostatTool struct {
	*BaseCollectorTool
}

// NewIostatTool creates a new iostat tool
func NewIostatTool(logger logr.Logger, config performance.CollectionConfig) (*IostatTool, error) {
	base, err := NewBaseCollectorTool(
		"iostat",
		"Show disk I/O statistics (equivalent to `iostat` command)",
		performance.MetricTypeDisk,
		logger,
		config,
	)
	if err != nil {
		return nil, err
	}

	return &IostatTool{
		BaseCollectorTool: base,
	}, nil
}

// formatAsText formats disk stats as iostat command output
func (i *IostatTool) formatAsText(data interface{}) (string, error) {
	diskStats, ok := data.([]performance.DiskStats)
	if !ok {
		return "", fmt.Errorf("expected []DiskStats, got %T", data)
	}

	var output strings.Builder

	// Header like iostat -x
	output.WriteString("Device            r/s     w/s     rkB/s     wkB/s   rrqm/s   wrqm/s  %rrqm  %wrqm r_await w_await aqu-sz rareq-sz wareq-sz  svctm  %util\n")

	for _, disk := range diskStats {
		// Convert sectors to KB (sectors are typically 512 bytes)
		const sectorSize = 512
		readKBs := float64(disk.SectorsRead*sectorSize) / 1024.0
		writeKBs := float64(disk.SectorsWritten*sectorSize) / 1024.0

		// Calculate average request sizes
		avgReadSize := float64(0)
		if disk.ReadsCompleted > 0 {
			avgReadSize = readKBs / float64(disk.ReadsCompleted)
		}

		avgWriteSize := float64(0)
		if disk.WritesCompleted > 0 {
			avgWriteSize = writeKBs / float64(disk.WritesCompleted)
		}

		// Calculate average wait times (in milliseconds)
		avgReadWait := float64(0)
		if disk.ReadsCompleted > 0 {
			avgReadWait = float64(disk.ReadTime) / float64(disk.ReadsCompleted)
		}

		avgWriteWait := float64(0)
		if disk.WritesCompleted > 0 {
			avgWriteWait = float64(disk.WriteTime) / float64(disk.WritesCompleted)
		}

		// Use calculated fields if available, otherwise calculate approximations
		iops := disk.IOPS
		if iops == 0 {
			iops = float64(disk.ReadsCompleted + disk.WritesCompleted)
		}

		utilization := disk.Utilization
		if utilization == 0 && disk.IOTime > 0 {
			utilization = float64(disk.IOTime) / 10.0 // rough approximation
		}

		output.WriteString(fmt.Sprintf("%-16s %7.2f %7.2f %9.2f %9.2f %8.2f %8.2f %6.2f %6.2f %7.2f %7.2f %6.2f %8.2f %8.2f %6.2f %6.2f\n",
			disk.Device,
			float64(disk.ReadsCompleted),  // r/s
			float64(disk.WritesCompleted), // w/s
			readKBs,                       // rkB/s
			writeKBs,                      // wkB/s
			float64(disk.ReadsMerged),     // rrqm/s
			float64(disk.WritesMerged),    // wrqm/s
			0.0,                           // %rrqm (we don't have this)
			0.0,                           // %wrqm (we don't have this)
			avgReadWait,                   // r_await
			avgWriteWait,                  // w_await
			disk.AvgQueueSize,             // aqu-sz
			avgReadSize,                   // rareq-sz
			avgWriteSize,                  // wareq-sz
			0.0,                           // svctm (deprecated)
			utilization,                   // %util
		))
	}

	return output.String(), nil
}

// formatAsTable formats disk stats as a table
func (i *IostatTool) formatAsTable(data interface{}) (string, error) {
	diskStats, ok := data.([]performance.DiskStats)
	if !ok {
		return "", fmt.Errorf("expected []DiskStats, got %T", data)
	}

	var output strings.Builder

	output.WriteString("Disk I/O Statistics:\n")
	output.WriteString("┌──────────────┬─────────┬─────────┬───────────┬───────────┬────────────┬─────────┐\n")
	output.WriteString("│ Device       │ Reads/s │ Writes/s│  Read KB/s│ Write KB/s│     IOPS   │  Util%  │\n")
	output.WriteString("├──────────────┼─────────┼─────────┼───────────┼───────────┼────────────┼─────────┤\n")

	for _, disk := range diskStats {
		// Convert sectors to KB (sectors are typically 512 bytes)
		const sectorSize = 512
		readKBs := float64(disk.SectorsRead*sectorSize) / 1024.0
		writeKBs := float64(disk.SectorsWritten*sectorSize) / 1024.0

		iops := disk.IOPS
		if iops == 0 {
			iops = float64(disk.ReadsCompleted + disk.WritesCompleted)
		}

		utilization := disk.Utilization

		output.WriteString(fmt.Sprintf("│ %-12s │ %7.1f │ %7.1f │ %9.1f │ %9.1f │ %10.1f │ %6.1f%% │\n",
			disk.Device,
			float64(disk.ReadsCompleted),
			float64(disk.WritesCompleted),
			readKBs,
			writeKBs,
			iops,
			utilization,
		))
	}

	output.WriteString("└──────────────┴─────────┴─────────┴───────────┴───────────┴────────────┴─────────┘\n")

	return output.String(), nil
}
