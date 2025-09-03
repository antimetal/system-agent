// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package mock

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type GRPCService struct {
	Descriptor *grpc.ServiceDesc
	Impl       any
}

// NewGRPCServer creates a mock gRPC server with the given services and returns a connection to it
func NewGRPCServer(t *testing.T, svcs ...GRPCService) (*grpc.ClientConn, func()) {
	server := grpc.NewServer()
	for _, svc := range svcs {
		server.RegisterService(svc.Descriptor, svc.Impl)
	}

	lis := bufconn.Listen(1024 * 1024)
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Logf("Server exited with error: %v", err)
		}
	}()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	cleanup := func() {
		conn.Close()
		server.Stop()
	}

	return conn, cleanup
}
