// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

func init() {
	performance.Register(performance.MetricTypePSI, performance.PartialNewContinuousPointCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewPSICollector(logger, config)
		},
	))
}

var _ performance.PointCollector = (*PSICollector)(nil)

// PSICollector collects Pressure Stall Information from /proc/pressure/
// PSI provides metrics about resource contention (CPU, memory, I/O)
// Reference: https://www.kernel.org/doc/html/latest/accounting/psi.html
// Available since kernel 4.20
type PSICollector struct {
	performance.BaseCollector
	cpuPath    string
	memoryPath string
	ioPath     string
}

func NewPSICollector(logger logr.Logger, config performance.CollectionConfig) (*PSICollector, error) {
	if err := config.Validate(performance.ValidateOptions{RequireHostProcPath: true}); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   false,
		RequiredCapabilities: nil,
		MinKernelVersion:     "4.20.0",
	}

	pressureDir := filepath.Join(config.HostProcPath, "pressure")
	if _, err := os.Stat(pressureDir); err != nil {
		return nil, fmt.Errorf("PSI not available (kernel < 4.20 or CONFIG_PSI=n): %w", err)
	}

	return &PSICollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypePSI,
			"Pressure Stall Information Collector",
			logger,
			config,
			capabilities,
		),
		cpuPath:    filepath.Join(pressureDir, "cpu"),
		memoryPath: filepath.Join(pressureDir, "memory"),
		ioPath:     filepath.Join(pressureDir, "io"),
	}, nil
}

func (c *PSICollector) Collect(ctx context.Context) (performance.Event, error) {
	stats, err := c.collectPSIStats()
	if err != nil {
		return performance.Event{}, err
	}
	return performance.Event{Metric: performance.MetricTypePSI, Data: stats}, nil
}

func (c *PSICollector) collectPSIStats() (*performance.PSIStats, error) {
	stats := &performance.PSIStats{}

	cpu, err := c.readPSIFile(c.cpuPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CPU pressure: %w", err)
	}
	stats.CPU = cpu

	memory, err := c.readPSIFile(c.memoryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read memory pressure: %w", err)
	}
	stats.Memory = memory

	io, err := c.readPSIFile(c.ioPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read I/O pressure: %w", err)
	}
	stats.IO = io

	return stats, nil
}

func (c *PSICollector) readPSIFile(path string) (*performance.PSIResourceStats, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	return ParsePSIData(string(data))
}

// ParsePSIData parses PSI file content into PSIResourceStats
// Format: two lines with "some" and "full" prefix, followed by key=value pairs
// Used by both system-level and cgroup PSI collectors
func ParsePSIData(data string) (*performance.PSIResourceStats, error) {
	stats := &performance.PSIResourceStats{}
	trimmedData := strings.TrimSpace(data)

	// Empty content is valid - return zero values for graceful degradation
	if trimmedData == "" {
		return stats, nil
	}

	lines := strings.Split(trimmedData, "\n")
	validLinesFound := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "some ") {
			if err := parsePSILine(strings.TrimPrefix(line, "some "), &stats.SomeAvg10, &stats.SomeAvg60, &stats.SomeAvg300, &stats.SomeTotal); err != nil {
				return nil, fmt.Errorf("failed to parse 'some' line: %w", err)
			}
			validLinesFound = true
		} else if strings.HasPrefix(line, "full ") {
			if err := parsePSILine(strings.TrimPrefix(line, "full "), &stats.FullAvg10, &stats.FullAvg60, &stats.FullAvg300, &stats.FullTotal); err != nil {
				return nil, fmt.Errorf("failed to parse 'full' line: %w", err)
			}
			validLinesFound = true
		}
	}

	// Non-empty content with no valid PSI lines is an error (invalid/malformed file)
	if !validLinesFound {
		return nil, fmt.Errorf("no valid PSI data found")
	}

	return stats, nil
}

func parsePSILine(line string, avg10, avg60, avg300 *float64, total *uint64) error {
	fields := strings.Fields(line)

	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key, value := parts[0], parts[1]

		switch key {
		case "avg10":
			f, err := parsePSIPercentage(value)
			if err != nil {
				return fmt.Errorf("failed to parse avg10: %w", err)
			}
			*avg10 = f
		case "avg60":
			f, err := parsePSIPercentage(value)
			if err != nil {
				return fmt.Errorf("failed to parse avg60: %w", err)
			}
			*avg60 = f
		case "avg300":
			f, err := parsePSIPercentage(value)
			if err != nil {
				return fmt.Errorf("failed to parse avg300: %w", err)
			}
			*avg300 = f
		case "total":
			t, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse total: %w", err)
			}
			*total = t
		}
	}

	return nil
}

// parsePSIPercentage parses a PSI percentage value and validates it's in range [0, 100]
func parsePSIPercentage(value string) (float64, error) {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	if f < 0 || f > 100 {
		return 0, fmt.Errorf("value %.2f out of valid range [0, 100]", f)
	}
	return f, nil
}
