// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build unit

package performance_test

import (
	"context"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/capabilities"
	"github.com/go-logr/logr"
)

// TestCollector implements the PointCollector interface for testing
type TestCollector struct {
	performance.BaseCollector
}

func NewTestCollector() *TestCollector {
	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   false,
		RequiredCapabilities: nil, // No special capabilities required
	}

	return &TestCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricType("test"),
			"test-collector",
			logr.Discard(),
			performance.CollectionConfig{},
			capabilities,
		),
	}
}

func (tc *TestCollector) Collect(ctx context.Context) (any, error) {
	testData := map[string]any{
		"timestamp": time.Now().Format(time.RFC3339),
		"metric":    "test_value",
		"value":     42,
		"status":    "success",
	}
	return testData, nil
}

func TestContinuousPointCollector(t *testing.T) {
	testCollector := NewTestCollector()
	config := performance.CollectionConfig{
		Interval: 100 * time.Millisecond,
	}
	continuousCollector := performance.NewContinuousPointCollector(testCollector, config, logr.Discard())

	// Test basic properties
	if continuousCollector.Type() != performance.MetricType("test") {
		t.Errorf("Expected type 'test', got %s", continuousCollector.Type())
	}

	if continuousCollector.Name() != "test-collector" {
		t.Errorf("Expected name 'test-collector', got %s", continuousCollector.Name())
	}

	caps := continuousCollector.Capabilities()
	if caps.SupportsOneShot {
		t.Error("Expected SupportsOneShot to be false")
	}
	if !caps.SupportsContinuous {
		t.Error("Expected SupportsContinuous to be true")
	}

	// Test initial status
	if continuousCollector.Status() != performance.CollectorStatusDisabled {
		t.Errorf("Expected status disabled, got %s", continuousCollector.Status())
	}

	// Start the collector
	ch, err := continuousCollector.Start(context.Background())
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Verify status changed to active
	if continuousCollector.Status() != performance.CollectorStatusActive {
		t.Errorf("Expected status active after start, got %s", continuousCollector.Status())
	}

	// Collect a few data points
	var dataPoints []any
	timeout := time.After(1 * time.Second)

	for len(dataPoints) < 3 {
		select {
		case data := <-ch:
			dataPoints = append(dataPoints, data)
			t.Logf("Received data point %d: %+v", len(dataPoints), data)
		case <-timeout:
			t.Fatalf("Timeout waiting for data points. Got %d points", len(dataPoints))
		}
	}

	// Stop the collector
	err = continuousCollector.Stop()
	if err != nil {
		t.Errorf("Failed to stop collector: %v", err)
	}

	if continuousCollector.Status() != performance.CollectorStatusDisabled {
		t.Errorf("Expected status disabled after stop, got %s", continuousCollector.Status())
	}
	if len(dataPoints) < 3 {
		t.Errorf("Expected at least 3 data points, got %d", len(dataPoints))
	}
}

func TestContinuousPointCollector_ContextCancellation(t *testing.T) {
	testCollector := NewTestCollector()
	config := performance.CollectionConfig{
		Interval: 50 * time.Millisecond,
	}
	continuousCollector := performance.NewContinuousPointCollector(testCollector, config, logr.Discard())

	// Start the collector
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := continuousCollector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Collect one data point to ensure it's working
	select {
	case data := <-ch:
		t.Logf("Received initial data point: %+v", data)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for initial data point")
	}

	// Cancel the context and stop the collector and allow some time for the collector to stop
	cancel()
	time.Sleep(100 * time.Millisecond)
	// Verify status changed to disabled
	if continuousCollector.Status() != performance.CollectorStatusDisabled {
		t.Errorf("Expected status disabled after stop, got %s", continuousCollector.Status())
	}
}

