// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt


package collectors

import (
	"encoding/binary"
)

// Wire format constants
const (
	// Magic bytes for profile batch
	ProfileMagic = "PROF"
	// Current wire format version
	ProfileVersion = 1
	// Header size in bytes
	ProfileHeaderSize = 16
	// Event size in bytes
	ProfileEventSize = 32
	// Maximum events per batch (1024 - 16) / 32 = 31
	MaxEventsPerBatch = (BufferSize - ProfileHeaderSize) / ProfileEventSize
)

// BinaryEncoder handles low-level binary encoding of profile events
// This is used internally by ProtobufEncoder for efficient packing
type BinaryEncoder struct {
	// No heap fields - all work done on stack
}

// NewBinaryEncoder creates a new binary encoder
func NewBinaryEncoder() *BinaryEncoder {
	return &BinaryEncoder{}
}

// EncodeEvents encodes events directly into the provided buffer
// Returns the number of bytes written
func (e *BinaryEncoder) EncodeEvents(events []ProfileEvent, buf []byte) (int, error) {
	if len(buf) < ProfileHeaderSize {
		return 0, ErrBufferTooSmall
	}

	// Limit events to what fits in buffer
	numEvents := len(events)
	maxEvents := (len(buf) - ProfileHeaderSize) / ProfileEventSize
	if numEvents > maxEvents {
		numEvents = maxEvents
	}

	// Write header
	offset := e.writeHeader(buf, numEvents)

	// Pack events into buffer
	for i := 0; i < numEvents; i++ {
		if offset+ProfileEventSize > len(buf) {
			break
		}
		offset += e.writeEvent(buf, offset, &events[i])
	}

	return offset, nil
}

// writeHeader writes the batch header
func (e *BinaryEncoder) writeHeader(buf []byte, count int) int {
	// Magic bytes (4 bytes)
	copy(buf[0:4], ProfileMagic)

	// Version (2 bytes)
	binary.LittleEndian.PutUint16(buf[4:6], ProfileVersion)

	// Flags (2 bytes) - reserved for future use
	binary.LittleEndian.PutUint16(buf[6:8], 0)

	// Total size in bytes (4 bytes)
	totalSize := ProfileHeaderSize + (count * ProfileEventSize)
	binary.LittleEndian.PutUint32(buf[8:12], uint32(totalSize))

	// Number of events (4 bytes)
	binary.LittleEndian.PutUint32(buf[12:16], uint32(count))

	return ProfileHeaderSize
}

// writeEvent writes a single event at the specified offset
func (e *BinaryEncoder) writeEvent(buf []byte, offset int, event *ProfileEvent) int {
	// Direct binary serialization - no allocations
	binary.LittleEndian.PutUint64(buf[offset:], event.Timestamp)
	binary.LittleEndian.PutUint32(buf[offset+8:], uint32(event.PID))
	binary.LittleEndian.PutUint32(buf[offset+12:], uint32(event.TID))
	binary.LittleEndian.PutUint32(buf[offset+16:], uint32(event.UserStackID))
	binary.LittleEndian.PutUint32(buf[offset+20:], uint32(event.KernelStackID))
	binary.LittleEndian.PutUint32(buf[offset+24:], event.CPU)
	binary.LittleEndian.PutUint32(buf[offset+28:], event.Flags)
	return ProfileEventSize
}

// DecodeHeader decodes the batch header from a buffer
func DecodeHeader(buf []byte) (numEvents int, err error) {
	if len(buf) < ProfileHeaderSize {
		return 0, ErrBufferTooSmall
	}

	// Check magic
	if string(buf[0:4]) != ProfileMagic {
		return 0, ErrInvalidMagic
	}

	// Check version
	version := binary.LittleEndian.Uint16(buf[4:6])
	if version != ProfileVersion {
		return 0, ErrUnsupportedVersion
	}

	// Get event count
	numEvents = int(binary.LittleEndian.Uint32(buf[12:16]))
	return numEvents, nil
}

// DecodeEvent decodes a single event from a buffer at the specified offset
func DecodeEvent(buf []byte, offset int) (*ProfileEvent, error) {
	if len(buf) < offset+ProfileEventSize {
		return nil, ErrBufferTooSmall
	}

	event := &ProfileEvent{
		Timestamp:     binary.LittleEndian.Uint64(buf[offset:]),
		PID:           int32(binary.LittleEndian.Uint32(buf[offset+8:])),
		TID:           int32(binary.LittleEndian.Uint32(buf[offset+12:])),
		UserStackID:   int32(binary.LittleEndian.Uint32(buf[offset+16:])),
		KernelStackID: int32(binary.LittleEndian.Uint32(buf[offset+20:])),
		CPU:           binary.LittleEndian.Uint32(buf[offset+24:]),
		Flags:         binary.LittleEndian.Uint32(buf[offset+28:]),
	}

	return event, nil
}