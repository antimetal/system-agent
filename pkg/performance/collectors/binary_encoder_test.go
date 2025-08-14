// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt


package collectors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBinaryEncoder_ZeroAllocations(t *testing.T) {
	encoder := NewBinaryEncoder()
	events := make([]ProfileEvent, 10)
	buf := make([]byte, BufferSize)

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

	// Measure allocations
	allocs := testing.AllocsPerRun(100, func() {
		encoder.EncodeEvents(events, buf)
	})

	assert.Equal(t, float64(0), allocs, "Binary encoding should have zero allocations")
}

func TestBinaryEncoder_EncodeEvents(t *testing.T) {
	encoder := NewBinaryEncoder()
	buf := make([]byte, BufferSize)

	testCases := []struct {
		name      string
		numEvents int
	}{
		{"empty", 0},
		{"single", 1},
		{"multiple", 10},
		{"max", MaxEventsPerBatch},
		{"overflow", MaxEventsPerBatch + 10}, // Should truncate
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			events := make([]ProfileEvent, tc.numEvents)
			for i := range events {
				events[i] = ProfileEvent{
					Timestamp:     uint64(i * 1000),
					PID:           int32(i + 1000),
					TID:           int32(i + 2000),
					UserStackID:   int32(i),
					KernelStackID: int32(i + 100),
					CPU:           uint32(i % 8),
					Flags:         uint32(i),
				}
			}

			n, err := encoder.EncodeEvents(events, buf)
			require.NoError(t, err)

			expectedEvents := tc.numEvents
			if expectedEvents > MaxEventsPerBatch {
				expectedEvents = MaxEventsPerBatch
			}

			expectedSize := ProfileHeaderSize + (expectedEvents * ProfileEventSize)
			assert.Equal(t, expectedSize, n)

			// Verify header
			numEvents, err := DecodeHeader(buf[:n])
			require.NoError(t, err)
			assert.Equal(t, expectedEvents, numEvents)
		})
	}
}

func TestBinaryEncoder_RoundTrip(t *testing.T) {
	encoder := NewBinaryEncoder()
	buf := make([]byte, BufferSize)

	// Create test events
	originalEvents := []ProfileEvent{
		{
			Timestamp:     123456789,
			PID:           1234,
			TID:           5678,
			UserStackID:   42,
			KernelStackID: 84,
			CPU:           3,
			Flags:         0x10,
		},
		{
			Timestamp:     987654321,
			PID:           9876,
			TID:           5432,
			UserStackID:   99,
			KernelStackID: 88,
			CPU:           7,
			Flags:         0x20,
		},
	}

	// Encode
	n, err := encoder.EncodeEvents(originalEvents, buf)
	require.NoError(t, err)

	// Decode header
	numEvents, err := DecodeHeader(buf[:n])
	require.NoError(t, err)
	assert.Equal(t, len(originalEvents), numEvents)

	// Decode events
	decodedEvents := make([]ProfileEvent, 0, numEvents)
	offset := ProfileHeaderSize
	for i := 0; i < numEvents; i++ {
		event, err := DecodeEvent(buf[:n], offset)
		require.NoError(t, err)
		decodedEvents = append(decodedEvents, *event)
		offset += ProfileEventSize
	}

	// Compare
	assert.Equal(t, originalEvents, decodedEvents)
}

func TestBinaryEncoder_SmallBuffer(t *testing.T) {
	encoder := NewBinaryEncoder()
	smallBuf := make([]byte, 10) // Too small for header

	events := []ProfileEvent{{
		Timestamp: 12345,
		PID:       100,
	}}

	_, err := encoder.EncodeEvents(events, smallBuf)
	assert.Equal(t, ErrBufferTooSmall, err)
}

func TestDecodeHeader_Validation(t *testing.T) {
	tests := []struct {
		name    string
		buf     []byte
		wantErr error
	}{
		{
			name:    "too small",
			buf:     make([]byte, 10),
			wantErr: ErrBufferTooSmall,
		},
		{
			name:    "wrong magic",
			buf:     []byte("BADM0000000000000"),
			wantErr: ErrInvalidMagic,
		},
		{
			name: "wrong version",
			buf: func() []byte {
				b := make([]byte, ProfileHeaderSize)
				copy(b, ProfileMagic)
				b[4] = 0xFF // Wrong version
				return b
			}(),
			wantErr: ErrUnsupportedVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeHeader(tt.buf)
			assert.Equal(t, tt.wantErr, err)
		})
	}
}

func BenchmarkBinaryEncoder_EncodeEvents(b *testing.B) {
	encoder := NewBinaryEncoder()
	events := make([]ProfileEvent, 10)
	buf := make([]byte, BufferSize)

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

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		encoder.EncodeEvents(events, buf)
	}

	b.ReportMetric(float64(len(events)*b.N)/b.Elapsed().Seconds(), "events/sec")
}