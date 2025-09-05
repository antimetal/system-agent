// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package debug

import (
	"flag"
	"fmt"
)

// LogLevel determines the verbosity of debug output
type LogLevel int

const (
	LogLevelBasic   LogLevel = 0 // basic event info only
	LogLevelDetails LogLevel = 1 // include metric type and source details
	LogLevelVerbose LogLevel = 2 // include full event data and attributes
)

// Command-line flag variables (populated by init())
var (
	flagEnabled   *bool
	flagLogLevel  *string
	flagLogFormat *string
)

func init() {
	// Define debug consumer flags that will be parsed in main()
	flagEnabled = flag.Bool("enable-debug-consumer", false, "Enable debug consumer for logging metrics events")
	flagLogLevel = flag.String("debug-log-level", "basic", "Debug consumer log level: basic, details, or verbose")
	flagLogFormat = flag.String("debug-log-format", "json", "Debug consumer log format: json or text")
}

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case LogLevelBasic:
		return "basic"
	case LogLevelDetails:
		return "details"
	case LogLevelVerbose:
		return "verbose"
	default:
		return fmt.Sprintf("unknown(%d)", int(l))
	}
}

// LogFormat determines the output format
type LogFormat string

const (
	LogFormatJSON LogFormat = "json" // structured JSON output
	LogFormatText LogFormat = "text" // human-readable text format
)

// String returns the string representation of the log format
func (f LogFormat) String() string {
	return string(f)
}

// IsValid checks if the log format is valid
func (f LogFormat) IsValid() bool {
	return f == LogFormatJSON || f == LogFormatText
}

type Config struct {
	// LogLevel determines the verbosity of debug output
	LogLevel LogLevel

	// LogFormat determines the output format
	LogFormat LogFormat

	IncludeTimestamp bool
	IncludeEventData bool
	MaxDataLength    int

	// MetricTypeFilter only logs events matching these metric types (empty = all)
	MetricTypeFilter []string

	// SourceFilter only logs events from these sources (empty = all)
	SourceFilter []string
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() Config {
	return Config{
		LogLevel:         LogLevelDetails, // Include basic details
		LogFormat:        LogFormatText,
		IncludeTimestamp: true,
		IncludeEventData: false,
		MaxDataLength:    1000,
		MetricTypeFilter: []string{},
		SourceFilter:     []string{},
	}
}

// Validate ensures the configuration is valid
func (c *Config) Validate() error {
	if c.LogLevel < LogLevelBasic || c.LogLevel > LogLevelVerbose {
		return ErrInvalidLogLevel
	}

	if !c.LogFormat.IsValid() {
		return ErrInvalidLogFormat
	}

	if c.MaxDataLength < 0 {
		c.MaxDataLength = 0
	}

	return nil
}

func (c *Config) ShouldLogMetricType(metricType string) bool {
	if len(c.MetricTypeFilter) == 0 {
		return true
	}

	for _, filter := range c.MetricTypeFilter {
		if filter == metricType {
			return true
		}
	}
	return false
}

func (c *Config) ShouldLogSource(source string) bool {
	if len(c.SourceFilter) == 0 {
		return true
	}

	for _, filter := range c.SourceFilter {
		if filter == source {
			return true
		}
	}
	return false
}

// GetConfigFromFlags builds a Config from the package's command-line flags
func GetConfigFromFlags() Config {
	// Parse log level
	var level LogLevel
	switch *flagLogLevel {
	case "verbose":
		level = LogLevelVerbose
	case "details":
		level = LogLevelDetails
	default:
		level = LogLevelBasic
	}

	// Parse log format
	var format LogFormat
	if *flagLogFormat == "text" {
		format = LogFormatText
	} else {
		format = LogFormatJSON
	}

	return Config{
		LogLevel:         level,
		LogFormat:        format,
		IncludeTimestamp: true,
		IncludeEventData: level >= LogLevelDetails,
		MaxDataLength:    1024,
	}
}

// IsEnabled returns whether debug consumer is enabled via flags
func IsEnabled() bool {
	return flagEnabled != nil && *flagEnabled
}

// Common errors
var (
	ErrInvalidLogLevel  = fmt.Errorf("log level must be basic (%d), details (%d), or verbose (%d)", LogLevelBasic, LogLevelDetails, LogLevelVerbose)
	ErrInvalidLogFormat = fmt.Errorf("log format must be '%s' or '%s'", LogFormatJSON, LogFormatText)
)
