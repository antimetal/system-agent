// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt


package collectors

import (
	"testing"

	pb "github.com/antimetal/agent/pkg/performance/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtobufEncoder_ZeroAllocations(t *testing.T) {
	encoder := NewProtobufEncoder("test-node")
	events := make([]ProfileEvent, 10)
	poolBuf := make([]byte, BufferSize)

	// Fill with test data
	for i := range events {
		events[i] = ProfileEvent{
			Timestamp:     uint64(i * 1000),
			PID:           int32(i + 100),
			TID:           int32(i + 200),
			UserStackID:   int32(i),
			KernelStackID: int32(i + 10),
			CPU:           uint32(i % 4),
			Flags:         0,
		}
	}

	// Warm up
	encoder.EncodeToProtobuf(events, poolBuf)

	// Measure allocations for EncodeToProtobuf
	// Note: Creating the protobuf message itself will allocate,
	// but the packed_events field should reference poolBuf without copying
	allocs := testing.AllocsPerRun(100, func() {
		encoder.EncodeToProtobuf(events, poolBuf)
	})

	// We expect some allocations for the protobuf message structure itself,
	// but the key is that packed_events doesn't copy the buffer
	// Typically ~3-4 allocations for the pb.ProfileBatch struct and metadata
	assert.Less(t, allocs, float64(10), "Should have minimal allocations (only for protobuf structure)")
}

func TestProtobufEncoder_RoundTrip(t *testing.T) {
	encoder := NewProtobufEncoder("test-node")
	poolBuf := make([]byte, BufferSize)

	// Create test events
	originalEvents := []ProfileEvent{
		{
			Timestamp:     123456789,
			PID:           1234,
			TID:           5678,
			UserStackID:   42,
			KernelStackID: 84,
			CPU:           3,
			Flags:         ProfileFlagUserStackTruncated,
		},
		{
			Timestamp:     987654321,
			PID:           9876,
			TID:           5432,
			UserStackID:   99,
			KernelStackID: 88,
			CPU:           7,
			Flags:         ProfileFlagKernelStackTruncated,
		},
	}

	// Encode to protobuf
	batch, bytesWritten, err := encoder.EncodeToProtobuf(originalEvents, poolBuf)
	require.NoError(t, err)
	assert.NotNil(t, batch)
	assert.Greater(t, bytesWritten, 0)

	// Verify protobuf fields
	assert.Equal(t, uint32(ProfileVersion), batch.FormatVersion)
	assert.Equal(t, uint32(len(originalEvents)), batch.EventCount)
	assert.Equal(t, "test-node", batch.NodeName)
	assert.Equal(t, uint32(0), batch.DroppedEvents)
	assert.Len(t, batch.PackedEvents, bytesWritten)

	// Decode from protobuf
	decodedEvents, err := DecodePackedEvents(batch)
	require.NoError(t, err)
	assert.Equal(t, originalEvents, decodedEvents)
}

func TestProtobufEncoder_DirectEncoding(t *testing.T) {
	encoder := NewProtobufEncoder("test-node")

	events := make([]ProfileEvent, 5)
	for i := range events {
		events[i] = ProfileEvent{
			Timestamp:     uint64(i * 1000),
			PID:           int32(i + 100),
			TID:           int32(i + 200),
			UserStackID:   int32(i),
			KernelStackID: int32(i + 10),
			CPU:           uint32(i % 4),
			Flags:         0,
		}
	}

	// Use direct encoding
	batch := encoder.EncodeDirect(events)
	require.NotNil(t, batch)

	// Verify batch
	assert.Equal(t, uint32(len(events)), batch.EventCount)
	assert.Equal(t, uint32(0), batch.DroppedEvents)

	// Decode and verify
	decodedEvents, err := DecodePackedEvents(batch)
	require.NoError(t, err)
	assert.Equal(t, events, decodedEvents)
}

