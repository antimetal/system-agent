// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package datadog

import (
	"flag"
	"os"
	"time"
)

// Command-line flag variables (populated by init())
var (
	flagEnabled *bool
	flagURL     *string
	flagService *string
	flagEnv     *string
	flagVersion *string
)

func init() {
	// Define Datadog consumer flags that will be parsed in main()
	flagEnabled = flag.Bool("enable-profiling-datadog", false, "Enable Datadog profiling consumer")
	flagURL = flag.String("profiling-datadog-url", "http://localhost:8126/profiling/v1/input", "Datadog Agent profiling intake endpoint")
	flagService = flag.String("profiling-datadog-service", "antimetal-agent", "Service name for profile tagging")
	flagEnv = flag.String("profiling-datadog-env", "production", "Environment name for profile tagging")
	flagVersion = flag.String("profiling-datadog-version", "", "Service version for profile tagging (defaults to agent version)")
}

// IsEnabled returns whether Datadog consumer is enabled via flags
func IsEnabled() bool {
	return flagEnabled != nil && *flagEnabled
}

// GetConfigFromFlags builds a Config from the package's command-line flags
func GetConfigFromFlags() Config {
	config := DefaultConfig()

	if flagURL != nil && *flagURL != "" {
		config.AgentURL = *flagURL
	}
	if flagService != nil && *flagService != "" {
		config.Service = *flagService
	}
	if flagEnv != nil && *flagEnv != "" {
		config.Env = *flagEnv
	}
	if flagVersion != nil && *flagVersion != "" {
		config.Version = *flagVersion
	}

	// Set hostname from system
	if hostname, err := os.Hostname(); err == nil {
		config.Hostname = hostname
	}

	// Use sensible defaults
	config.UploadInterval = 60 * time.Second
	config.MaxQueueSize = 100
	config.Tags = make(map[string]string)

	return config
}
