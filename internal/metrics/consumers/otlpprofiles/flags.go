// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otlpprofiles

import (
	"flag"
	"time"

	"github.com/antimetal/agent/internal/intake"
)

// Command-line flag variables (populated by init())
var (
	flagEnabled         *bool
	flagExportTimeout   *time.Duration
	flagExportInterval  *time.Duration
	flagMaxQueueSize    *int
	flagExportBatchSize *int
)

func init() {
	// Define OTLP profiles consumer flags that will be parsed in main()
	flagEnabled = flag.Bool("otlp-profiles-enabled", false,
		"Enable OTLP profiles export to Overlook intake service via gRPC")

	flagExportTimeout = flag.Duration("otlp-profiles-export-timeout", 30*time.Second,
		"gRPC request timeout for profile export")

	flagExportInterval = flag.Duration("otlp-profiles-export-interval", 10*time.Second,
		"How often to export buffered profiles")

	flagMaxQueueSize = flag.Int("otlp-profiles-max-queue-size", 1000,
		"Maximum number of profiles to buffer before dropping")

	flagExportBatchSize = flag.Int("otlp-profiles-export-batch-size", 100,
		"Number of profiles to export in one batch")
}

// Enabled returns whether OTLP profiles export is enabled
func Enabled() bool {
	return flagEnabled != nil && *flagEnabled
}

// GetConfig returns a Config populated from command-line flags
// Note: Uses --intake-api-key for authentication (shared with resource intake)
func GetConfig(serviceName, serviceVersion string) Config {
	return Config{
		AuthToken:       intake.APIKey(), // Reuse the same API key as intake worker
		ExportTimeout:   *flagExportTimeout,
		ExportInterval:  *flagExportInterval,
		MaxQueueSize:    *flagMaxQueueSize,
		ExportBatchSize: *flagExportBatchSize,
		ServiceName:     serviceName,
		ServiceVersion:  serviceVersion,
	}
}
