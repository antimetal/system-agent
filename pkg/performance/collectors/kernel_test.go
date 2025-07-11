// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKernelCollector_parseKmsgLine(t *testing.T) {
	// Create a test collector
	config := performance.CollectionConfig{
		HostProcPath: t.TempDir(),
		HostDevPath:  "/dev",
	}
	collector := NewKernelCollector(logr.Discard(), config)
	
	// Create a fake uptime file for boot time calculation
	uptimeContent := "100.50 200.25\n" // 100.5 seconds uptime
	err := os.WriteFile(filepath.Join(config.HostProcPath, "uptime"), []byte(uptimeContent), 0644)
	require.NoError(t, err)
	
	bootTime, err := collector.getBootTime()
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
				assert.Equal(t, uint8(0), msg.Facility)    // 6 >> 3 = 0
				assert.Equal(t, uint8(6), msg.Severity)    // 6 & 7 = 6 (INFO)
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
				assert.Equal(t, uint8(0), msg.Facility)    // 4 >> 3 = 0
				assert.Equal(t, uint8(4), msg.Severity)    // 4 & 7 = 4 (WARNING)
				assert.Equal(t, "[drm:intel_dp_detect [i915]] DP-1: EDID checksum failed", msg.Message)
				assert.Equal(t, "drm:intel_dp_detect [i915]", msg.Subsystem)
			},
		},
		{
			name: "emergency message",
			line: "0,100,50000000,-;kernel: Out of memory: Kill process 1234 (chrome) score 999\n",
			validate: func(t *testing.T, msg *performance.KernelMessage) {
				assert.Equal(t, uint8(0), msg.Facility)    // 0 >> 3 = 0
				assert.Equal(t, uint8(0), msg.Severity)    // 0 & 7 = 0 (EMERGENCY)
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

func TestKernelCollector_getBootTime(t *testing.T) {
	tempDir := t.TempDir()
	
	// Create test uptime file
	uptimeContent := "12345.67 98765.43\n"
	uptimePath := filepath.Join(tempDir, "uptime")
	err := os.WriteFile(uptimePath, []byte(uptimeContent), 0644)
	require.NoError(t, err)
	
	config := performance.CollectionConfig{
		HostProcPath: tempDir,
		HostDevPath:  "/dev",
	}
	collector := NewKernelCollector(logr.Discard(), config)
	
	// First call should calculate boot time
	beforeCall := time.Now()
	bootTime1, err := collector.getBootTime()
	afterCall := time.Now()
	require.NoError(t, err)
	
	// Boot time should be approximately now - 12345.67 seconds
	expectedBootTime := beforeCall.Add(-time.Duration(12345.67 * float64(time.Second)))
	assert.WithinDuration(t, expectedBootTime, bootTime1, 2*time.Second)
	assert.True(t, bootTime1.Before(afterCall))
	
	// Second call should return cached value
	bootTime2, err := collector.getBootTime()
	require.NoError(t, err)
	assert.Equal(t, bootTime1, bootTime2)
	
	// Modify the uptime file - cached value should not change
	newUptimeContent := "99999.99 88888.88\n"
	err = os.WriteFile(uptimePath, []byte(newUptimeContent), 0644)
	require.NoError(t, err)
	
	bootTime3, err := collector.getBootTime()
	require.NoError(t, err)
	assert.Equal(t, bootTime1, bootTime3) // Still cached
}

func TestKernelCollector_Properties(t *testing.T) {
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostDevPath:  "/dev",
	}
	collector := NewKernelCollector(logr.Discard(), config)
	
	assert.Equal(t, performance.MetricTypeKernel, collector.Type())
	assert.Equal(t, "Kernel Message Collector", collector.Name())
	
	caps := collector.Capabilities()
	assert.True(t, caps.SupportsOneShot)
	assert.False(t, caps.SupportsContinuous)
	assert.True(t, caps.RequiresRoot)
	assert.False(t, caps.RequiresEBPF)
	assert.Equal(t, "3.5.0", caps.MinKernelVersion)
}

func TestKernelCollector_MessageLimit(t *testing.T) {
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostDevPath:  "/dev",
	}
	
	// Test with custom message limit
	collector := NewKernelCollector(logr.Discard(), config, WithMessageLimit(10))
	assert.Equal(t, 10, collector.messageLimit)
	
	// Test with default
	collector2 := NewKernelCollector(logr.Discard(), config)
	assert.Equal(t, defaultMessageLimit, collector2.messageLimit)
	
	// Test with invalid limit (should keep default)
	collector3 := NewKernelCollector(logr.Discard(), config, WithMessageLimit(0))
	assert.Equal(t, defaultMessageLimit, collector3.messageLimit)
}