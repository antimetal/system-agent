// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProfileEvent_Size verifies the ProfileEvent struct is exactly 32 bytes
func TestProfileEvent_Size(t *testing.T) {
	var event collectors.ProfileEvent
	size := binary.Size(event)
	assert.Equal(t, 32, size, "ProfileEvent should be exactly 32 bytes")
}

// TestProfileEvent_Encoding tests binary encoding/decoding of ProfileEvent
func TestProfileEvent_Encoding(t *testing.T) {
	tests := []struct {
		name  string
		event collectors.ProfileEvent
	}{
		{
			name: "basic event",
			event: collectors.ProfileEvent{
				Timestamp:     1234567890,
				PID:           1234,
				TID:           5678,
				UserStackId:   100,
				KernelStackId: 200,
				Cpu:           3,
				Flags:         0,
			},
		},
		{
			name: "event with flags",
			event: collectors.ProfileEvent{
				Timestamp:     9876543210,
				PID:           9999,
				TID:           8888,
				UserStackId:   -1, // Invalid stack
				KernelStackId: -1,
				Cpu:           7,
				Flags:         collectors.ProfileFlagUserStackTruncated | collectors.ProfileFlagKernelStackTruncated,
			},
		},
		{
			name: "max values",
			event: collectors.ProfileEvent{
				Timestamp:     ^uint64(0), // Max uint64
				PID:           2147483647, // Max int32
				TID:           2147483647, // Max int32
				UserStackId:   2147483647, // Max int32
				KernelStackId: 2147483647, // Max int32
				Cpu:           4294967295, // Max uint32
				Flags:         4294967295, // Max uint32
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			buf := new(bytes.Buffer)
			err := binary.Write(buf, binary.LittleEndian, tt.event)
			require.NoError(t, err)

			// Verify size
			assert.Equal(t, 32, buf.Len(), "encoded event should be 32 bytes")

			// Decode
			var decoded collectors.ProfileEvent
			err = binary.Read(buf, binary.LittleEndian, &decoded)
			require.NoError(t, err)

			// Verify round-trip
			assert.Equal(t, tt.event, decoded)
		})
	}
}

// TestProfileEvent_Parse tests parsing from raw bytes (simulating BPF ring buffer)
func TestProfileEvent_Parse(t *testing.T) {
	// Create a raw 32-byte buffer simulating BPF output
	raw := make([]byte, 32)

	// Fill with test data (little-endian)
	binary.LittleEndian.PutUint64(raw[0:8], 1234567890) // Timestamp
	binary.LittleEndian.PutUint32(raw[8:12], 1234)      // PID
	binary.LittleEndian.PutUint32(raw[12:16], 5678)     // TID
	binary.LittleEndian.PutUint32(raw[16:20], 100)      // UserStackId
	binary.LittleEndian.PutUint32(raw[20:24], 200)      // KernelStackId
	binary.LittleEndian.PutUint32(raw[24:28], 3)        // CPU
	binary.LittleEndian.PutUint32(raw[28:32], 0)        // Flags

	// Parse using binary.Read
	var event collectors.ProfileEvent
	reader := bytes.NewReader(raw)
	err := binary.Read(reader, binary.LittleEndian, &event)
	require.NoError(t, err)

	// Verify parsed values
	assert.Equal(t, uint64(1234567890), event.Timestamp)
	assert.Equal(t, int32(1234), event.PID)
	assert.Equal(t, int32(5678), event.TID)
	assert.Equal(t, int32(100), event.UserStackId)
	assert.Equal(t, int32(200), event.KernelStackId)
	assert.Equal(t, uint32(3), event.Cpu)
	assert.Equal(t, uint32(0), event.Flags)
}

