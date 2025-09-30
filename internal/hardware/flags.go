// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt
package hardware

import (
	"flag"
	"time"
)

var (
	enable                bool
	defaultUpdateInterval time.Duration
)

func init() {
	flag.BoolVar(&enable, "enable-hardware-discovery", true,
		"Enable hardware topology discovery")
	flag.DurationVar(&defaultUpdateInterval, "hardware-update-interval", 5*time.Minute,
		"Interval for hardware topology discovery updates")
}

func Enabled() bool { return enable }
