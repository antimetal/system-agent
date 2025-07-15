// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/antimetal/agent/internal/instrumentor"
	"github.com/antimetal/agent/pkg/errors"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
)

// Manager coordinates collector registration and will eventually handle collection
type Manager struct {
	config            performance.CollectionConfig
	logger            logr.Logger
	registry          *performance.CollectorRegistry
	nodeName          string
	clusterName       string
	instrumentor      instrumentor.Reader
	runningCollectors map[performance.MetricType]performance.ContinuousCollector
}

type ManagerOptions struct {
	Config       performance.CollectionConfig
	Logger       logr.Logger
	NodeName     string
	ClusterName  string
	Instrumentor instrumentor.Reader
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

	if opts.Instrumentor == nil {
		opts.Instrumentor = &instrumentor.Static{}
	}

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

	registry := performance.NewCollectorRegistry(opts.Logger)
	err := registry.RegisterPoint(collectors.NewLoadCollector(opts.Logger, config))
	if err != nil {
		return nil, fmt.Errorf("failed to register load collector: %w", err)
	}

	m := &Manager{
		config:            config,
		logger:            opts.Logger.WithName("performance-manager"),
		registry:          registry,
		nodeName:          nodeName,
		clusterName:       opts.ClusterName,
		instrumentor:      opts.Instrumentor,
		runningCollectors: make(map[performance.MetricType]performance.ContinuousCollector),
	}

	return m, nil
}

func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("starting performance manager")

	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-ticker.C:
			m.updateCollectors(ctx)
		case <-ctx.Done():
			m.logger.Info("shutting down performance manager")
			ticker.Stop()
			err := m.stopAllCollectors()
			if err != nil {
				m.logger.Error(err, "failed to shutdown down performance manager")
			}
			return err
		}
	}
}

func (m *Manager) RegisterPointCollector(collector performance.PointCollector) error {
	return m.registry.RegisterPoint(collector)
}

func (m *Manager) RegisterContinuousCollector(collector performance.ContinuousCollector) error {
	return m.registry.RegisterContinuous(collector)
}

func (m *Manager) UnregisterCollector(metricType performance.MetricType) {
	m.registry.UnregisterCollector(metricType)
}

// GetRegistry returns the collector registry for inspection
func (m *Manager) GetRegistry() *performance.CollectorRegistry {
	return m.registry
}

// GetConfig returns the current configuration
func (m *Manager) GetConfig() performance.CollectionConfig {
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

func (m *Manager) updateCollectors(ctx context.Context) {
	// Load new config
	configData := m.instrumentor.GetEffectiveConfig()
	if configData == nil {
		m.logger.V(1).Info("no effective config data available from instrumentor")
		return
	}
	config := make(map[string]any)
	err := json.Unmarshal(configData, &config)
	if err != nil {
		m.logger.Error(err, "failed to unmarshal instrumentor config")
		return
	}

	colls, ok := config["collectors"].([]any)
	if !ok {
		m.logger.Error(fmt.Errorf("invalid collectors config format"), "expected array of strings", "collectors", config["collectors"])
		return
	}
	for _, mType := range colls {
		mStr, ok := mType.(string)
		if !ok {
			m.logger.Error(fmt.Errorf("invalid collector type format"), "expected string", "type", mType)
			continue
		}
		metricType := performance.MetricType(mStr)
		if _, exists := m.runningCollectors[metricType]; exists {
			continue // Already running
		}

		// Find collector in registry
		coll := m.registry.GetContinuous(metricType)
		if coll == nil {
			pointColl := m.registry.GetPoint(metricType)
			if pointColl == nil {
				m.logger.Error(fmt.Errorf("no collector registered"), "cannot start continuous collection", "type", metricType)
				continue
			}
			coll = performance.NewContinuousPointCollector(pointColl, m.config, m.logger)
		}

		// Start the collector
		m.logger.Info("starting continuous collector", "type", metricType)
		ch, err := coll.Start(ctx)
		if err != nil {
			m.logger.Error(err, "failed to start continuous collector", "type", metricType)
			continue
		}
		m.runningCollectors[metricType] = coll
		go func() {
			for data := range ch {
				// Handle collected data (e.g., send to intake service)
				m.logger.V(1).Info("collected data", "type", metricType, "data", data)
			}
		}()
	}
}

func (m *Manager) stopAllCollectors() error {
	var collectorErrs error
	for _, collector := range m.runningCollectors {
		if err := collector.Stop(); err != nil {
			m.logger.Error(err, "failed to stop continuous collector", "type", collector.Type())
			collectorErrs = errors.Join(collectorErrs, err)
		}
	}
	return collectorErrs
}

// TODO: Add methods for:
// - Starting/stopping collection based on external signals
// - Performing on-demand collection
// - Managing collector lifecycle
// - Integrating with BadgerDB for storage
// - Forwarding data to intake service
