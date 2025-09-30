// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt
package endpoints

import (
	"flag"
	"time"

	"google.golang.org/grpc"
)

var (
	defaultAMSAddr   string
	defaultAMSSecure bool
)

func init() {
	flag.StringVar(&defaultAMSAddr, "config-ams-addr", "agent.api.antimetal.com:443",
		"AMS service address for configuration (used with ams loader)")
	flag.BoolVar(&defaultAMSSecure, "config-ams-secure", true,
		"Use secure connection to the AMS service")
}

func AMS(opts ...Option) (*grpc.ClientConn, error) {
	cfg := &options{
		addr:      defaultAMSAddr,
		secure:    defaultAMSSecure,
		keepalive: 5 * time.Minute,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return createGrpcConn(cfg)
}
