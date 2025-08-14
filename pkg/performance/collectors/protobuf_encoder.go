// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt


package collectors

import (
	"encoding/binary"
	"runtime"
	"time"

	pb "github.com/antimetal/agent/pkg/performance/proto"
)

// ProtobufEncoder wraps binary encoding for protobuf compatibility
type ProtobufEncoder struct {
	encoder  *BinaryEncoder
	nodeName string
}

// NewProtobufEncoder creates a new protobuf-compatible encoder
func NewProtobufEncoder(nodeName string) *ProtobufEncoder {
	return &ProtobufEncoder{
		encoder:  NewBinaryEncoder(),
		nodeName: nodeName,
	}
}

// EncodeToProtobuf encodes events into a protobuf message with zero allocations
// The poolBuf is used directly as the packed_events field to avoid copies
func (p *ProtobufEncoder) EncodeToProtobuf(events []ProfileEvent, poolBuf []byte) (*pb.ProfileBatch, int, error) {
	// Use existing zero-allocation binary encoder
	bytesWritten, err := p.encoder.EncodeEvents(events, poolBuf)
	if err != nil {
		return nil, 0, err
	}

	// Create protobuf message
	// Note: poolBuf[:bytesWritten] creates a slice header, not a copy
	batch := &pb.ProfileBatch{
		TimestampNs:   uint64(time.Now().UnixNano()),
		CpuCount:      uint32(runtime.NumCPU()),
		PackedEvents:  poolBuf[:bytesWritten], // Zero-copy reference
		EventCount:    uint32(len(events)),
		FormatVersion: ProfileVersion,
		NodeName:      p.nodeName,
	}

	// Calculate actual events encoded (may be less than requested due to buffer size)
	actualEvents := (bytesWritten - ProfileHeaderSize) / ProfileEventSize
	if actualEvents < len(events) {
		batch.DroppedEvents = uint32(len(events) - actualEvents)
	}
	batch.EventCount = uint32(actualEvents)

	return batch, bytesWritten, nil
}

// DecodePackedEvents decodes events from a protobuf ProfileBatch
func DecodePackedEvents(batch *pb.ProfileBatch) ([]ProfileEvent, error) {
	if batch.FormatVersion != ProfileVersion {
		return nil, ErrUnsupportedVersion
	}

	// Pre-allocate slice with exact capacity
	events := make([]ProfileEvent, 0, batch.EventCount)
	buf := batch.PackedEvents

	// Skip our internal header if present
	offset := 0
	if len(buf) >= ProfileHeaderSize && string(buf[0:4]) == ProfileMagic {
		// Verify header
		numEvents, err := DecodeHeader(buf)
		if err != nil {
			return nil, err
		}
		if uint32(numEvents) != batch.EventCount {
			return nil, ErrEventCountMismatch
		}
		offset = ProfileHeaderSize
	}

	// Decode each event
	for i := uint32(0); i < batch.EventCount; i++ {
		eventOffset := offset + int(i*ProfileEventSize)
		if eventOffset+ProfileEventSize > len(buf) {
			break
		}

		event := ProfileEvent{
			Timestamp:     binary.LittleEndian.Uint64(buf[eventOffset:]),
			PID:           int32(binary.LittleEndian.Uint32(buf[eventOffset+8:])),
			TID:           int32(binary.LittleEndian.Uint32(buf[eventOffset+12:])),
			UserStackID:   int32(binary.LittleEndian.Uint32(buf[eventOffset+16:])),
			KernelStackID: int32(binary.LittleEndian.Uint32(buf[eventOffset+20:])),
			CPU:           binary.LittleEndian.Uint32(buf[eventOffset+24:]),
			Flags:         binary.LittleEndian.Uint32(buf[eventOffset+28:]),
		}
		events = append(events, event)
	}

	return events, nil
}

// EncodeDirect encodes events directly into protobuf without intermediate buffer
// This is an alternative approach that modifies the protobuf message in place
func (p *ProtobufEncoder) EncodeDirect(events []ProfileEvent) *pb.ProfileBatch {
	// Calculate required size
	requiredSize := ProfileHeaderSize + (len(events) * ProfileEventSize)
	if requiredSize > BufferSize {
		requiredSize = BufferSize
	}

	// Create protobuf with pre-allocated packed_events
	batch := &pb.ProfileBatch{
		TimestampNs:   uint64(time.Now().UnixNano()),
		CpuCount:      uint32(runtime.NumCPU()),
		PackedEvents:  make([]byte, requiredSize),
		FormatVersion: ProfileVersion,
		NodeName:      p.nodeName,
	}

	// Encode directly into the protobuf's packed_events field
	numEvents := len(events)
	if numEvents > MaxEventsPerBatch {
		numEvents = MaxEventsPerBatch
		batch.DroppedEvents = uint32(len(events) - numEvents)
	}

	// Write header
	offset := p.encoder.writeHeader(batch.PackedEvents, numEvents)

	// Write events
	for i := 0; i < numEvents; i++ {
		offset += p.encoder.writeEvent(batch.PackedEvents, offset, &events[i])
	}

	// Trim to actual size
	batch.PackedEvents = batch.PackedEvents[:offset]
	batch.EventCount = uint32(numEvents)

	return batch
}