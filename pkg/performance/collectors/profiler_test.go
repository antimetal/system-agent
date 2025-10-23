// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

package collectors_test

import (
	"context"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfilerCollector_Constructor(t *testing.T) {
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		Interval:     time.Second,
	}

	// NewProfiler should succeed with valid interval
	collector, err := collectors.NewProfiler(logr.Discard(), config)

	assert.NoError(t, err)
	assert.NotNil(t, collector)
	assert.Equal(t, "profiler", collector.Name())

	caps := collector.Capabilities()
	assert.NotEmpty(t, caps.RequiredCapabilities, "Profiler should require eBPF capabilities")
	assert.False(t, caps.SupportsOneShot, "Profiler should not support one-shot collection")
	assert.True(t, caps.SupportsContinuous, "Profiler should support continuous collection")
}

func TestProfilerCollector_Constructor_IntervalValidation(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
	}{
		{
			name:     "Valid positive interval",
			interval: time.Second,
		},
		{
			name:     "Zero interval allowed for registry checks",
			interval: 0,
		},
		{
			name:     "Negative interval allowed for registry checks",
			interval: -time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := performance.CollectionConfig{
				HostProcPath: "/proc",
				HostSysPath:  "/sys",
				Interval:     tt.interval,
			}

			// Constructor should now allow any interval
			collector, err := collectors.NewProfiler(logr.Discard(), config)
			assert.NoError(t, err)
			assert.NotNil(t, collector)
		})
	}
}

func TestProfilerCollector_Setup_ConfigValidation(t *testing.T) {
	// Unit tests for configuration validation (hardware-agnostic)
	tests := []struct {
		name           string
		profilerConfig collectors.ProfilerConfig
		expectError    bool
		errorContains  string
	}{
		{
			name: "Empty event name",
			profilerConfig: collectors.ProfilerConfig{
				Event: collectors.PerfEventConfig{
					Name:         "", // Invalid - empty name
					Type:         0,
					Config:       0,
					SamplePeriod: 1000000,
				},
			},
			expectError:   true,
			errorContains: "event name is required",
		},
		{
			name: "Zero sample period",
			profilerConfig: collectors.ProfilerConfig{
				Event: collectors.PerfEventConfig{
					Name:         "test-event",
					Type:         0,
					Config:       0,
					SamplePeriod: 0, // Invalid - zero sample period
				},
			},
			expectError:   true,
			errorContains: "sample period must be greater than zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := performance.CollectionConfig{
				HostProcPath: "/proc",
				HostSysPath:  "/sys",
				Interval:     time.Second,
			}

			collector, err := collectors.NewProfiler(logr.Discard(), config)
			require.NoError(t, err)

			err = collector.Setup(tt.profilerConfig)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestProfilerCollector_StartWithoutSetup(t *testing.T) {
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		Interval:     time.Second,
	}

	collector, err := collectors.NewProfiler(logr.Discard(), config)
	require.NoError(t, err)

	// Start should fail if Setup() was not called
	ctx := context.Background()
	ch, err := collector.Start(ctx)
	assert.Error(t, err)
	assert.Nil(t, ch)
	assert.Contains(t, err.Error(), "Setup() must be called before Start()")
}
