// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package collectors_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfilerCollector_StartStop_Integration(t *testing.T) {
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
	}

	collector, err := collectors.NewCPUProfiler(logr.Discard(), config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start collector
	ch, err := collector.Start(ctx)
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Should not be able to start again
	ch2, err := collector.Start(ctx)
	assert.Error(t, err)
	assert.Nil(t, ch2)
	assert.Contains(t, err.Error(), "already running")

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)

	// Should be able to stop again without error
	err = collector.Stop()
	assert.NoError(t, err)
}

func TestProfilerCollector_DataCollection_Integration(t *testing.T) {
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
	}

	collector, err := collectors.NewCPUProfiler(logr.Discard(), config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Start collector
	ch, err := collector.Start(ctx)
	require.NoError(t, err)
	require.NotNil(t, ch)
	defer collector.Stop()

	// Wait for at least one profile
	select {
	case data := <-ch:
		profile, ok := data.(*performance.ProfileStats)
		require.True(t, ok, "Expected ProfileStats type")

		// Validate profile data
		assert.Equal(t, "cpu-cycles", profile.EventName)
		assert.Equal(t, uint32(0), profile.EventType)   // PERF_TYPE_HARDWARE
		assert.Equal(t, uint64(0), profile.EventConfig) // PERF_COUNT_HW_CPU_CYCLES
		assert.Equal(t, uint64(1000000), profile.SamplePeriod)
		assert.NotZero(t, profile.Duration)

		// We should have collected some samples
		assert.NotZero(t, profile.SampleCount, "Should have collected some samples")
		assert.NotEmpty(t, profile.Stacks, "Should have collected some stacks")
		assert.NotEmpty(t, profile.Processes, "Should have collected some processes")

	case <-ctx.Done():
		t.Fatal("Timeout waiting for profile data")
	}
}
