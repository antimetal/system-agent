// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package capabilities_test

import (
	"testing"

	"github.com/antimetal/agent/pkg/kernel"
	"github.com/antimetal/agent/pkg/performance/capabilities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEBPFCapabilities_Integration(t *testing.T) {
	// Test that GetEBPFCapabilities works on actual Linux systems
	caps := capabilities.GetEBPFCapabilities()

	// Should return at least one capability
	assert.NotEmpty(t, caps)

	// Get the current kernel version for validation
	version, err := kernel.GetCurrentVersion()
	require.NoError(t, err)

	if version.IsAtLeast(5, 8) {
		// Modern kernels should return CAP_BPF and CAP_PERFMON
		assert.Contains(t, caps, capabilities.CAP_BPF)
		assert.Contains(t, caps, capabilities.CAP_PERFMON)
		assert.NotContains(t, caps, capabilities.CAP_SYS_ADMIN)
		assert.Equal(t, 2, len(caps))
	} else {
		// Older kernels should return CAP_SYS_ADMIN
		assert.Contains(t, caps, capabilities.CAP_SYS_ADMIN)
		assert.NotContains(t, caps, capabilities.CAP_BPF)
		assert.NotContains(t, caps, capabilities.CAP_PERFMON)
		assert.Equal(t, 1, len(caps))
	}

	t.Logf("Kernel version: %s", version.String())
	t.Logf("Required capabilities: %v", caps)
}