func TestProtobufEncoder_Overflow(t *testing.T) {
	encoder := NewProtobufEncoder("test-node")
	poolBuf := make([]byte, BufferSize)

	// Create more events than can fit
	events := make([]ProfileEvent, MaxEventsPerBatch+10)
	for i := range events {
		events[i] = ProfileEvent{
			Timestamp: uint64(i),
			PID:       int32(i),
		}
	}

	// Encode
	batch, _, err := encoder.EncodeToProtobuf(events, poolBuf)
	require.NoError(t, err)

	// Should have truncated to MaxEventsPerBatch
	assert.Equal(t, uint32(MaxEventsPerBatch), batch.EventCount)
	assert.Equal(t, uint32(10), batch.DroppedEvents)

	// Decode and verify we get MaxEventsPerBatch events
	decodedEvents, err := DecodePackedEvents(batch)
	require.NoError(t, err)
	assert.Len(t, decodedEvents, MaxEventsPerBatch)
}

func TestDecodePackedEvents_VersionCheck(t *testing.T) {
	batch := &pb.ProfileBatch{
		FormatVersion: 999, // Unsupported version
		PackedEvents:  []byte{},
		EventCount:    0,
	}

	_, err := DecodePackedEvents(batch)
	assert.Equal(t, ErrUnsupportedVersion, err)
}

func TestProtobufEncoder_BufferSharing(t *testing.T) {
	encoder := NewProtobufEncoder("test-node")
	poolBuf := make([]byte, BufferSize)

	events := []ProfileEvent{{
		Timestamp: 12345,
		PID:       100,
		TID:       200,
	}}

	// Encode
	batch, _, err := encoder.EncodeToProtobuf(events, poolBuf)
	require.NoError(t, err)

	// Verify that packed_events shares the same underlying array as poolBuf
	// This is the key to zero-copy behavior
	if len(batch.PackedEvents) > 0 {
		// Modify poolBuf
		poolBuf[ProfileHeaderSize] = 0xFF

		// Should affect packed_events since they share memory
		assert.Equal(t, byte(0xFF), batch.PackedEvents[ProfileHeaderSize])
	}
}

func TestProtobufEncoder_Metadata(t *testing.T) {
	encoder := NewProtobufEncoder("test-node")
	poolBuf := make([]byte, BufferSize)

	events := []ProfileEvent{{
		Timestamp: 12345,
		PID:       100,
	}}

	batch, _, err := encoder.EncodeToProtobuf(events, poolBuf)
	require.NoError(t, err)

	// Can add metadata after creation
	batch.Metadata = map[string]string{
		"version": "1.0.0",
		"kernel":  "5.15.0",
	}

	// Metadata doesn't affect decoding
	decodedEvents, err := DecodePackedEvents(batch)
	require.NoError(t, err)
	assert.Len(t, decodedEvents, 1)
}

func BenchmarkProtobufEncoder_EncodeToProtobuf(b *testing.B) {
	encoder := NewProtobufEncoder("bench-node")
	events := make([]ProfileEvent, 10)
	poolBuf := make([]byte, BufferSize)

	for i := range events {
		events[i] = ProfileEvent{
			Timestamp:     uint64(i * 1000),
			PID:           int32(i + 100),
			TID:           int32(i + 200),
			UserStackID:   int32(i),
			KernelStackID: int32(i + 10),
			CPU:           uint32(i % 4),
			Flags:         0,
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		encoder.EncodeToProtobuf(events, poolBuf)
	}

	b.ReportMetric(float64(len(events)*b.N)/b.Elapsed().Seconds(), "events/sec")
}

func BenchmarkProtobufEncoder_EncodeDirect(b *testing.B) {
	encoder := NewProtobufEncoder("bench-node")
	events := make([]ProfileEvent, 10)

	for i := range events {
		events[i] = ProfileEvent{
			Timestamp:     uint64(i * 1000),
			PID:           int32(i + 100),
			TID:           int32(i + 200),
			UserStackID:   int32(i),
			KernelStackID: int32(i + 10),
			CPU:           uint32(i % 4),
			Flags:         0,
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		encoder.EncodeDirect(events)
	}

	b.ReportMetric(float64(len(events)*b.N)/b.Elapsed().Seconds(), "events/sec")
}