// TestProfileEvent_Flags tests flag operations
func TestProfileEvent_Flags(t *testing.T) {
	tests := []struct {
		name    string
		flags   uint32
		hasUser bool
		hasKern bool
		hasCol  bool
	}{
		{
			name:    "no flags",
			flags:   0,
			hasUser: false,
			hasKern: false,
			hasCol:  false,
		},
		{
			name:    "user truncated",
			flags:   collectors.ProfileFlagUserStackTruncated,
			hasUser: true,
			hasKern: false,
			hasCol:  false,
		},
		{
			name:    "kernel truncated",
			flags:   collectors.ProfileFlagKernelStackTruncated,
			hasUser: false,
			hasKern: true,
			hasCol:  false,
		},
		{
			name:    "collision",
			flags:   collectors.ProfileFlagStackCollision,
			hasUser: false,
			hasKern: false,
			hasCol:  true,
		},
		{
			name:    "all flags",
			flags:   collectors.ProfileFlagUserStackTruncated | collectors.ProfileFlagKernelStackTruncated | collectors.ProfileFlagStackCollision,
			hasUser: true,
			hasKern: true,
			hasCol:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := collectors.ProfileEvent{Flags: tt.flags}

			assert.Equal(t, tt.hasUser, event.Flags&collectors.ProfileFlagUserStackTruncated != 0,
				"user stack truncated flag")
			assert.Equal(t, tt.hasKern, event.Flags&collectors.ProfileFlagKernelStackTruncated != 0,
				"kernel stack truncated flag")
			assert.Equal(t, tt.hasCol, event.Flags&collectors.ProfileFlagStackCollision != 0,
				"stack collision flag")
		})
	}
}

// BenchmarkProfileEvent_Parse benchmarks parsing events from raw bytes
func BenchmarkProfileEvent_Parse(b *testing.B) {
	// Create a raw 32-byte buffer
	raw := make([]byte, 32)
	binary.LittleEndian.PutUint64(raw[0:8], 1234567890)
	binary.LittleEndian.PutUint32(raw[8:12], 1234)
	binary.LittleEndian.PutUint32(raw[12:16], 5678)
	binary.LittleEndian.PutUint32(raw[16:20], 100)
	binary.LittleEndian.PutUint32(raw[20:24], 200)
	binary.LittleEndian.PutUint32(raw[24:28], 3)
	binary.LittleEndian.PutUint32(raw[28:32], 0)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var event collectors.ProfileEvent
		reader := bytes.NewReader(raw)
		_ = binary.Read(reader, binary.LittleEndian, &event)
	}
}

// BenchmarkProfileEvent_Encode benchmarks encoding events to bytes
func BenchmarkProfileEvent_Encode(b *testing.B) {
	event := collectors.ProfileEvent{
		Timestamp:     1234567890,
		PID:           1234,
		TID:           5678,
		UserStackId:   100,
		KernelStackId: 200,
		Cpu:           3,
		Flags:         0,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.LittleEndian, event)
	}
}

// BenchmarkProfileEvent_ZeroAllocationParse attempts zero-allocation parsing
func BenchmarkProfileEvent_ZeroAllocationParse(b *testing.B) {
	// Pre-allocate buffer
	raw := make([]byte, 32)
	binary.LittleEndian.PutUint64(raw[0:8], 1234567890)
	binary.LittleEndian.PutUint32(raw[8:12], 1234)
	binary.LittleEndian.PutUint32(raw[12:16], 5678)
	binary.LittleEndian.PutUint32(raw[16:20], 100)
	binary.LittleEndian.PutUint32(raw[20:24], 200)
	binary.LittleEndian.PutUint32(raw[24:28], 3)
	binary.LittleEndian.PutUint32(raw[28:32], 0)

	// Pre-allocate event
	var event collectors.ProfileEvent

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Direct field assignment (zero allocation)
		event.Timestamp = binary.LittleEndian.Uint64(raw[0:8])
		event.PID = int32(binary.LittleEndian.Uint32(raw[8:12]))
		event.TID = int32(binary.LittleEndian.Uint32(raw[12:16]))
		event.UserStackId = int32(binary.LittleEndian.Uint32(raw[16:20]))
		event.KernelStackId = int32(binary.LittleEndian.Uint32(raw[20:24]))
		event.Cpu = binary.LittleEndian.Uint32(raw[24:28])
		event.Flags = binary.LittleEndian.Uint32(raw[28:32])
	}
}
