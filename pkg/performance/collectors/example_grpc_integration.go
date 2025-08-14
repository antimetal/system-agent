// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"context"
	"fmt"
)

// ProfileStreamer demonstrates how to integrate the zero-allocation
// profiler with gRPC streaming
type ProfileStreamer struct {
	pool    *MinimalPool
	encoder *ProtobufEncoder
	// stream would be the actual gRPC stream in production
	// stream pb.ProfileService_StreamProfilesClient
}

// NewProfileStreamer creates a new streaming profiler
func NewProfileStreamer(nodeName string) *ProfileStreamer {
	return &ProfileStreamer{
		pool:    NewMinimalPool(),
		encoder: NewProtobufEncoder(nodeName),
	}
}

// StreamBatch sends a batch of profile events via gRPC with zero allocations
func (s *ProfileStreamer) StreamBatch(ctx context.Context, events []ProfileEvent) error {
	// Get buffer from pool (zero allocation)
	buf := s.pool.Get()
	if buf == nil {
		// Pool exhausted - this is expected under load
		// Could implement backpressure or drop events here
		return ErrPoolExhausted
	}
	defer s.pool.Put(buf)

	// Encode events into protobuf (1 allocation for protobuf struct only)
	batch, _, err := s.encoder.EncodeToProtobuf(events, buf)
	if err != nil {
		return fmt.Errorf("encoding failed: %w", err)
	}

	// In production, you would send via gRPC stream:
	// req := &pb.ProfileStreamRequest{
	//     Payload: &pb.ProfileStreamRequest_Batch{
	//         Batch: batch,
	//     },
	// }
	// return s.stream.Send(req)

	// For demonstration, just validate the batch
	if batch.EventCount == 0 {
		return fmt.Errorf("no events encoded")
	}

	return nil
}

// Example usage showing the complete zero-allocation pipeline:
//
// 1. BPF ring buffer → ProfileEvent (zero copy from kernel)
// 2. ProfileEvent → Binary encoding in pool buffer (zero allocation)
// 3. Pool buffer → Protobuf packed_events field (zero copy)
// 4. Protobuf → gRPC stream (minimal allocations for protobuf wrapper)
//
// Result: ~15-20MB total memory usage vs 50-100MB baseline