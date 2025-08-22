// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tracker

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

func BenchmarkPollingTrackerSnapshot(b *testing.B) {
	logger := logr.Discard()
	config := TrackerConfig{
		Mode:           TrackerModePolling,
		CgroupPath:     "/sys/fs/cgroup",
		UpdateInterval: 30 * time.Second,
	}

	tracker, err := NewPollingTracker(logger, config)
	if err != nil {
		b.Skipf("Polling tracker creation failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = tracker.Start(ctx)
	if err != nil {
		b.Skipf("Polling tracker start failed: %v", err)
	}
	defer tracker.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tracker.GetSnapshot()
		if err != nil {
			b.Fatalf("GetSnapshot failed: %v", err)
		}
	}
}

func BenchmarkEventDrivenTrackerSnapshot(b *testing.B) {
	logger := logr.Discard()
	config := TrackerConfig{
		Mode:             TrackerModeEventDriven,
		CgroupPath:       "/sys/fs/cgroup",
		EventBufferSize:  100,
		DebounceInterval: 5 * time.Millisecond,
	}

	tracker, err := NewEventDrivenTracker(logger, config)
	if err != nil {
		b.Skipf("Event-driven tracker creation failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = tracker.Start(ctx)
	if err != nil {
		b.Skipf("Event-driven tracker start failed: %v", err)
	}
	defer tracker.Stop()

	// Let it initialize
	time.Sleep(10 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tracker.GetSnapshot()
		if err != nil {
			b.Fatalf("GetSnapshot failed: %v", err)
		}
	}
}

func BenchmarkCapabilityDetection(b *testing.B) {
	for i := 0; i < b.N; i++ {
		DetectCapabilities("/sys/fs/cgroup")
	}
}

func BenchmarkConfigValidation(b *testing.B) {
	config := TrackerConfig{
		Mode:             TrackerModeAuto,
		UpdateInterval:   30 * time.Second,
		EventBufferSize:  1000,
		DebounceInterval: 10 * time.Millisecond,
	}

	for i := 0; i < b.N; i++ {
		testConfig := config // Copy
		ValidateConfig(&testConfig)
	}
}