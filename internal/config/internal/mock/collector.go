// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package mock

import (
	"context"

	"github.com/go-logr/logr"

	"github.com/antimetal/agent/pkg/performance"
)

// Collector implements ContinuousCollector interface for testing
type Collector struct {
	metricType performance.MetricType
}

func (m *Collector) Type() performance.MetricType {
	return m.metricType
}

func (m *Collector) Name() string {
	return string(m.metricType) + "-collector"
}

func (m *Collector) Capabilities() performance.CollectorCapabilities {
	return performance.CollectorCapabilities{
		SupportsOneShot:      true,
		SupportsContinuous:   true,
		RequiredCapabilities: nil,
	}
}

func (m *Collector) Start(ctx context.Context) (<-chan any, error) {
	ch := make(chan any, 1)
	close(ch)
	return ch, nil
}

func (m *Collector) Stop() error {
	return nil
}

func (m *Collector) Status() performance.CollectorStatus {
	return performance.CollectorStatusActive
}

func (m *Collector) LastError() error {
	return nil
}

// NewCollector creates a new mock collector instance
func NewCollector(metricType performance.MetricType) performance.NewContinuousCollector {
	return func(logger logr.Logger, config performance.CollectionConfig) (performance.ContinuousCollector, error) {
		return &Collector{metricType: metricType}, nil
	}
}
