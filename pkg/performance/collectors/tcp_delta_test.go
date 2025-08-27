// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/antimetal/agent/pkg/performance"
)

func TestTCPCollector_DeltaAwareCollector(t *testing.T) {
	t.Run("interface compliance", func(t *testing.T) {
		logger := logr.Discard()
		config := performance.CollectionConfig{
			HostProcPath: "/tmp",
		}
		config.ApplyDefaults()

		collector, err := NewTCPCollector(logger, config)
		require.NoError(t, err)

		// Verify interface compliance
		var _ performance.DeltaAwareCollector = collector

		// Test interface methods
		assert.False(t, collector.HasDeltaState())

		capabilities := collector.GetDeltaCapabilities()
		assert.Contains(t, capabilities, "ActiveOpens")
		assert.Contains(t, capabilities, "RetransSegs")
		assert.Contains(t, capabilities, "InSegs")

		collector.ResetDeltaState()
	})

	t.Run("delta calculation progression", func(t *testing.T) {
		// Create a mock TCP collector that we can control
		logger := logr.Discard()
		config := performance.CollectionConfig{
			HostProcPath: "/tmp",
			Delta: performance.DeltaConfig{
				Mode:        performance.DeltaModeEnabled,
				MinInterval: 100 * time.Millisecond,
				MaxInterval: 5 * time.Minute,
				EnabledCollectors: map[performance.MetricType]bool{
					performance.MetricTypeTCP: true,
				},
			},
		}

		collector := &TCPCollector{
			BaseDeltaCollector: performance.NewBaseDeltaCollector(
				performance.MetricTypeTCP,
				"Test TCP Collector",
				logger,
				config,
				performance.CollectorCapabilities{},
			),
		}

		// Test first collection (no deltas available)
		assert.False(t, collector.HasDeltaState())
		should, reason := collector.ShouldCalculateDeltas(time.Now())
		assert.False(t, should)
		assert.Contains(t, reason, "no previous state")

		// Simulate first collection
		firstStats := &performance.TCPStats{
			ActiveOpens: 100,
			RetransSegs: 50,
			InSegs:      1000,
		}
		firstTime := time.Now()
		collector.UpdateDeltaState(firstStats, firstTime)

		assert.True(t, collector.HasDeltaState())

		// Simulate second collection (deltas should be calculated)
		// Use 1.5 seconds to test actual rate calculation logic (not trivial 1:1)
		interval := 1500 * time.Millisecond
		secondTime := firstTime.Add(interval)
		should, reason = collector.ShouldCalculateDeltas(secondTime)
		assert.True(t, should)
		assert.Empty(t, reason)

		// Test delta calculation directly
		secondStats := &performance.TCPStats{
			ActiveOpens: 150,  // +50
			RetransSegs: 75,   // +25
			InSegs:      2000, // +1000
		}

		collector.calculateTCPDeltas(
			secondStats,
			firstStats,
			secondTime,
			config.Delta,
		)

		// Verify delta calculations (now in secondStats directly)
		require.NotNil(t, secondStats.Delta)
		assert.Equal(t, uint64(50), secondStats.Delta.ActiveOpens)
		assert.Equal(t, uint64(25), secondStats.Delta.RetransSegs)
		assert.Equal(t, uint64(1000), secondStats.Delta.InSegs)

		// Verify rate calculations (delta / 1.5s) - values are truncated to uint64
		assert.Equal(t, uint64(33), secondStats.Delta.ActiveOpensPerSec) // 50/1.5s ≈ 33.33/s truncated to 33
		assert.Equal(t, uint64(16), secondStats.Delta.RetransSegsPerSec) // 25/1.5s ≈ 16.67/s truncated to 16

		assert.Equal(t, uint64(666), secondStats.Delta.InSegsPerSec) // 1000/1.5s ≈ 666.67/s truncated to 666

		// Verify metadata
		assert.Equal(t, interval, secondStats.Delta.CollectionInterval)
		assert.False(t, secondStats.Delta.IsFirstCollection)
		assert.False(t, secondStats.Delta.CounterResetDetected)
	})

	t.Run("counter reset detection", func(t *testing.T) {
		logger := logr.Discard()
		config := performance.CollectionConfig{
			Delta: performance.DeltaConfig{
				Mode: performance.DeltaModeEnabled,
			},
		}

		collector := &TCPCollector{
			BaseDeltaCollector: performance.NewBaseDeltaCollector(
				performance.MetricTypeTCP,
				"Test TCP Collector",
				logger,
				config,
				performance.CollectorCapabilities{},
			),
		}

		// First collection
		firstStats := &performance.TCPStats{
			ActiveOpens: 1000,
			InSegs:      5000,
		}
		firstTime := time.Now()
		collector.UpdateDeltaState(firstStats, firstTime)

		// Second collection with counter reset (values decreased)
		secondStats := &performance.TCPStats{
			ActiveOpens: 50,  // Reset happened
			InSegs:      100, // Reset happened
		}
		secondTime := firstTime.Add(time.Second)

		collector.calculateTCPDeltas(
			secondStats,
			firstStats,
			secondTime,
			config.Delta,
		)

		// Deltas should be 0 due to reset detection
		require.NotNil(t, secondStats.Delta)
		assert.Equal(t, uint64(0), secondStats.Delta.ActiveOpens)
		assert.Equal(t, uint64(0), secondStats.Delta.InSegs)

		// Reset should be detected
		assert.True(t, secondStats.Delta.CounterResetDetected)
	})

	t.Run("interval validation", func(t *testing.T) {
		logger := logr.Discard()
		config := performance.CollectionConfig{
			Delta: performance.DeltaConfig{
				Mode:        performance.DeltaModeEnabled,
				MaxInterval: 5 * time.Minute,
			},
		}

		collector := &TCPCollector{
			BaseDeltaCollector: performance.NewBaseDeltaCollector(
				performance.MetricTypeTCP,
				"Test TCP Collector",
				logger,
				config,
				performance.CollectorCapabilities{},
			),
		}

		// Set up initial state
		firstTime := time.Now()
		collector.UpdateDeltaState(&performance.TCPStats{}, firstTime)

		// Test interval too large
		laterTime := firstTime.Add(10 * time.Minute)
		should, reason := collector.ShouldCalculateDeltas(laterTime)
		assert.False(t, should)
		assert.Contains(t, reason, "interval too large")

		// Test time going backwards
		earlierTime := firstTime.Add(-time.Second)
		should, reason = collector.ShouldCalculateDeltas(earlierTime)
		assert.False(t, should)
		assert.Contains(t, reason, "time went backwards")
	})
}
