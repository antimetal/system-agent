// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt
package manager

import "flag"

var (
	enable bool
)

func init() {
	flag.BoolVar(&enable, "enable-performance-collectors", false,
		"Enable continuous performance collectors for testing (CPU, memory, disk, network, process)")
}

func Enabled() bool { return enable }
