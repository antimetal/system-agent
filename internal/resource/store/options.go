// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt
package store

import (
	"flag"

	"github.com/go-logr/logr"
)

var (
	defaultDataDir string
)

func init() {
	flag.StringVar(&defaultDataDir, "data-directory", "/var/lib/antimetal",
		"The directory where the agent will place its persistent data files. Set to empty string for in-memory mode.")
}

type Option func(*options)

type options struct {
	dataDir string
	logger  logr.Logger
}

func WithDataDir(dir string) Option {
	return func(o *options) {
		o.dataDir = dir
	}
}

func WithLogger(logger logr.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}
