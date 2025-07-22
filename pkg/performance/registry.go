// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"fmt"
)

var registry = make(map[MetricType]NewContinuousCollector)

// Register adds a NewCollector factory to the global registry for metricType.
// collector is used to create new collector instances with the provided logger and
// configuration.
//
// This function is usually called during package initialization (typically in init() functions)
// to register collector implementations before they can be instantiated by performance.Manager.
//
// It will panic if a collector for the given metricType is already registered
func Register(metricType MetricType, collector NewContinuousCollector) {
	_, exists := registry[metricType]
	if exists {
		panic(fmt.Sprintf("Collector for %s already registered", metricType))
	}
	registry[metricType] = collector
}

// GetCollector retrieves the collector factory function from the global registry for metricType.
// The returned factory function can be used to create new collector instances.
func GetCollector(metricType MetricType) (NewContinuousCollector, error) {
	collector, exists := registry[metricType]
	if !exists {
		return nil, fmt.Errorf("Collector for %s not found", metricType)
	}
	return collector, nil
}
