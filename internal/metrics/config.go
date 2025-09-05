// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package metrics

import (
	"flag"
	"time"
)

// Command-line flag variables (populated by init())
var (
	flagEnabled         *bool
	flagPublishInterval *time.Duration
)

func init() {
	// Define metrics pipeline flags that will be parsed in main()
	flagEnabled = flag.Bool("enable-metrics", false, "Enable metrics pipeline for external monitoring systems")
	flagPublishInterval = flag.Duration("metrics-publish-interval", 30*time.Second, "Interval for publishing performance metrics")
}

// Config configures the metrics pipeline
type Config struct {
	// Enabled determines if the metrics pipeline should be active
	Enabled bool

	// Bus configuration
	Bus BusConfig

	// Performance integration
	Performance PerformanceConfig
}

// PerformanceConfig configures performance metrics integration
type PerformanceConfig struct {
	// Enabled determines if performance metrics should be published
	Enabled bool

	// PublishInterval is how often to publish performance snapshots
	PublishInterval time.Duration

	// Source identifier for performance events
	Source string

	// Tags to add to all performance events
	Tags map[string]string
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() Config {
	return Config{
		Enabled: false, // Disabled by default
		Bus:     DefaultBusConfig(),
		Performance: PerformanceConfig{
			Enabled:         false,
			PublishInterval: 30 * time.Second,
			Source:          "performance-collector",
			Tags:            make(map[string]string),
		},
	}
}

// ApplyDefaults fills in zero values with defaults
func (c *Config) ApplyDefaults() {
	defaults := DefaultConfig()

	if c.Bus.BufferSize == 0 {
		c.Bus = defaults.Bus
	}

	if c.Performance.PublishInterval == 0 {
		c.Performance.PublishInterval = defaults.Performance.PublishInterval
	}

	if c.Performance.Source == "" {
		c.Performance.Source = defaults.Performance.Source
	}

	if c.Performance.Tags == nil {
		c.Performance.Tags = make(map[string]string)
	}
}

// Validate ensures the configuration is valid
func (c *Config) Validate() error {
	// Add validation logic as needed
	return nil
}

// GetConfigFromFlags builds a Config from the package's command-line flags
func GetConfigFromFlags() Config {
	config := DefaultConfig()
	config.Enabled = *flagEnabled
	config.Performance.Enabled = *flagEnabled
	config.Performance.PublishInterval = *flagPublishInterval
	config.Performance.Source = "performance-collector"
	config.ApplyDefaults()
	return config
}

// IsEnabled returns whether metrics pipeline is enabled via flags
func IsEnabled() bool {
	return flagEnabled != nil && *flagEnabled
}
