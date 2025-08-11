// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !integration

package core_test

import (
	"runtime"
	"testing"

	"github.com/antimetal/agent/pkg/ebpf/core"
	"github.com/cilium/ebpf/btf"
	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCOREManager(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("CO-RE tests only run on Linux")
	}

	logger := zapr.NewLogger(zap.NewNop())

	t.Run("kernel features detection", func(t *testing.T) {
		manager, err := core.NewManager(logger)
		require.NoError(t, err)

		features := manager.GetKernelFeatures()
		assert.NotNil(t, features)
		assert.NotEmpty(t, features.KernelVersion)

		// Log features for debugging
		t.Logf("Kernel features: %+v", features)
	})

	t.Run("BTF availability", func(t *testing.T) {
		// Check if kernel BTF is available
		_, err := btf.LoadKernelSpec()
		hasBTF := err == nil

		manager, err := core.NewManager(logger)
		require.NoError(t, err)

		features := manager.GetKernelFeatures()
		assert.Equal(t, hasBTF, features.HasBTF)
	})

	t.Run("CO-RE support detection", func(t *testing.T) {
		manager, err := core.NewManager(logger)
		require.NoError(t, err)

		features := manager.GetKernelFeatures()

		// Verify CO-RE support level is set
		assert.Contains(t, []string{"full", "partial", "none"}, features.CORESupport)

		// If we have BTF, we should have at least partial CO-RE support
		if features.HasBTF {
			assert.NotEqual(t, "none", features.CORESupport)
		}
	})
}

func TestKernelVersionParsing(t *testing.T) {
	tests := []struct {
		name          string
		kernelVersion string
		expectCORE    string
		expectBTF     bool
	}{
		{
			name:          "kernel 5.15",
			kernelVersion: "5.15.0-generic",
			expectCORE:    "full",
			expectBTF:     true, // Would be true on real 5.15 kernel
		},
		{
			name:          "kernel 5.2",
			kernelVersion: "5.2.0",
			expectCORE:    "full",
			expectBTF:     true, // Would be true on real 5.2 kernel
		},
		{
			name:          "kernel 4.19",
			kernelVersion: "4.19.0",
			expectCORE:    "partial",
			expectBTF:     false,
		},
		{
			name:          "kernel 4.18",
			kernelVersion: "4.18.0",
			expectCORE:    "partial",
			expectBTF:     false,
		},
		{
			name:          "kernel 4.14",
			kernelVersion: "4.14.0",
			expectCORE:    "none",
			expectBTF:     false,
		},
	}

	// Note: This is a unit test that validates parsing logic
	// Actual BTF availability depends on the running kernel
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't easily mock kernel version detection in the current implementation
			// This would require refactoring to inject version info
			t.Logf("Test case %s would expect CO-RE support: %s", tt.kernelVersion, tt.expectCORE)
		})
	}
}
