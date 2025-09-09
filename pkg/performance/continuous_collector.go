// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
)

// ContinuousCollectionConfig configures continuous metric collection
type ContinuousCollectionConfig struct {
	// Interval between collections
	Interval time.Duration
	// MetricTypes to collect (if empty, collects all available)
	MetricTypes []MetricType
}

// activeCollector holds a collector's metadata and channel
type activeCollector struct {
	metricType MetricType
	channel    <-chan any
}

// CollectAllMetrics starts continuous collection of all available performance metrics.
// It returns immediately after starting collectors. The collection runs in a background
// goroutine until the context is cancelled.
func (m *Manager) CollectAllMetrics(ctx context.Context, config ContinuousCollectionConfig) error {
	// Default to all continuous metrics if none specified
	metricTypes := config.MetricTypes
	if len(metricTypes) == 0 {
		metricTypes = []MetricType{
			MetricTypeCPU,
			MetricTypeMemory,
			MetricTypeDisk,
			MetricTypeNetwork,
			MetricTypeSystem,
			MetricTypeLoad,
			MetricTypeTCP,
			MetricTypeProcess,
		}
	}

	// Use provided interval or default
	interval := config.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}

	// Update config with interval
	collectorConfig := m.config
	collectorConfig.Interval = interval

	collectorLogger := m.logger.WithName("continuous-collectors")

	var activeCollectors []activeCollector
	var collectorNames []string

	// Create and start collectors from the registry
	for _, metricType := range metricTypes {
		// Get collector factory from registry
		factory, err := GetCollector(metricType)
		if err != nil {
			// Check if it's unavailable due to platform constraints
			available, reason := GetCollectorStatus(metricType)
			if !available {
				collectorLogger.V(1).Info("Collector not available",
					"metric_type", metricType, "reason", reason)
			} else {
				collectorLogger.Error(err, "Failed to get collector from registry",
					"metric_type", metricType)
			}
			continue
		}

		// Create the continuous collector instance
		collector, err := factory(collectorLogger.WithName(string(metricType)), collectorConfig)
		if err != nil {
			collectorLogger.Error(err, "Failed to create collector",
				"metric_type", metricType)
			continue
		}

		// Start the collector
		dataChan, err := collector.Start(ctx)
		if err != nil {
			collectorLogger.Error(err, "Failed to start collector",
				"metric_type", metricType)
			continue
		}

		activeCollectors = append(activeCollectors, activeCollector{
			metricType: metricType,
			channel:    dataChan,
		})
		collectorNames = append(collectorNames, string(metricType))
	}

	if len(activeCollectors) == 0 {
		return fmt.Errorf("no collectors could be started")
	}

	// Start a goroutine to handle data from all collectors
	go m.handleCollectorData(ctx, activeCollectors, collectorLogger)

	collectorLogger.Info("Started continuous collectors",
		"collectors", collectorNames,
		"interval", interval)

	return nil
}

// handleCollectorData reads from collector channels and publishes data
func (m *Manager) handleCollectorData(ctx context.Context, collectors []activeCollector, logger logr.Logger) {
	// Since we know our collector types at compile time, we can use a static select.
	// We'll set up channels for each known collector type, using nil for unavailable ones.
	var (
		cpuChan     <-chan any
		memoryChan  <-chan any
		diskChan    <-chan any
		networkChan <-chan any
		systemChan  <-chan any
		loadChan    <-chan any
		tcpChan     <-chan any
		processChan <-chan any
	)

	// Map collectors to their specific channels
	for _, c := range collectors {
		switch c.metricType {
		case MetricTypeCPU:
			cpuChan = c.channel
		case MetricTypeMemory:
			memoryChan = c.channel
		case MetricTypeDisk:
			diskChan = c.channel
		case MetricTypeNetwork:
			networkChan = c.channel
		case MetricTypeSystem:
			systemChan = c.channel
		case MetricTypeLoad:
			loadChan = c.channel
		case MetricTypeTCP:
			tcpChan = c.channel
		case MetricTypeProcess:
			processChan = c.channel
		}
	}

	// Static select loop - handles all known collector channels
	for {
		select {
		case <-ctx.Done():
			logger.Info("Context cancelled, stopping collector handler")
			return

		case data, ok := <-cpuChan:
			if ok && data != nil {
				if err := m.PublishCollectorData(MetricTypeCPU, data); err != nil {
					logger.Error(err, "Failed to publish CPU data")
				} else {
					logger.V(2).Info("Published CPU data")
				}
			} else if !ok && cpuChan != nil {
				logger.V(1).Info("CPU collector channel closed")
				cpuChan = nil
			}

		case data, ok := <-memoryChan:
			if ok && data != nil {
				if err := m.PublishCollectorData(MetricTypeMemory, data); err != nil {
					logger.Error(err, "Failed to publish memory data")
				} else {
					logger.V(2).Info("Published memory data")
				}
			} else if !ok && memoryChan != nil {
				logger.V(1).Info("Memory collector channel closed")
				memoryChan = nil
			}

		case data, ok := <-diskChan:
			if ok && data != nil {
				if err := m.PublishCollectorData(MetricTypeDisk, data); err != nil {
					logger.Error(err, "Failed to publish disk data")
				} else {
					logger.V(2).Info("Published disk data")
				}
			} else if !ok && diskChan != nil {
				logger.V(1).Info("Disk collector channel closed")
				diskChan = nil
			}

		case data, ok := <-networkChan:
			if ok && data != nil {
				if err := m.PublishCollectorData(MetricTypeNetwork, data); err != nil {
					logger.Error(err, "Failed to publish network data")
				} else {
					logger.V(2).Info("Published network data")
				}
			} else if !ok && networkChan != nil {
				logger.V(1).Info("Network collector channel closed")
				networkChan = nil
			}

		case data, ok := <-systemChan:
			if ok && data != nil {
				if err := m.PublishCollectorData(MetricTypeSystem, data); err != nil {
					logger.Error(err, "Failed to publish system data")
				} else {
					logger.V(2).Info("Published system data")
				}
			} else if !ok && systemChan != nil {
				logger.V(1).Info("System collector channel closed")
				systemChan = nil
			}

		case data, ok := <-loadChan:
			if ok && data != nil {
				if err := m.PublishCollectorData(MetricTypeLoad, data); err != nil {
					logger.Error(err, "Failed to publish load data")
				} else {
					logger.V(2).Info("Published load data")
				}
			} else if !ok && loadChan != nil {
				logger.V(1).Info("Load collector channel closed")
				loadChan = nil
			}

		case data, ok := <-tcpChan:
			if ok && data != nil {
				if err := m.PublishCollectorData(MetricTypeTCP, data); err != nil {
					logger.Error(err, "Failed to publish TCP data")
				} else {
					logger.V(2).Info("Published TCP data")
				}
			} else if !ok && tcpChan != nil {
				logger.V(1).Info("TCP collector channel closed")
				tcpChan = nil
			}

		case data, ok := <-processChan:
			if ok && data != nil {
				if err := m.PublishCollectorData(MetricTypeProcess, data); err != nil {
					logger.Error(err, "Failed to publish process data")
				} else {
					logger.V(2).Info("Published process data")
				}
			} else if !ok && processChan != nil {
				logger.V(1).Info("Process collector channel closed")
				processChan = nil
			}
		}
	}
}
