// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"fmt"
	"os"

	"github.com/go-logr/logr"
)

// Manager coordinates collector registration and handles performance collection
type Manager struct {
	config      CollectionConfig
	logger      logr.Logger
	nodeName    string
	clusterName string
	receiver    Receiver // Optional receiver for metrics
}

type ManagerOptions struct {
	Config          CollectionConfig
	Logger          logr.Logger
	NodeName        string
	ClusterName     string
	MetricsReceiver Receiver // Optional metrics receiver
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

	if opts.MetricsReceiver != nil {
		m.receiver = opts.MetricsReceiver
		m.logger.Info("Metrics receiver enabled for performance manager")
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
	if m.receiver == nil {
		return nil // Silently ignore if no receiver
	}

	// The receiver will handle creating the event and routing it
	if err := m.receiver.Accept(data); err != nil {
		m.logger.Error(err, "Failed to publish collector data", "metric_type", metricType)
		return err
	}

	m.logger.V(2).Info("Published collector data", "metric_type", metricType)
	return nil
}

// HasMetricsReceiver returns true if the manager has a metrics receiver configured
func (m *Manager) HasMetricsReceiver() bool {
	return m.receiver != nil
}
