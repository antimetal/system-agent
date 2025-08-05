// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build unit

package performance_test

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/antimetal/agent/pkg/performance"
	_ "github.com/antimetal/agent/pkg/performance/collectors" // Import to trigger init() functions
	"github.com/stretchr/testify/assert"
)

func TestCapabilityAwareRegistration(t *testing.T) {
	// This test verifies that collectors are registered based on their capabilities
	
	// Get available collectors
	available := performance.GetAvailableCollectors()
	unavailable := performance.GetUnavailableCollectors()
	
	t.Logf("Platform: %s", runtime.GOOS)
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
	
	// On non-Linux platforms, all collectors should be available
	if runtime.GOOS != "linux" {
		assert.Empty(t, unavailable, "No collectors should be unavailable on non-Linux platforms")
		return
	}
	
	// Test specific collector status
	kernelAvailable, kernelReason := performance.GetCollectorStatus(performance.MetricTypeKernel)
	t.Logf("Kernel collector status: available=%v, reason=%s", kernelAvailable, kernelReason)
	
	// The exact number of available/unavailable collectors depends on the running system's capabilities
	// So we just verify the mechanism works
	assert.NotNil(t, available, "Available collectors list should not be nil")
	assert.NotNil(t, unavailable, "Unavailable collectors list should not be nil")
}

func TestGetCollectorStatus(t *testing.T) {
	tests := []struct {
		name       string
		metricType performance.MetricType
	}{
		{
			name:       "Load collector",
			metricType: performance.MetricTypeLoad,
		},
		{
			name:       "Kernel collector",
			metricType: performance.MetricTypeKernel,
		},
		{
			name:       "Process collector",
			metricType: performance.MetricTypeProcess,
		},
		{
			name:       "Non-existent collector",
			metricType: performance.MetricType("non-existent"),
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			available, reason := performance.GetCollectorStatus(tt.metricType)
			t.Logf("%s: available=%v, reason=%s", tt.metricType, available, reason)
			
			// Verify we get a reason string in all cases
			assert.NotEmpty(t, reason, "Should always have a reason")
		})
	}
}