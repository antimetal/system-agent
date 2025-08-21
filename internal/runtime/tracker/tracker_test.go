// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tracker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		input       TrackerConfig
		expectError bool
		expected    TrackerConfig
	}{
		{
			name:  "empty config gets defaults",
			input: TrackerConfig{},
			expected: TrackerConfig{
				Mode:             TrackerModeAuto,
				CgroupPath:       "/sys/fs/cgroup",
				UpdateInterval:   30 * time.Second,
				EventBufferSize:  1000,
				DebounceInterval: 10 * time.Millisecond,
			},
		},
		{
			name: "partial config fills defaults",
			input: TrackerConfig{
				Mode:           TrackerModePolling,
				UpdateInterval: 60 * time.Second,
			},
			expected: TrackerConfig{
				Mode:             TrackerModePolling,
				CgroupPath:       "/sys/fs/cgroup",
				UpdateInterval:   60 * time.Second,
				EventBufferSize:  1000,
				DebounceInterval: 10 * time.Millisecond,
			},
		},
		{
			name: "valid config unchanged",
			input: TrackerConfig{
				Mode:             TrackerModeEventDriven,
				CgroupPath:       "/custom/cgroup",
				UpdateInterval:   45 * time.Second,
				EventBufferSize:  500,
				DebounceInterval: 5 * time.Millisecond,
			},
			expected: TrackerConfig{
				Mode:             TrackerModeEventDriven,
				CgroupPath:       "/custom/cgroup",
				UpdateInterval:   45 * time.Second,
				EventBufferSize:  500,
				DebounceInterval: 5 * time.Millisecond,
			},
		},
		{
			name: "invalid mode rejected",
			input: TrackerConfig{
				Mode: "invalid-mode",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.input // Copy to avoid mutation
			err := ValidateConfig(&config)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, config)
			}
		})
	}
}

func TestTrackerCapabilitiesMethods(t *testing.T) {
	tests := []struct {
		name         string
		capabilities TrackerCapabilities
		expectedMode TrackerMode
		canEvent     bool
		canPoll      bool
	}{
		{
			name: "full capabilities",
			capabilities: TrackerCapabilities{
				FsnotifyAvailable: true,
				ProcfsAvailable:   true,
				CgroupfsAvailable: true,
				CanWatchProc:      true,
				CanWatchCgroups:   true,
			},
			expectedMode: TrackerModeEventDriven,
			canEvent:     true,
			canPoll:      true,
		},
		{
			name: "polling only",
			capabilities: TrackerCapabilities{
				FsnotifyAvailable: false,
				ProcfsAvailable:   true,
				CgroupfsAvailable: true,
				CanWatchProc:      false,
				CanWatchCgroups:   false,
			},
			expectedMode: TrackerModePolling,
			canEvent:     false,
			canPoll:      true,
		},
		{
			name: "partial event capabilities",
			capabilities: TrackerCapabilities{
				FsnotifyAvailable: true,
				ProcfsAvailable:   true,
				CgroupfsAvailable: false,
				CanWatchProc:      true,
				CanWatchCgroups:   false,
			},
			expectedMode: TrackerModeEventDriven, // Can watch proc
			canEvent:     true,
			canPoll:      false, // No cgroup access
		},
		{
			name: "no capabilities",
			capabilities: TrackerCapabilities{
				FsnotifyAvailable: false,
				ProcfsAvailable:   false,
				CgroupfsAvailable: false,
				CanWatchProc:      false,
				CanWatchCgroups:   false,
			},
			expectedMode: TrackerModePolling, // Fallback
			canEvent:     false,
			canPoll:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedMode, tt.capabilities.RecommendedMode())
			assert.Equal(t, tt.canEvent, tt.capabilities.CanUseEventDriven())
			assert.Equal(t, tt.canPoll, tt.capabilities.CanUsePolling())

			// Summary should always contain basic structure
			summary := tt.capabilities.GetCapabilitySummary()
			assert.Contains(t, summary, "Runtime Tracking Capabilities")
			assert.Contains(t, summary, "Recommended mode")
		})
	}
}