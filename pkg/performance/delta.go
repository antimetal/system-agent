// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
)

// BaseDeltaCollector provides common delta calculation functionality for collectors
type BaseDeltaCollector struct {
	BaseCollector
	config       DeltaConfig
	LastSnapshot any
	LastTime     time.Time
	IsFirst      bool
}

// NewBaseDeltaCollector creates a new base delta collector
func NewBaseDeltaCollector(
	metricType MetricType,
	name string,
	logger logr.Logger,
	config CollectionConfig,
	capabilities CollectorCapabilities,
) BaseDeltaCollector {
	return BaseDeltaCollector{
		BaseCollector: NewBaseCollector(metricType, name, logger, config, capabilities),
		config:        config.Delta,
		IsFirst:       true,
	}
}

// ResetDeltaState clears the delta calculation state
func (b *BaseDeltaCollector) ResetDeltaState() {
	b.LastSnapshot = nil
	b.LastTime = time.Time{}
	b.IsFirst = true
	b.Logger().V(1).Info("Delta state reset")
}

// HasDeltaState returns whether there is previous state for delta calculation
func (b *BaseDeltaCollector) HasDeltaState() bool {
	return !b.IsFirst && b.LastSnapshot != nil
}

// UpdateDeltaState updates the internal state after a successful collection
func (b *BaseDeltaCollector) UpdateDeltaState(snapshot any, currentTime time.Time) {
	b.LastSnapshot = snapshot
	b.LastTime = currentTime
	b.IsFirst = false
}

// ShouldCalculateDeltas checks if delta calculation should proceed
func (b *BaseDeltaCollector) ShouldCalculateDeltas(currentTime time.Time) (bool, string) {
	if b.config.Mode == DeltaModeDisabled {
		return false, "delta calculation disabled"
	}

	if b.IsFirst || b.LastTime.IsZero() {
		return false, "no previous state available"
	}

	interval := currentTime.Sub(b.LastTime)

	if interval < 0 {
		return false, "time went backwards"
	}

	if interval > b.config.MaxInterval {
		return false, fmt.Sprintf("interval too large (%v > %v)", interval, b.config.MaxInterval)
	}

	if interval < b.config.MinInterval {
		return false, fmt.Sprintf("interval too small (%v < %v)", interval, b.config.MinInterval)
	}

	return true, ""
}

// CreateDeltaMetadata creates metadata for this collection
func (b *BaseDeltaCollector) CreateDeltaMetadata(currentTime time.Time, resetDetected bool) DeltaMetadata {
	var interval time.Duration
	if !b.IsFirst && !b.LastTime.IsZero() {
		interval = currentTime.Sub(b.LastTime)
	}

	return DeltaMetadata{
		CollectionInterval:   interval,
		LastCollectionTime:   b.LastTime,
		IsFirstCollection:    b.IsFirst,
		CounterResetDetected: resetDetected,
	}
}

// CalculateUint64Delta calculates delta for uint64 counters with reset detection
func (b *BaseDeltaCollector) CalculateUint64Delta(
	current, previous uint64,
	interval time.Duration,
) (delta uint64, resetDetected bool) {
	// Detect counter reset (current < previous)
	// This happens due to system reboots, process restarts, etc.
	if current < previous {
		// Counter was reset - return zero delta and flag reset
		return 0, true
	}

	delta = current - previous
	return delta, false
}

// PopulateMetadata is a composition helper that sets DeltaMetadata for any delta struct
func (b *BaseDeltaCollector) PopulateMetadata(delta interface{}, currentTime time.Time, resetDetected bool) {
	metadata := b.CreateDeltaMetadata(currentTime, resetDetected)

	// Use type assertion to set the DeltaMetadata field
	switch d := delta.(type) {
	case *SystemDeltaData:
		d.DeltaMetadata = metadata
	case *MemoryDeltaData:
		d.DeltaMetadata = metadata
	case *CPUDeltaData:
		d.DeltaMetadata = metadata
	case *NetworkDeltaData:
		d.DeltaMetadata = metadata
	case *DiskDeltaData:
		d.DeltaMetadata = metadata
	case *TCPDeltaData:
		d.DeltaMetadata = metadata
	case *NUMADeltaData:
		d.DeltaMetadata = metadata
	}
}
