// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package collectors_test

import (
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnumerateAvailablePerfEvents(t *testing.T) {
	events, err := collectors.EnumerateAvailablePerfEvents()
	require.NoError(t, err)

	// Should have at least some events
	assert.Greater(t, len(events), 0, "Should find some perf events")

	// Should have both hardware and software events
	hasHardware := false
	hasSoftware := false

	for _, event := range events {
		t.Logf("Found event: %s (type=%d, config=%d, source=%s, available=%v)",
			event.Name, event.Type, event.Config, event.Source, event.Available)

		// Basic validation
		assert.NotEmpty(t, event.Name, "Event should have a name")
		assert.NotEmpty(t, event.Description, "Event should have a description")
		assert.NotEmpty(t, event.Source, "Event should have a source")

		if event.Type == 0 { // PERF_TYPE_HARDWARE
			hasHardware = true
		}
		if event.Type == 1 { // PERF_TYPE_SOFTWARE
			hasSoftware = true
		}
	}

	t.Logf("Found hardware events: %v, software events: %v", hasHardware, hasSoftware)
	assert.True(t, hasSoftware, "Should find software events (always available)")
	// Hardware events may not be available in VMs, so we don't assert on them
}

func TestGetAvailablePerfEventNames(t *testing.T) {
	names, err := collectors.GetAvailablePerfEventNames()
	require.NoError(t, err)

	// Check if perf events are available at all
	if len(names) == 0 {
		t.Skip("No perf events available - may be due to permissions, virtualization, or perf_event_paranoid settings")
	}

	// Should have at least some available events
	assert.Greater(t, len(names), 0, "Should have some available perf events")

	// Should include basic software events
	foundCpuClock := false
	for _, name := range names {
		t.Logf("Available event: %s", name)
		assert.NotEmpty(t, name, "Event name should not be empty")

		if name == "cpu-clock" {
			foundCpuClock = true
		}
	}

	assert.True(t, foundCpuClock, "Should find cpu-clock software event")
}

func TestFindPerfEventByName(t *testing.T) {
	// Test finding a software event (should always exist)
	event, err := collectors.FindPerfEventByName("cpu-clock")
	require.NoError(t, err)
	assert.NotNil(t, event)
	assert.Equal(t, "cpu-clock", event.Name)
	assert.Equal(t, uint32(1), event.Type) // PERF_TYPE_SOFTWARE

	// Check availability - may be disabled in restricted environments
	if !event.Available {
		t.Skip("cpu-clock event not available - may be due to permissions or perf_event_paranoid settings")
	}
	assert.True(t, event.Available, "cpu-clock should be available")

	// Test finding non-existent event
	event, err = collectors.FindPerfEventByName("nonexistent-event")
	assert.Error(t, err)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "not found")
}

func TestProfilerCollector_EnumerateSupported(t *testing.T) {
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		Interval:     time.Second,
	}

	collector, err := collectors.NewProfiler(logr.Discard(), config)
	require.NoError(t, err)

	// Test getting all supported events
	events, err := collector.EnumerateSupportedEvents()
	require.NoError(t, err)

	if len(events) == 0 {
		t.Skip("No perf events available - may be due to permissions or perf_event_paranoid settings")
	}
	assert.Greater(t, len(events), 0, "Should find supported events")

	// Test getting just event names
	names, err := collector.GetSupportedEventNames()
	require.NoError(t, err)
	assert.Greater(t, len(names), 0, "Should find supported event names")

	t.Logf("Supported events: %v", names)
}

func TestPrintEventSummary(t *testing.T) {
	// This test just ensures PrintEventSummary doesn't crash
	err := collectors.PrintEventSummary()
	assert.NoError(t, err, "PrintEventSummary should not fail")
}

func TestProfilerCollector_ValidationWithEnumeration(t *testing.T) {
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		Interval:     time.Second,
	}

	collector, err := collectors.NewProfiler(logr.Discard(), config)
	require.NoError(t, err)

	// Test setup with cpu-clock (software event)
	err = collector.Setup(collectors.NewProfilerConfig(collectors.CPUClockEvent))
	if err != nil {
		// Check if it's because perf events aren't available
		events, _ := collector.EnumerateSupportedEvents()
		if len(events) == 0 {
			t.Skip("No perf events available - may be due to permissions or perf_event_paranoid settings")
		}
		assert.NoError(t, err, "cpu-clock should be supported when perf events are available")
	}
}

func TestGetPerfEventSummary(t *testing.T) {
	summary, err := collectors.GetPerfEventSummary()
	require.NoError(t, err)
	assert.NotNil(t, summary)

	// Should have found some events
	assert.Greater(t, summary.TotalEvents, 0, "Should discover some events")

	if summary.AvailableEvents == 0 {
		t.Log("No perf events available - may be due to permissions or perf_event_paranoid settings")
		return
	}
	assert.Greater(t, summary.AvailableEvents, 0, "Should have some available events")

	// Should have events by source
	assert.Greater(t, len(summary.BySource), 0, "Should categorize events by source")
	assert.Greater(t, len(summary.ByType), 0, "Should categorize events by type")

	t.Logf("Summary: %+v", summary)

	// Should have software events (always available)
	assert.Contains(t, summary.BySource, "software", "Should find software events")
	assert.Greater(t, summary.BySource["software"], 0, "Should have software events")
}

func TestGetEventsBySource(t *testing.T) {
	// Test getting software events
	softwareEvents, err := collectors.GetEventsBySource("software")
	require.NoError(t, err)
	assert.Greater(t, len(softwareEvents), 0, "Should find software events")

	for _, event := range softwareEvents {
		assert.Equal(t, "software", event.Source, "All events should be software source")
		assert.Equal(t, uint32(1), event.Type, "All software events should have PERF_TYPE_SOFTWARE")
		t.Logf("Software event: %s (config=%d)", event.Name, event.Config)
	}

	// Test getting cache events (if available)
	cacheEvents, err := collectors.GetEventsBySource("cache")
	require.NoError(t, err)
	t.Logf("Found %d cache events", len(cacheEvents))

	for _, event := range cacheEvents {
		assert.Equal(t, "cache", event.Source, "All events should be cache source")
		assert.Equal(t, uint32(3), event.Type, "All cache events should have PERF_TYPE_HW_CACHE")
		t.Logf("Cache event: %s (config=0x%x)", event.Name, event.Config)
	}
}

func TestGetEventsByType(t *testing.T) {
	// Test getting software events by type
	softwareEvents, err := collectors.GetEventsByType(1) // PERF_TYPE_SOFTWARE
	require.NoError(t, err)
	assert.Greater(t, len(softwareEvents), 0, "Should find software events")

	for _, event := range softwareEvents {
		assert.Equal(t, uint32(1), event.Type, "All events should be PERF_TYPE_SOFTWARE")
		assert.Equal(t, "software", event.Source, "All should be from software source")
	}

	// Test getting cache events by type
	cacheEvents, err := collectors.GetEventsByType(3) // PERF_TYPE_HW_CACHE
	require.NoError(t, err)
	t.Logf("Found %d hardware cache events", len(cacheEvents))

	for _, event := range cacheEvents {
		assert.Equal(t, uint32(3), event.Type, "All events should be PERF_TYPE_HW_CACHE")
		assert.Equal(t, "cache", event.Source, "All should be from cache source")
	}
}
