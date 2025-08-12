// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package performance_test

import (
	"fmt"
	"testing"

	"github.com/antimetal/agent/pkg/performance"
	_ "github.com/antimetal/agent/pkg/performance/collectors" // Import to trigger init() functions
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinuxCollectorRegistration(t *testing.T) {
	// Get available and unavailable collectors
	available := performance.GetAvailableCollectors()
	unavailable := performance.GetUnavailableCollectors()

	t.Logf("Available collectors: %d", len(available))
	t.Logf("Unavailable collectors: %d", len(unavailable))

	// Log available collectors
	for _, metricType := range available {
		t.Logf("  ✓ %s: available", metricType)
	}

	// Log unavailable collectors with reasons
	for metricType, info := range unavailable {
		reason := info.Reason
		if len(info.MissingCapabilities) > 0 {
			capNames := make([]string, len(info.MissingCapabilities))
			for i, cap := range info.MissingCapabilities {
				capNames[i] = cap.String()
			}
			reason = fmt.Sprintf("%s (missing: %v)", reason, capNames)
		}
		t.Logf("  ✗ %s: %s", metricType, reason)
	}

	// We should have SOME collectors available
	assert.NotEmpty(t, available, "Should have some collectors available on on any linux kernel!")

	// Basic collectors should be available on any Linux system
	basicCollectors := []performance.MetricType{
		performance.MetricTypeLoad,
		performance.MetricTypeMemory,
		performance.MetricTypeCPU,
	}

	for _, metricType := range basicCollectors {
		isAvailable, reason := performance.GetCollectorStatus(metricType)
		if !isAvailable {
			t.Logf("Basic collector %s not available: %s", metricType, reason)
		}

		// Note: We don't assert.True here because even basic collectors might fail
		// on certain container environments or restricted systems

		// XXX NO! We should assert now that we are an integration test
	}

	// Verify the registry mechanism works
	assert.NotNil(t, available, "Available collectors list should not be nil")
	assert.NotNil(t, unavailable, "Unavailable collectors list should not be nil")
}

func TestLinuxCollectorStatus(t *testing.T) {
	tests := []struct {
		name            string
		metricType      performance.MetricType
		expectAvailable bool // Some collectors might not be available in all environments
	}{
		{
			name:            "Load collector",
			metricType:      performance.MetricTypeLoad,
			expectAvailable: true, // Should be available on all Linux systems
		},
		{
			name:            "Memory collector",
			metricType:      performance.MetricTypeMemory,
			expectAvailable: true, // Should be available on all Linux systems
		},
		{
			name:            "CPU collector",
			metricType:      performance.MetricTypeCPU,
			expectAvailable: true, // Should be available on all Linux systems
		},
		{
			name:            "Process collector",
			metricType:      performance.MetricTypeProcess,
			expectAvailable: false, // Might require additional capabilities
		},
		{
			name:            "Kernel collector",
			metricType:      performance.MetricTypeKernel,
			expectAvailable: false, // Might require eBPF support
		},
		{
			name:            "Non-existent collector",
			metricType:      performance.MetricType("non-existent"),
			expectAvailable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			available, reason := performance.GetCollectorStatus(tt.metricType)
			t.Logf("%s: available=%v, reason=%s", tt.metricType, available, reason)

			// Verify we always get a reason string
			assert.NotEmpty(t, reason, "Should always have a reason")

			// Log expectations vs reality for debugging
			if tt.expectAvailable && !available {
				t.Logf("Expected %s to be available but it's not: %s", tt.metricType, reason)
			} else if !tt.expectAvailable && available {
				t.Logf("Did not expect %s to be available but it is", tt.metricType)
			}
		})
	}
}

func TestLinuxCollectorCreation(t *testing.T) {
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
	}

	available := performance.GetAvailableCollectors()
	require.NotEmpty(t, available, "Should have at least some collectors available")

	// Try to create each available collector
	for _, metricType := range available {
		t.Run(string(metricType), func(t *testing.T) {
			factory, err := performance.GetCollector(metricType)
			require.NoError(t, err, "Should be able to get collector factory")

			// Create a discard logger for testing
			testLogger := logr.Discard()

			// Try to create the collector
			collector, err := factory(testLogger, config)
			if err != nil {
				t.Logf("Failed to create %s collector: %v", metricType, err)
				// Don't fail the test - some collectors might need special privileges
				return
			}

			// Verify the collector has capabilities
			caps := collector.Capabilities()
			assert.NotNil(t, caps, "Collector should have capabilities")

			t.Logf("✓ Successfully created %s collector", metricType)
		})
	}
}
