// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build unit

package capabilities

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCapability_String(t *testing.T) {
	tests := []struct {
		name string
		cap  Capability
		want string
	}{
		{
			name: "CAP_SYS_ADMIN",
			cap:  CAP_SYS_ADMIN,
			want: "CAP_SYS_ADMIN",
		},
		{
			name: "CAP_SYSLOG",
			cap:  CAP_SYSLOG,
			want: "CAP_SYSLOG",
		},
		{
			name: "CAP_BPF",
			cap:  CAP_BPF,
			want: "CAP_BPF",
		},
		{
			name: "CAP_PERFMON",
			cap:  CAP_PERFMON,
			want: "CAP_PERFMON",
		},
		{
			name: "Unknown capability",
			cap:  Capability(999),
			want: "UNKNOWN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cap.String())
		})
	}
}

func TestGetEBPFCapabilities(t *testing.T) {
	caps := GetEBPFCapabilities()

	// Should include the modern eBPF capabilities
	assert.Contains(t, caps, CAP_BPF)
	assert.Contains(t, caps, CAP_PERFMON)
	assert.Contains(t, caps, CAP_SYS_ADMIN)

	// Should have at least these 3 capabilities
	assert.GreaterOrEqual(t, len(caps), 3)
}

func TestHasAllCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		required []Capability
	}{
		{
			name:     "Empty capabilities",
			required: []Capability{},
		},
		{
			name:     "Single capability",
			required: []Capability{CAP_SYSLOG},
		},
		{
			name:     "Multiple capabilities",
			required: []Capability{CAP_BPF, CAP_PERFMON},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasAll, missing, err := HasAllCapabilities(tt.required)

			// Should not error
			assert.NoError(t, err)

			// Empty capabilities should always work
			if len(tt.required) == 0 {
				assert.True(t, hasAll)
				assert.Empty(t, missing)
			}

			// Verify consistency between hasAll and missing
			if hasAll {
				assert.Empty(t, missing)
			} else {
				assert.NotEmpty(t, missing)
			}

			// Log for debugging (actual capability checking is platform-specific)
			t.Logf("HasAll: %v, Missing: %v", hasAll, missing)
		})
	}
}
