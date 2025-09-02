// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/proc"
)

func TestKernelCollector_Constructor(t *testing.T) {
	tests := []struct {
		name    string
		config  performance.CollectionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid absolute paths",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
				HostDevPath:  "/dev",
			},
			wantErr: false,
		},
		{
			name: "invalid relative proc path",
			config: performance.CollectionConfig{
				HostProcPath: "proc",
				HostDevPath:  "/dev",
			},
			wantErr: true,
			errMsg:  "HostProcPath must be an absolute path",
		},
		{
			name: "invalid relative dev path",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
				HostDevPath:  "dev",
			},
			wantErr: true,
			errMsg:  "HostDevPath must be an absolute path",
		},
		{
			name: "empty paths",
			config: performance.CollectionConfig{
				HostProcPath: "",
				HostDevPath:  "",
			},
			wantErr: true,
			errMsg:  "HostProcPath is required but not provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewKernelCollector(logr.Discard(), tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
			}
		})
	}
}

func TestKernelCollector_parseKmsgLine(t *testing.T) {
	config := performance.CollectionConfig{
		HostProcPath: t.TempDir(),
		HostDevPath:  "/dev",
	}
	collector, err := NewKernelCollector(logr.Discard(), config)
	require.NoError(t, err)

	statContent := "cpu  10 20 30 40 50 60 70 80 90 100\n" +
		"cpu0 1 2 3 4 5 6 7 8 9 10\n" +
		"intr 1234567890\n" +
		"ctxt 9876543210\n" +
		"btime 1640995200\n" + // 2022-01-01 00:00:00 UTC
		"processes 12345\n" +
		"procs_running 1\n" +
		"procs_blocked 0\n"
	err = os.WriteFile(filepath.Join(config.HostProcPath, "stat"), []byte(statContent), 0644)
	require.NoError(t, err)

	bootTime, err := proc.BootTime(config.HostProcPath)
	require.NoError(t, err)

	tests := []struct {
		name     string
		line     string
		wantErr  bool
		validate func(t *testing.T, msg *performance.KernelMessage)
	}{
		{
			name: "standard kernel message",
			line: "6,1234,5678901234,-;usb 1-1: new high-speed USB device number 2 using xhci_hcd\n",
			validate: func(t *testing.T, msg *performance.KernelMessage) {
				assert.Equal(t, uint8(0), msg.Facility) // 6 >> 3 = 0
				assert.Equal(t, uint8(6), msg.Severity) // 6 & 7 = 6 (INFO)
				assert.Equal(t, uint64(1234), msg.SequenceNum)
				assert.Equal(t, "usb 1-1: new high-speed USB device number 2 using xhci_hcd", msg.Message)
				assert.Equal(t, "usb", msg.Subsystem)
				assert.Equal(t, "1-1", msg.Device)

				// Check timestamp calculation
				expectedTime := bootTime.Add(time.Duration(5678901234) * time.Microsecond)
				assert.Equal(t, expectedTime.Unix(), msg.Timestamp.Unix())
			},
		},
		{
			name: "message with subsystem in brackets",
			line: "4,999,123456789,-;[drm:intel_dp_detect [i915]] DP-1: EDID checksum failed\n",
			validate: func(t *testing.T, msg *performance.KernelMessage) {
				assert.Equal(t, uint8(0), msg.Facility) // 4 >> 3 = 0
				assert.Equal(t, uint8(4), msg.Severity) // 4 & 7 = 4 (WARNING)
				assert.Equal(t, "[drm:intel_dp_detect [i915]] DP-1: EDID checksum failed", msg.Message)
				assert.Equal(t, "drm:intel_dp_detect [i915]", msg.Subsystem)
			},
		},
		{
			name: "emergency message",
			line: "0,100,50000000,-;kernel: Out of memory: Kill process 1234 (chrome) score 999\n",
			validate: func(t *testing.T, msg *performance.KernelMessage) {
				assert.Equal(t, uint8(0), msg.Facility) // 0 >> 3 = 0
				assert.Equal(t, uint8(0), msg.Severity) // 0 & 7 = 0 (EMERGENCY)
				assert.Equal(t, "kernel: Out of memory: Kill process 1234 (chrome) score 999", msg.Message)
				assert.Equal(t, "kernel", msg.Subsystem)
			},
		},
		{
			name:    "invalid format - missing semicolon",
			line:    "6,1234,5678901234,- missing semicolon",
			wantErr: true,
		},
		{
			name:    "invalid format - not enough fields",
			line:    "6,1234;message",
			wantErr: true,
		},
		{
			name:    "invalid priority",
			line:    "abc,1234,5678901234,-;test message",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := collector.parseKmsgLine(tt.line, bootTime)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, msg)

			if tt.validate != nil {
				tt.validate(t, msg)
			}
		})
	}
}

