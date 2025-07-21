// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"fmt"

	"github.com/go-logr/logr"
)

type CollectorRegistry struct {
	pointCollectors      map[MetricType]PointCollector
	continuousCollectors map[MetricType]ContinuousCollector
	logger               logr.Logger
}

func NewCollectorRegistry(logger logr.Logger) *CollectorRegistry {
	return &CollectorRegistry{
		pointCollectors:      make(map[MetricType]PointCollector),
		continuousCollectors: make(map[MetricType]ContinuousCollector),
		logger:               logger.WithName("registry"),
	}
}

func (r *CollectorRegistry) RegisterPoint(collector PointCollector) error {
	if collector == nil {
		return fmt.Errorf("cannot register nil collector")
	}

	metricType := collector.Type()
	if _, exists := r.pointCollectors[metricType]; exists {
		return fmt.Errorf("point collector for metric type %s already registered", metricType)
	}
	if _, exists := r.continuousCollectors[metricType]; exists {
		return fmt.Errorf("continuous collector for metric type %s already registered", metricType)
	}

	r.pointCollectors[metricType] = collector
	r.logger.Info("registered point collector", "type", metricType, "name", collector.Name())
	return nil
}

func (r *CollectorRegistry) RegisterContinuous(collector ContinuousCollector) error {
	if collector == nil {
		return fmt.Errorf("cannot register nil collector")
	}

	metricType := collector.Type()
	if _, exists := r.continuousCollectors[metricType]; exists {
		return fmt.Errorf("continuous collector for metric type %s already registered", metricType)
	}
	if _, exists := r.pointCollectors[metricType]; exists {
		return fmt.Errorf("point collector for metric type %s already registered", metricType)
	}

	r.continuousCollectors[metricType] = collector
	r.logger.Info("registered continuous collector", "type", metricType, "name", collector.Name())
	return nil
}

func (r *CollectorRegistry) GetPoint(metricType MetricType) PointCollector {
	return r.pointCollectors[metricType]
}

func (r *CollectorRegistry) GetContinuous(metricType MetricType) ContinuousCollector {
	return r.continuousCollectors[metricType]
}

func (r *CollectorRegistry) GetAllPoint() []PointCollector {
	collectors := make([]PointCollector, 0, len(r.pointCollectors))
	for _, collector := range r.pointCollectors {
		collectors = append(collectors, collector)
	}
	return collectors
}

func (r *CollectorRegistry) GetAllContinuous() []ContinuousCollector {
	collectors := make([]ContinuousCollector, 0, len(r.continuousCollectors))
	for _, collector := range r.continuousCollectors {
		collectors = append(collectors, collector)
	}
	return collectors
}

func (r *CollectorRegistry) GetEnabledPoint(config CollectionConfig) []PointCollector {
	var enabled []PointCollector
	for metricType, collector := range r.pointCollectors {
		if config.EnabledCollectors[metricType] {
			enabled = append(enabled, collector)
		}
	}
	return enabled
}

func (r *CollectorRegistry) GetEnabledContinuous(config CollectionConfig) []ContinuousCollector {
	var enabled []ContinuousCollector
	for metricType, collector := range r.continuousCollectors {
		if config.EnabledCollectors[metricType] {
			enabled = append(enabled, collector)
		}
	}
	return enabled
}
