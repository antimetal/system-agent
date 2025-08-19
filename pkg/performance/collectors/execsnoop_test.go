// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !integration

package collectors_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecSnoopCollector_ParseEvent(t *testing.T) {
	logger := logr.Discard()
	config := performance.DefaultCollectionConfig()
	collector, err := collectors.NewExecSnoopCollector(logger, config, "")
	require.NoError(t, err)

	// Test event parsing without requiring actual BPF functionality
	type execsnoopEvent struct {
		PID       int32
		PPID      int32
		UID       uint32
		RetVal    int32
		ArgsCount int32
		ArgsSize  uint32
		Comm      [16]byte
	}

	tests := []struct {
		name      string
		buildData func() []byte
		wantErr   bool
		validate  func(t *testing.T, event *collectors.ExecEvent)
	}{
		{
			name: "valid event with args",
			buildData: func() []byte {
				var buf bytes.Buffer
				event := execsnoopEvent{
					PID:       1234,
					PPID:      1000,
					UID:       1001,
					RetVal:    0,
					ArgsCount: 3,
					ArgsSize:  15, // "arg1\x00arg2\x00arg3\x00" = 5+5+5 = 15 bytes
				}
				copy(event.Comm[:], "test-cmd")

				err := binary.Write(&buf, binary.LittleEndian, event)
				if err != nil {
					panic(err)
				}
				// Write args
				buf.WriteString("arg1\x00")
				buf.WriteString("arg2\x00")
				buf.WriteString("arg3\x00")

				return buf.Bytes()
			},
			wantErr: false,
			validate: func(t *testing.T, event *collectors.ExecEvent) {
				assert.Equal(t, int32(1234), event.PID)
				assert.Equal(t, int32(1000), event.PPID)
				assert.Equal(t, uint32(1001), event.UID)
				assert.Equal(t, int32(0), event.RetVal)
				assert.Equal(t, "arg1", event.Command) // Now extracts from args[0]
				assert.Equal(t, []string{"arg1", "arg2", "arg3"}, event.Args)
			},
		},
		{
			name: "event with no args",
			buildData: func() []byte {
				var buf bytes.Buffer
				event := execsnoopEvent{
					PID:       5678,
					PPID:      5000,
					UID:       2001,
					RetVal:    -1,
					ArgsCount: 0,
					ArgsSize:  0,
				}
				copy(event.Comm[:], "noargs")

				err := binary.Write(&buf, binary.LittleEndian, event)
				if err != nil {
					panic(err)
				}
				return buf.Bytes()
			},
			wantErr: false,
			validate: func(t *testing.T, event *collectors.ExecEvent) {
				assert.Equal(t, int32(5678), event.PID)
				assert.Equal(t, int32(5000), event.PPID)
				assert.Equal(t, uint32(2001), event.UID)
				assert.Equal(t, int32(-1), event.RetVal)
				assert.Equal(t, "noargs", event.Command)
				assert.Empty(t, event.Args)
			},
		},
		{
			name: "event with path command",
			buildData: func() []byte {
				var buf bytes.Buffer
				event := execsnoopEvent{
					PID:       9999,
					PPID:      8888,
					UID:       0,
					RetVal:    0,
					ArgsCount: 2,
					ArgsSize:  16, // "/usr/bin/ls\x00-la\x00" = 12+4 = 16 bytes
				}
				copy(event.Comm[:], "sh")

				err := binary.Write(&buf, binary.LittleEndian, event)
				if err != nil {
					panic(err)
				}
				// Write args with full path
				buf.WriteString("/usr/bin/ls\x00")
				buf.WriteString("-la\x00")

				return buf.Bytes()
			},
			wantErr: false,
			validate: func(t *testing.T, event *collectors.ExecEvent) {
				assert.Equal(t, int32(9999), event.PID)
				assert.Equal(t, int32(8888), event.PPID)
				assert.Equal(t, uint32(0), event.UID)
				assert.Equal(t, int32(0), event.RetVal)
				assert.Equal(t, "ls", event.Command) // Should extract basename
				assert.Equal(t, []string{"/usr/bin/ls", "-la"}, event.Args)
			},
		},
		{
			name: "event with command name only (no path)",
			buildData: func() []byte {
				var buf bytes.Buffer
				event := execsnoopEvent{
					PID:       7777,
					PPID:      6666,
					UID:       1000,
					RetVal:    0,
					ArgsCount: 1,
					ArgsSize:  5, // "echo\x00" = 5 bytes
				}
				copy(event.Comm[:], "echo")

				err := binary.Write(&buf, binary.LittleEndian, event)
				if err != nil {
					panic(err)
				}
				buf.WriteString("echo\x00")

				return buf.Bytes()
			},
			wantErr: false,
			validate: func(t *testing.T, event *collectors.ExecEvent) {
				assert.Equal(t, int32(7777), event.PID)
				assert.Equal(t, "echo", event.Command) // Should use args[0] even without path
				assert.Equal(t, []string{"echo"}, event.Args)
			},
		},
		{
			name: "event with whitespace in path",
			buildData: func() []byte {
				var buf bytes.Buffer
				event := execsnoopEvent{
					PID:       5555,
					PPID:      4444,
					UID:       0,
					RetVal:    0,
					ArgsCount: 1,
					ArgsSize:  13, // "  /bin/cat  \x00" = 13 bytes
				}
				copy(event.Comm[:], "cat")

				err := binary.Write(&buf, binary.LittleEndian, event)
				if err != nil {
					panic(err)
				}
				buf.WriteString("  /bin/cat  \x00")

				return buf.Bytes()
			},
			wantErr: false,
			validate: func(t *testing.T, event *collectors.ExecEvent) {
				assert.Equal(t, "cat", event.Command) // Should handle whitespace
				assert.Equal(t, []string{"  /bin/cat  "}, event.Args)
			},
		},
		{
			name: "event with edge case paths",
			buildData: func() []byte {
				var buf bytes.Buffer
				event := execsnoopEvent{
					PID:       3333,
					PPID:      2222,
					UID:       0,
					RetVal:    0,
					ArgsCount: 1,
					ArgsSize:  2, // "/\x00" = 2 bytes
				}
				copy(event.Comm[:], "init")

				err := binary.Write(&buf, binary.LittleEndian, event)
				if err != nil {
					panic(err)
				}
				buf.WriteString("/\x00")

				return buf.Bytes()
			},
			wantErr: false,
			validate: func(t *testing.T, event *collectors.ExecEvent) {
				assert.Equal(t, "init", event.Command) // Should keep original comm for edge case
				assert.Equal(t, []string{"/"}, event.Args)
			},
		},
		{
			name: "event too small",
			buildData: func() []byte {
				return []byte{1, 2, 3, 4} // Too small
			},
			wantErr: true,
		},
		{
			name: "args size mismatch",
			buildData: func() []byte {
				var buf bytes.Buffer
				event := execsnoopEvent{
					PID:       1111,
					PPID:      2222,
					UID:       1000,
					RetVal:    0,
					ArgsCount: 1,
					ArgsSize:  100, // Claims 100 bytes but we only provide 5
				}
				copy(event.Comm[:], "test")

				err := binary.Write(&buf, binary.LittleEndian, event)
				if err != nil {
					panic(err)
				}
				buf.WriteString("test\x00") // Only 5 bytes

				return buf.Bytes()
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.buildData()
			event, err := collector.ParseEvent(data)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, event)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, event)
				assert.NotZero(t, event.Timestamp)
				if tt.validate != nil {
					tt.validate(t, event)
				}
			}
		})
	}
}

func TestExecSnoopCollector_StartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BPF test in short mode")
	}

	logger := logr.Discard()
	config := performance.DefaultCollectionConfig()

	// Use a non-existent BPF object path to test error handling
	collector, err := collectors.NewExecSnoopCollector(logger, config, "/nonexistent/execsnoop.bpf.o")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start should fail due to missing BPF object or CO-RE not supported on non-Linux
	_, err = collector.Start(ctx)
	assert.Error(t, err, "Start should fail with missing BPF object or unsupported platform")

	// Stop should work even if start failed
	err = collector.Stop()
	assert.NoError(t, err)

	// Multiple stops should be safe
	err = collector.Stop()
	assert.NoError(t, err)
}

func TestExecSnoopCollector_DoubleStart(t *testing.T) {
	logger := logr.Discard()
	config := performance.DefaultCollectionConfig()

	collector, err := collectors.NewExecSnoopCollector(logger, config, "")
	require.NoError(t, err)

	// Manually set status to active to test double-start protection
	collector.SetStatus(performance.CollectorStatusActive)

	ctx := context.Background()
	_, err = collector.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}
