// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package perfdata

import "flag"

// Command-line flag variables (populated by init())
var (
	flagEnabled *bool
	flagPath    *string
)

func init() {
	// Define perfdata consumer flags that will be parsed in main()
	flagEnabled = flag.Bool("enable-profiling-perfdata", false, "Enable perf.data file consumer for profiling output")
	flagPath = flag.String("profiling-perfdata-path", "/var/lib/antimetal/profiles", "Output directory for perf.data files")
}

// IsEnabled returns whether perfdata consumer is enabled via flags
func IsEnabled() bool {
	return flagEnabled != nil && *flagEnabled
}

// GetConfigFromFlags builds a Config from the package's command-line flags
func GetConfigFromFlags() Config {
	config := DefaultConfig()
	if flagPath != nil && *flagPath != "" {
		config.OutputPath = *flagPath
	}
	return config
}
