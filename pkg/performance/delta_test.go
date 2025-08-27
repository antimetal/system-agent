// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
)

func TestDeltaConfig(t *testing.T) {
	t.Run("default configuration", func(t *testing.T) {
		config := DefaultDeltaConfig()

		assert.Equal(t, DeltaModeDisabled, config.Mode)
		assert.Equal(t, 100*time.Millisecond, config.MinInterval)
		assert.Equal(t, 5*time.Minute, config.MaxInterval)
	})

	t.Run("IsEnabled with disabled mode", func(t *testing.T) {
		config := DeltaConfig{Mode: DeltaModeDisabled}

		assert.False(t, config.IsEnabled(MetricTypeTCP))
		assert.False(t, config.IsEnabled(MetricTypeNetwork))
	})

	t.Run("IsEnabled with enabled mode", func(t *testing.T) {
		config := DeltaConfig{Mode: DeltaModeEnabled}

		// All supported collectors should be enabled
		assert.True(t, config.IsEnabled(MetricTypeTCP))
		assert.True(t, config.IsEnabled(MetricTypeNetwork))
		assert.True(t, config.IsEnabled(MetricTypeCPU))
		assert.True(t, config.IsEnabled(MetricTypeSystem))
		assert.True(t, config.IsEnabled(MetricTypeDisk))
		assert.True(t, config.IsEnabled(MetricTypeMemory))
		assert.True(t, config.IsEnabled(MetricTypeNUMAStats))

		// Unsupported collectors should remain disabled (example: hypothetical future types)
		// Currently all implemented collectors support delta calculation
	})

}

func TestBaseDeltaCalculation(t *testing.T) {
	t.Run("basic delta calculation", func(t *testing.T) {
		config := CollectionConfig{
			Delta: DeltaConfig{Mode: DeltaModeEnabled},
		}
		collector := NewBaseDeltaCollector(
			MetricTypeTCP, "test", logr.Discard(), config, CollectorCapabilities{},
		)

		delta, reset := collector.CalculateUint64Delta(100, 50, time.Second)

		assert.Equal(t, uint64(50), delta)
		assert.False(t, reset)
	})

	t.Run("delta calculation with rates", func(t *testing.T) {
		config := CollectionConfig{
			Delta: DeltaConfig{
				Mode:        DeltaModeEnabled,
				MinInterval: 100 * time.Millisecond,
			},
		}
		collector := NewBaseDeltaCollector(
			MetricTypeTCP, "test", logr.Discard(), config, CollectorCapabilities{},
		)

		delta, reset := collector.CalculateUint64Delta(150, 50, time.Second)

		assert.Equal(t, uint64(100), delta)
		assert.False(t, reset)
	})

	t.Run("counter reset detection", func(t *testing.T) {
		config := CollectionConfig{
			Delta: DeltaConfig{Mode: DeltaModeEnabled},
		}
		collector := NewBaseDeltaCollector(
			MetricTypeTCP, "test", logr.Discard(), config, CollectorCapabilities{},
		)

		// Current < previous indicates reset (simple case)
		delta, reset := collector.CalculateUint64Delta(10, 100, time.Second)

		assert.Equal(t, uint64(0), delta)
		assert.True(t, reset)

		// Large previous value, small current value = still a reset
		// (no special rollover handling)
		delta2, reset2 := collector.CalculateUint64Delta(5, ^uint64(0)-10, time.Second)

		assert.Equal(t, uint64(0), delta2)
		assert.True(t, reset2)
	})

	t.Run("should calculate deltas validation", func(t *testing.T) {
		config := CollectionConfig{
			Delta: DeltaConfig{
				Mode:        DeltaModeEnabled,
				MaxInterval: 5 * time.Minute,
			},
		}
		collector := NewBaseDeltaCollector(
			MetricTypeTCP, "test", logr.Discard(), config, CollectorCapabilities{},
		)

		now := time.Now()

		// First collection - should not calculate
		should, reason := collector.ShouldCalculateDeltas(now)
		assert.False(t, should)
		assert.Contains(t, reason, "no previous state")

		// Set up previous state
		collector.IsFirst = false
		collector.LastTime = now.Add(-time.Second)

		// Normal interval - should calculate
		should, reason = collector.ShouldCalculateDeltas(now)
		assert.True(t, should)
		assert.Empty(t, reason)

		// Interval too large - should not calculate
		collector.LastTime = now.Add(-10 * time.Minute)
		should, reason = collector.ShouldCalculateDeltas(now)
		assert.False(t, should)
		assert.Contains(t, reason, "interval too large")

		// Time went backwards - should not calculate
		collector.LastTime = now.Add(time.Second)
		should, reason = collector.ShouldCalculateDeltas(now)
		assert.False(t, should)
		assert.Contains(t, reason, "time went backwards")
	})
}
