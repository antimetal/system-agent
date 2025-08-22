// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tracker

import (
	"fmt"

	"github.com/go-logr/logr"
)

// NewRuntimeTracker creates a runtime tracker based on configuration and system capabilities
func NewRuntimeTracker(logger logr.Logger, config TrackerConfig) (RuntimeTracker, error) {
	// Detect system capabilities if auto mode is requested
	if config.Mode == TrackerModeAuto {
		capabilities := DetectCapabilities(config.CgroupPath)
		config.Mode = capabilities.RecommendedMode()
		
		logger.Info("Auto-detected tracker mode", 
			"mode", config.Mode,
			"capabilities", capabilities.GetCapabilitySummary())
	}

	// Create tracker based on mode
	switch config.Mode {
	case TrackerModePolling:
		return NewPollingTracker(logger, config)
	
	case TrackerModeEventDriven:
		return NewEventDrivenTracker(logger, config)
	
	case TrackerModeEBPF:
		// TODO: Implement eBPF tracker in future phases
		logger.Info("eBPF tracker not yet implemented, falling back to polling")
		return NewPollingTracker(logger, config)
	
	default:
		return nil, fmt.Errorf("unsupported tracker mode: %s", config.Mode)
	}
}

// ValidateConfig validates tracker configuration and sets defaults
func ValidateConfig(config *TrackerConfig) error {
	// Set defaults
	if config.Mode == "" {
		config.Mode = TrackerModeAuto
	}
	if config.CgroupPath == "" {
		config.CgroupPath = "/sys/fs/cgroup"
	}
	if config.UpdateInterval <= 0 {
		config.UpdateInterval = DefaultTrackerConfig().UpdateInterval
	}
	if config.EventBufferSize <= 0 {
		config.EventBufferSize = DefaultTrackerConfig().EventBufferSize
	}
	if config.DebounceInterval <= 0 {
		config.DebounceInterval = DefaultTrackerConfig().DebounceInterval
	}

	// Validate mode
	switch config.Mode {
	case TrackerModeAuto, TrackerModePolling, TrackerModeEventDriven, TrackerModeEBPF:
		// Valid modes
	default:
		return fmt.Errorf("invalid tracker mode: %s", config.Mode)
	}

	return nil
}