// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"testing"
	"time"
	"unsafe"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProfilerCollector(t *testing.T) {
	logger := testr.New(t)
	config := performance.CollectionConfig{
		Interval:     time.Second,
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
	}

	collector, err := NewProfilerCollector(logger, config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	// Test collector properties
	assert.Equal(t, performance.MetricTypeProfiler, collector.Type())
	assert.Equal(t, "profiler", collector.Name())

	caps := collector.Capabilities()
	assert.False(t, caps.SupportsOneShot)
	assert.True(t, caps.SupportsContinuous)
	assert.NotEmpty(t, caps.RequiredCapabilities)
	assert.Equal(t, "5.8", caps.MinKernelVersion)
}

// Tests for removed event enumeration functions have been removed
// The profiler now uses a fixed configuration without event discovery

// Tests that require actual eBPF functionality have been moved to integration tests
// Unit tests focus on cross-platform functionality like constructors and parsing

func TestProfileEvent_Size(t *testing.T) {
	// Test that ProfileEvent has expected size (32 bytes as designed)
	var event ProfileEvent
	size := int(unsafe.Sizeof(event))
	assert.Equal(t, 32, size, "ProfileEvent should be exactly 32 bytes")
}

func TestParseEvent(t *testing.T) {
	logger := testr.New(t)
	config := performance.CollectionConfig{
		Interval: time.Second,
	}

	collector, err := NewProfilerCollector(logger, config)
	require.NoError(t, err)

	// Test with too small data
	_, err = collector.parseEvent([]byte{1, 2, 3})
	assert.Error(t, err, "Should error on too small data")

	// Test with valid size but invalid data structure is harder to test
	// without actual ring buffer data, so we just test the size check
	validSize := make([]byte, 32)
	event, err := collector.parseEvent(validSize)
	assert.NoError(t, err)
	assert.NotNil(t, event)
}
