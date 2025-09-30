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
	defaultIntakeAddr   string
	defaultIntakeSecure bool
)

func init() {
	flag.StringVar(&defaultIntakeAddr, "intake-address", "intake.antimetal.com:443",
		"The address of the intake service",
	)
	flag.BoolVar(&defaultIntakeSecure, "intake-secure", true,
		"Use secure connection to the Antimetal intake service",
	)
}

func Intake(opts ...Option) (*grpc.ClientConn, error) {
	cfg := &options{
		addr:      defaultIntakeAddr,
		secure:    defaultIntakeSecure,
		keepalive: 5 * time.Minute,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return createGrpcConn(cfg)
}