func TestKernelCollector_MessageLimit(t *testing.T) {
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostDevPath:  "/dev",
	}

	// Test with custom message limit
	collector, err := NewKernelCollector(logr.Discard(), config, WithMessageLimit(10))
	require.NoError(t, err)
	assert.Equal(t, 10, collector.messageLimit)

	// Test with default
	collector2, err := NewKernelCollector(logr.Discard(), config)
	require.NoError(t, err)
	assert.Equal(t, defaultMessageLimit, collector2.messageLimit)

	// Test with invalid limit (should keep default)
	collector3, err := NewKernelCollector(logr.Discard(), config, WithMessageLimit(0))
	require.NoError(t, err)
	assert.Equal(t, defaultMessageLimit, collector3.messageLimit)
}

func TestKernelCollector_ContinuousCollection(t *testing.T) {
	config := performance.CollectionConfig{
		HostProcPath: t.TempDir(),
		HostDevPath:  t.TempDir(),
	}

	// Create a fake /proc/stat file for boot time calculation
	statContent := "cpu  10 20 30 40 50 60 70 80 90 100\n" +
		"btime 1640995200\n" + // 2022-01-01 00:00:00 UTC
		"processes 12345\n"
	err := os.WriteFile(filepath.Join(config.HostProcPath, "stat"), []byte(statContent), 0644)
	require.NoError(t, err)

	collector, err := NewKernelCollector(logr.Discard(), config)
	require.NoError(t, err)

	// Test Status before starting
	assert.Equal(t, performance.CollectorStatusDisabled, collector.Status())
	assert.Nil(t, collector.LastError())

	// Test Start
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := collector.Start(ctx)
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Test Status after starting
	assert.Equal(t, performance.CollectorStatusActive, collector.Status())

	// Test double start should fail
	_, err = collector.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Test Stop
	err = collector.Stop()
	require.NoError(t, err)

	// Test Status after stopping
	assert.Equal(t, performance.CollectorStatusDisabled, collector.Status())

	// Test double stop should be ok
	err = collector.Stop()
	assert.NoError(t, err)
}

func TestKernelCollector_ParseMessageContent(t *testing.T) {
	tests := []struct {
		name          string
		message       string
		wantSubsystem string
		wantDevice    string
	}{
		{
			name:          "usb device format",
			message:       "usb 1-1: new high-speed USB device number 2 using xhci_hcd",
			wantSubsystem: "usb",
			wantDevice:    "1-1",
		},
		{
			name:          "bracketed subsystem",
			message:       "[drm:intel_dp_detect [i915]] DP-1: EDID checksum failed",
			wantSubsystem: "drm:intel_dp_detect [i915]",
			wantDevice:    "",
		},
		{
			name:          "simple subsystem with colon",
			message:       "kernel: Out of memory: Kill process 1234 (chrome) score 999",
			wantSubsystem: "kernel",
			wantDevice:    "",
		},
		{
			name:          "no clear subsystem",
			message:       "Some random kernel message without clear structure",
			wantSubsystem: "",
			wantDevice:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subsystem, device := parseMessageContent(tt.message)
			assert.Equal(t, tt.wantSubsystem, subsystem)
			assert.Equal(t, tt.wantDevice, device)
		})
	}
}
