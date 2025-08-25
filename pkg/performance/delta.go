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

// DeltaCalculator provides utilities for calculating deltas and rates from monotonic counters
type DeltaCalculator struct {
	config DeltaConfig
}

// NewDeltaCalculator creates a new delta calculator with the given configuration
func NewDeltaCalculator(config DeltaConfig) *DeltaCalculator {
	return &DeltaCalculator{
		config: config,
	}
}

// CalculateUint64Delta calculates delta and optional rate for uint64 counters
func (d *DeltaCalculator) CalculateUint64Delta(
	current, previous uint64,
	interval time.Duration,
) (delta uint64, rate *float64, resetDetected bool) {
	// Detect counter reset (current < previous)
	// This happens due to system reboots, process restarts, etc.
	if current < previous {
		// Counter was reset - return zero delta and flag reset
		return 0, nil, true
	}

	delta = current - previous

	// Calculate rate if enabled and interval is sufficient
	if d.config.IsRatesEnabled() && interval >= d.config.MinInterval {
		seconds := interval.Seconds()
		if seconds > 0 {
			rateValue := float64(delta) / seconds
			rate = &rateValue
		}
	}

	return delta, rate, false
}

// CreateDeltaMetadata creates metadata for delta calculations
func (d *DeltaCalculator) CreateDeltaMetadata(
	currentTime, lastTime time.Time,
	isFirst, resetDetected bool,
) DeltaMetadata {
	var interval time.Duration
	if !isFirst && !lastTime.IsZero() {
		interval = currentTime.Sub(lastTime)
	}

	return DeltaMetadata{
		CollectionInterval:   interval,
		LastCollectionTime:   lastTime,
		IsFirstCollection:    isFirst,
		CounterResetDetected: resetDetected,
	}
}

// ShouldCalculateDeltas determines if delta calculation should proceed based on timing
func (d *DeltaCalculator) ShouldCalculateDeltas(
	currentTime, lastTime time.Time,
	isFirst bool,
) (bool, string) {
	if d.config.Mode == DeltaModeDisabled {
		return false, "delta calculation disabled"
	}

	if isFirst || lastTime.IsZero() {
		return false, "no previous state available"
	}

	interval := currentTime.Sub(lastTime)

	if interval < 0 {
		return false, "time went backwards"
	}

	if interval > d.config.MaxInterval {
		return false, fmt.Sprintf("interval too large (%v > %v)", interval, d.config.MaxInterval)
	}

	if interval < d.config.MinInterval {
		return false, fmt.Sprintf("interval too small (%v < %v)", interval, d.config.MinInterval)
	}

	return true, ""
}

// ValidateInterval checks if the collection interval is suitable for rate calculations
func (d *DeltaCalculator) ValidateInterval(interval time.Duration) error {
	if interval < 0 {
		return fmt.Errorf("negative interval: %v", interval)
	}

	if d.config.IsRatesEnabled() && interval > 0 && interval < d.config.MinInterval {
		return fmt.Errorf("interval %v is less than minimum %v for rate calculation",
			interval, d.config.MinInterval)
	}

	if interval > d.config.MaxInterval {
		return fmt.Errorf("interval %v exceeds maximum %v", interval, d.config.MaxInterval)
	}

	return nil
}

// BaseDeltaCollector provides common delta calculation functionality for collectors
type BaseDeltaCollector struct {
	BaseCollector
	deltaCalc    *DeltaCalculator
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
		deltaCalc:     NewDeltaCalculator(config.Delta),
		IsFirst:       true,
	}
}

// ResetDeltaState clears the delta calculation state
func (b *BaseDeltaCollector) ResetDeltaState() error {
	b.LastSnapshot = nil
	b.LastTime = time.Time{}
	b.IsFirst = true
	b.Logger().V(1).Info("Delta state reset")
	return nil
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
	return b.deltaCalc.ShouldCalculateDeltas(currentTime, b.LastTime, b.IsFirst)
}

// CreateDeltaMetadata creates metadata for this collection
func (b *BaseDeltaCollector) CreateDeltaMetadata(currentTime time.Time, resetDetected bool) DeltaMetadata {
	return b.deltaCalc.CreateDeltaMetadata(currentTime, b.LastTime, b.IsFirst, resetDetected)
}

// CalculateUint64Delta is a convenience method for uint64 delta calculations
func (b *BaseDeltaCollector) CalculateUint64Delta(
	current, previous uint64,
	interval time.Duration,
) (uint64, *float64, bool) {
	return b.deltaCalc.CalculateUint64Delta(current, previous, interval)
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
