// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt
package config

import (
	"flag"
	"fmt"

	"github.com/go-logr/logr"

	"github.com/antimetal/agent/internal/endpoints"
)

var (
	defaultLoader    string
	defaultFSPath    string
	defaultAMSAPIKey string
)

func init() {
	flag.StringVar(&defaultLoader, "config-loader", "fs",
		"Config loader type: 'fs' for filesystem loader, 'ams' for AMS gRPC loader")
	flag.StringVar(&defaultFSPath, "config-fs-path", "/etc/antimetal/agent",
		"Path to configuration directory (used with fs loader)")
	flag.StringVar(&defaultAMSAPIKey, "config-ams-api-key", "",
		"API key for AMS service authentication")
}

func getDefaultLoader(logger logr.Logger) (Loader, error) {
	switch defaultLoader {
	case "fs":
		return NewFSLoader(defaultFSPath, logger.WithName("config.fs"))
	case "ams":
		amsConn, err := endpoints.AMS()
		if err != nil {
			return nil, fmt.Errorf("unable to connect to AMS service: %w", err)
		}
		amsOpts := []AMSLoaderOpts{
			WithAMSLogger(logger.WithName("config.ams")),
			WithMaxStreamAge(endpoints.MaxStreamAge()),
			WithAMSAPIKey(defaultAMSAPIKey),
		}
		return NewAMSLoader(amsConn, amsOpts...)
	default:
		return nil, fmt.Errorf("unknown config loader: %s", defaultLoader)
	}
}