func TestOnceContinuousCollector(t *testing.T) {
	testCollector := NewTestCollector()
	config := performance.CollectionConfig{}
	onceCollector := performance.NewOnceContinuousCollector(
		testCollector, config, logr.Discard(),
	)

	// Test basic properties
	if onceCollector.Type() != performance.MetricType("test") {
		t.Errorf("Expected type 'test', got %s", onceCollector.Type())
	}

	if onceCollector.Name() != "test-collector" {
		t.Errorf("Expected name 'test-collector', got %s", onceCollector.Name())
	}

	caps := onceCollector.Capabilities()
	if !caps.SupportsOneShot {
		t.Error("Expected SupportsOneShot to be true")
	}
	if caps.SupportsContinuous {
		t.Error("Expected SupportsContinuous to be false")
	}

	// Test initial status
	if onceCollector.Status() != performance.CollectorStatusDisabled {
		t.Errorf("Expected status disabled, got %s", onceCollector.Status())
	}

	// Start the collector
	ctx := context.Background()
	ch, err := onceCollector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Verify status changed to active
	if onceCollector.Status() != performance.CollectorStatusActive {
		t.Errorf("Expected status active after start, got %s", onceCollector.Status())
	}

	testCollect := func(ch <-chan any) {
		// Collect the single data point
		var data any
		var chOpen bool

		select {
		case data, chOpen = <-ch:
			break
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Timeout waiting for data point")
		}

		// Verify we received data
		if data == nil {
			t.Error("Expected to receive data, got nil")
		}

		// Verify channel is closed after one-shot collection
		select {
		case _, chOpen = <-ch:
			break
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Timeout waiting for data point")
		}

		if chOpen {
			t.Error("Expected channel to be closed after one-shot collection, but it is still open")
		}
	}

	testCollect(ch)

	// Stop the collector
	err = onceCollector.Stop()
	if err != nil {
		t.Errorf("Failed to stop collector: %v", err)
	}

	// Verify status changed to disabled
	if onceCollector.Status() != performance.CollectorStatusDisabled {
		t.Errorf("Expected status disabled after stop, got %s", onceCollector.Status())
	}

	// Start the channel again and verify we get the same data
	ch, err = onceCollector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector again: %v", err)
	}
	if onceCollector.Status() != performance.CollectorStatusActive {
		t.Errorf("Expected status active after start, got %s", onceCollector.Status())
	}
	testCollect(ch)
}

func TestCollectorCapabilities_CanRun(t *testing.T) {
	tests := []struct {
		name    string
		caps    performance.CollectorCapabilities
		wantRun bool // We can't be sure on all platforms, but we can test the logic
	}{
		{
			name: "No capabilities required",
			caps: performance.CollectorCapabilities{
				RequiredCapabilities: nil,
			},
			wantRun: true, // Should always run if no capabilities required
		},
		{
			name: "Empty capabilities slice",
			caps: performance.CollectorCapabilities{
				RequiredCapabilities: []capabilities.Capability{},
			},
			wantRun: true, // Should always run if empty slice
		},
		{
			name: "Single capability",
			caps: performance.CollectorCapabilities{
				RequiredCapabilities: []capabilities.Capability{capabilities.CAP_SYSLOG},
			},
		},
		{
			name: "Multiple capabilities",
			caps: performance.CollectorCapabilities{
				RequiredCapabilities: []capabilities.Capability{
					capabilities.CAP_BPF,
					capabilities.CAP_PERFMON,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canRun, missing, err := tt.caps.CanRun()

			if err != nil {
				t.Errorf("CanRun() returned error: %v", err)
			}

			// Verify consistency between canRun and missing
			if canRun && len(missing) > 0 {
				t.Error("CanRun() returned canRun=true but missing capabilities is not empty")
			}

			if !canRun && len(missing) == 0 {
				t.Error("CanRun() returned canRun=false but missing capabilities is empty")
			}

			// Special case: no capabilities required should always work
			if tt.wantRun && !canRun {
				t.Errorf("CanRun() = %v, want %v", canRun, tt.wantRun)
			}

			t.Logf("CanRun: %v, Missing capabilities: %v", canRun, missing)
		})
	}
}
