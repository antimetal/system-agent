// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt
package endpoints

import (
	"crypto/tls"
	"flag"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

var (
	defaultMaxStreamAge time.Duration
)

func init() {
	flag.DurationVar(&defaultMaxStreamAge, "max-stream-age", 10*time.Minute,
		"Maximum age of the gRPC stream before it is reset")
}

func MaxStreamAge() time.Duration { return defaultMaxStreamAge }

type options struct {
	addr      string
	secure    bool
	keepalive time.Duration
}

type Option func(*options)

func Addr(a string) Option {
	return func(o *options) {
		o.addr = a
	}
}

func Secure(s bool) Option {
	return func(o *options) {
		o.secure = s
	}
}

func Keepalive(k time.Duration) Option {
	return func(o *options) {
		o.keepalive = k
	}
}

func createGrpcConn(opts *options) (*grpc.ClientConn, error) {
	var creds credentials.TransportCredentials
	if opts.secure {
		creds = credentials.NewTLS(&tls.Config{})
	} else {
		creds = insecure.NewCredentials()
	}

	return grpc.NewClient(opts.addr,
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time: opts.keepalive,
		}),
	)
}
