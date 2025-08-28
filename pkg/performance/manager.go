// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"fmt"
	"os"
	"time"

	"github.com/antimetal/agent/pkg/metrics"
	"github.com/go-logr/logr"
)

// Manager coordinates collector registration and handles performance collection
type Manager struct {
	config      CollectionConfig
	logger      logr.Logger
	nodeName    string
	clusterName string

	// Direct metrics publisher (no wrapper needed)
	publisher metrics.Publisher
}

type ManagerOptions struct {
	Config           CollectionConfig
	Logger           logr.Logger
	NodeName         string
	ClusterName      string
	MetricsPublisher metrics.Publisher // Optional metrics publisher
}

func NewManager(opts ManagerOptions) (*Manager, error) {
	if opts.Logger.GetSink() == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Get node name from environment if not provided
	nodeName := opts.NodeName
	if nodeName == "" {
		nodeName = os.Getenv("NODE_NAME")
		if nodeName == "" {
			hostname, err := os.Hostname()
			if err != nil {
				return nil, fmt.Errorf("failed to get hostname: %w", err)
			}
			nodeName = hostname
		}
	}

	// Apply defaults to config
	config := opts.Config
	config.ApplyDefaults()

	// Override paths for containerized environments
	if os.Getenv("HOST_PROC") != "" {
		config.HostProcPath = os.Getenv("HOST_PROC")
	}
	if os.Getenv("HOST_SYS") != "" {
		config.HostSysPath = os.Getenv("HOST_SYS")
	}
	if os.Getenv("HOST_DEV") != "" {
		config.HostDevPath = os.Getenv("HOST_DEV")
	}

	m := &Manager{
		config:      config,
		logger:      opts.Logger.WithName("performance-manager"),
		nodeName:    nodeName,
		clusterName: opts.ClusterName,
	}

	// Store the metrics publisher directly
	if opts.MetricsPublisher != nil {
		m.publisher = opts.MetricsPublisher
		m.logger.Info("Metrics publisher enabled for performance manager")
	}

	return m, nil
}

// GetConfig returns the current configuration
func (m *Manager) GetConfig() CollectionConfig {
	return m.config
}

// GetNodeName returns the node name
func (m *Manager) GetNodeName() string {
	return m.nodeName
}

// GetClusterName returns the cluster name
func (m *Manager) GetClusterName() string {
	return m.clusterName
}

// PublishCollectorData publishes data from a specific collector
func (m *Manager) PublishCollectorData(metricType metrics.MetricType, data any) error {
	if m.publisher == nil {
		return nil // Silently ignore if no publisher
	}

	// Create the event directly - no need for a wrapper
	event := metrics.MetricEvent{
		Timestamp:   time.Now(),
		Source:      "performance-collector",
		NodeName:    m.nodeName,
		ClusterName: m.clusterName,
		MetricType:  metricType,
		EventType:   determineEventType(metricType),
		Data:        data,
	}

	if err := m.publisher.Publish(event); err != nil {
		m.logger.Error(err, "Failed to publish collector data", "metric_type", metricType)
		return err
	}

	m.logger.V(2).Info("Published collector data", "metric_type", metricType)
	return nil
}

// HasMetricsPublisher returns true if the manager has a metrics publisher configured
func (m *Manager) HasMetricsPublisher() bool {
	return m.publisher != nil
}

// determineEventType maps metric types to appropriate event types
func determineEventType(metricType metrics.MetricType) metrics.EventType {
	switch metricType {
	case metrics.MetricTypeLoad, metrics.MetricTypeMemory, metrics.MetricTypeCPU,
		metrics.MetricTypeDisk, metrics.MetricTypeNetwork, metrics.MetricTypeProcess,
		metrics.MetricTypeCPUInfo, metrics.MetricTypeMemoryInfo, metrics.MetricTypeDiskInfo,
		metrics.MetricTypeNetworkInfo, metrics.MetricTypeNUMAStats:
		return metrics.EventTypeGauge
	case metrics.MetricTypeSystem, metrics.MetricTypeTCP, metrics.MetricTypeKernel:
		return metrics.EventTypeCounter
	default:
		return metrics.EventTypeGauge
	}
}
