// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt
package containers

import (
	"flag"
	"time"
)

var (
	enable                bool
	defaultUpdateInterval time.Duration
)

func init() {
	flag.BoolVar(&enable, "enable-container-discovery", true,
		"Enable container and process discovery")
	flag.DurationVar(&defaultUpdateInterval, "containers-update-interval", 30*time.Second,
		"Interval for container and process discovery updates")
}

func Enabled() bool { return enable }
