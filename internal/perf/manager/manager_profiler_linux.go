// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

package manager

import (
	"context"
	"fmt"
	"time"

	agentv1 "github.com/antimetal/agent/pkg/api/antimetal/agent/v1"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
)

func (m *manager) startProfileCollector(ctx context.Context, configObj *agentv1.ProfileCollectionConfig, name string) (context.CancelFunc, error) {
	// Defensive validation
	if configObj.GetEventName() == "" {
		return nil, fmt.Errorf("event_name is required in ProfileCollectionConfig")
	}

	// Build CollectionConfig
	interval := time.Duration(configObj.GetIntervalSeconds()) * time.Second
	if interval == 0 {
		interval = 60 * time.Second // Default 60 seconds
	}

	collectionConfig := performance.CollectionConfig{
		HostProcPath: m.procPath,
		HostSysPath:  m.sysPath,
		HostDevPath:  m.devPath,
		Interval:     interval,
	}
	collectionConfig.ApplyDefaults()

	// Create profiler directly (not through registry since we need type-specific Setup)
	profiler, err := collectors.NewProfiler(m.logger.WithName(name), collectionConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create profiler: %w", err)
	}

	// Find and validate event
	eventInfo, err := collectors.FindPerfEventByName(configObj.GetEventName())
	if err != nil {
		return nil, fmt.Errorf("invalid event_name %q: %w", configObj.GetEventName(), err)
	}

	// Determine sample period with smart defaults
	samplePeriod := configObj.GetSamplePeriod()
	if samplePeriod == 0 {
		// Default based on event type (hardware=1M cycles, software=10ms)
		if eventInfo.Type == collectors.PERF_TYPE_HARDWARE {
			samplePeriod = 1000000
		} else {
			samplePeriod = 10000000
		}
	}

	// Convert PerfEventInfo to PerfEventConfig
	eventConfig := collectors.PerfEventConfig{
		Name:         eventInfo.Name,
		Type:         eventInfo.Type,
		Config:       eventInfo.Config,
		SamplePeriod: samplePeriod,
	}

	// Setup profiler with event config (defensive validation happens in Setup)
	profilerConfig := collectors.NewProfilerConfigWithSamplePeriod(eventConfig, samplePeriod)
	if err := profiler.Setup(profilerConfig); err != nil {
		return nil, fmt.Errorf("profiler setup failed: %w. Event %q may not be available on this system",
			err, configObj.GetEventName())
	}

	// Start profiler
	collectorCtx, cancel := context.WithCancel(ctx)
	ch, err := profiler.Start(collectorCtx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start profiler: %w", err)
	}

	// Forward events to merger
	m.perfEvents.Add(ch)

	m.logger.Info("profiler started successfully",
		"event", configObj.GetEventName(),
		"sample_period", samplePeriod,
		"interval", interval)

	return cancel, nil
}
