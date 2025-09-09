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

	"github.com/antimetal/agent/internal/metrics"
	"github.com/go-logr/logr"
)

// Manager coordinates collector registration and handles performance collection
type Manager struct {
	config      CollectionConfig
	logger      logr.Logger
	nodeName    string
	clusterName string
	router      metrics.Router
}

type ManagerOptions struct {
	Config        CollectionConfig
	Logger        logr.Logger
	NodeName      string
	ClusterName   string
	MetricsRouter metrics.Router // Optional metrics router
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

	if opts.MetricsRouter != nil {
		m.router = opts.MetricsRouter
		m.logger.Info("Metrics router enabled for performance manager")
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
func (m *Manager) PublishCollectorData(metricType MetricType, data any) error {
	if m.router == nil {
		return nil // Silently ignore if no router
	}

	event := metrics.MetricEvent{
		Timestamp:   time.Now(),
		Source:      "performance-collector",
		NodeName:    m.nodeName,
		ClusterName: m.clusterName,
		MetricType:  metrics.MetricType(metricType), // Convert performance.MetricType to metrics.MetricType (duplicated to avoid import cycle)
		EventType:   determineEventType(metricType),
		Data:        data,
	}

	if err := m.router.Publish(event); err != nil {
		m.logger.Error(err, "Failed to publish collector data", "metric_type", metricType)
		return err
	}

	m.logger.V(2).Info("Published collector data", "metric_type", metricType)
	return nil
}

// HasMetricsRouter returns true if the manager has a metrics router configured
func (m *Manager) HasMetricsRouter() bool {
	return m.router != nil
}

// determineEventType maps metric types to appropriate event types
func determineEventType(metricType MetricType) metrics.EventType {
	switch metricType {
	case MetricTypeLoad, MetricTypeMemory, MetricTypeCPU,
		MetricTypeDisk, MetricTypeNetwork, MetricTypeProcess,
		MetricTypeCPUInfo, MetricTypeMemoryInfo, MetricTypeDiskInfo,
		MetricTypeNetworkInfo, MetricTypeNUMAStats:
		return metrics.EventTypeGauge
	case MetricTypeSystem, MetricTypeTCP, MetricTypeKernel:
		return metrics.EventTypeCounter
	default:
		return metrics.EventTypeGauge
	}
}
