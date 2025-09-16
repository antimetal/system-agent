// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"google.golang.org/protobuf/proto"

	"github.com/antimetal/agent/internal/config"
	"github.com/antimetal/agent/internal/metrics"
	agentv1 "github.com/antimetal/agent/pkg/api/antimetal/agent/v1"
	"github.com/antimetal/agent/pkg/channel"
	"github.com/antimetal/agent/pkg/config/environment"
	"github.com/antimetal/agent/pkg/performance"
	// register all collectors to the registry
	_ "github.com/antimetal/agent/pkg/performance/collectors"
)

type collectorInstance struct {
	version string
	cancel  context.CancelFunc
}

type manager struct {
	wg                  sync.WaitGroup
	logger              logr.Logger
	configLoader        config.Loader
	router              metrics.Router
	perfEvents          *channel.Merger[performance.Event]
	runningCollectors   map[string]*collectorInstance
	runningCollectorsMu sync.RWMutex
	procPath            string
	sysPath             string
	devPath             string
}

type Option func(m *manager)

func WithLogger(logger logr.Logger) Option {
	return func(m *manager) {
		m.logger = logger
	}
}

func WithProcPath(path string) Option {
	return func(m *manager) {
		m.procPath = path
	}
}

func WithSysPath(path string) Option {
	return func(m *manager) {
		m.sysPath = path
	}
}

func WithDevPath(path string) Option {
	return func(m *manager) {
		m.devPath = path
	}
}

func New(configLoader config.Loader, router metrics.Router, opts ...Option) (*manager, error) {
	if configLoader == nil {
		return nil, fmt.Errorf("configLoader cannot be nil")
	}
	if router == nil {
		return nil, fmt.Errorf("router cannot be nil")
	}

	perfEvents := channel.NewMerger[performance.Event]()

	// Get host paths from environment
	hostPaths := environment.GetHostPaths()

	m := &manager{
		configLoader:      configLoader,
		router:            router,
		perfEvents:        perfEvents,
		runningCollectors: make(map[string]*collectorInstance),
		procPath:          hostPaths.Proc,
		sysPath:           hostPaths.Sys,
		devPath:           hostPaths.Dev,
	}
	for _, opt := range opts {
		opt(m)
	}

	m.logger = m.logger.WithName("perf")

	return m, nil
}

func (m *manager) GetRunningCollectors() map[string]string {
	m.runningCollectorsMu.RLock()
	defer m.runningCollectorsMu.RUnlock()

	collectors := make(map[string]string)
	for name, instance := range m.runningCollectors {
		collectors[name] = instance.version
	}
	return collectors
}

func (m *manager) Start(ctx context.Context) error {
	m.logger.Info("starting manager")

	m.wg.Add(1)
	go m.eventCollector(ctx)

	m.wg.Add(1)
	go m.collectorManager(ctx)

	m.wg.Wait()

	m.logger.Info("shutting down manager")

	m.perfEvents.Close()

	return nil
}

// Implements sigs.k8s.io/controller-runtime/pkg/manager.LeaderElectionRunnable interface
// Always returns false to disable leader election.
func (m *manager) NeedLeaderElection() bool {
	return false
}

func (m *manager) eventCollector(ctx context.Context) {
	defer m.wg.Done()

	m.logger.V(1).Info("starting event collector")

	for {
		select {
		case <-ctx.Done():
			m.logger.V(1).Info("stopping event collector")
			return
		case event, ok := <-m.perfEvents.Out():
			if !ok {
				m.logger.V(1).Info("stopping event collector")
				return
			}

			metricEvent := metrics.MetricEvent{
				Timestamp:  time.Now(),
				Source:     "performance-collector",
				MetricType: metrics.MetricType(event.Metric),
				Data:       event.Data,
			}

			if err := m.router.Publish(metricEvent); err != nil {
				m.logger.Error(err, "failed to publish metric event", "metricType", event.Metric)
			}
		}
	}
}

func (m *manager) collectorManager(ctx context.Context) {
	defer m.wg.Done()

	m.logger.V(1).Info("starting collector manager")

	configs := m.configLoader.Watch(config.Options{
		Filters: config.Filters{
			Types: []string{string(proto.MessageName(&agentv1.HostStatsCollectionConfig{}))},
		},
	})

	for {
		select {
		case <-ctx.Done():
			m.logger.V(1).Info("stopping all collectors")
			m.stopAllCollectors()
			return
		case config := <-configs:
			m.logger.V(1).Info("received config instance",
				"name", config.Name,
				"version", config.Version,
				"expired", config.Expired)

			if config.Expired {
				m.handleExpiredConfig(config)
			} else {
				m.handleActiveConfig(ctx, config)
			}
		}
	}
}

func (m *manager) stopAllCollectors() {
	m.runningCollectorsMu.Lock()
	defer m.runningCollectorsMu.Unlock()

	m.logger.V(1).Info("stopping all collectors", "count", len(m.runningCollectors))

	for name, instance := range m.runningCollectors {
		m.logger.V(1).Info("stopping collector", "name", name)
		instance.cancel()
	}

	clear(m.runningCollectors)
}

func (m *manager) handleExpiredConfig(config config.Instance) {
	collectorKey := config.Name

	m.runningCollectorsMu.Lock()
	defer m.runningCollectorsMu.Unlock()

	if instance, exists := m.runningCollectors[collectorKey]; exists {
		m.logger.Info("stopping collector", "name", collectorKey)
		instance.cancel()
		delete(m.runningCollectors, collectorKey)
	} else {
		m.logger.V(1).Info("received expired config for non-running collector", "name", collectorKey)
	}
}

func (m *manager) handleActiveConfig(ctx context.Context, config config.Instance) {
	collectorKey := config.Name

	m.runningCollectorsMu.Lock()
	defer m.runningCollectorsMu.Unlock()

	instance, exists := m.runningCollectors[collectorKey]
	if exists {
		if instance.version == config.Version {
			m.logger.V(1).Info("ignoring config update with same version",
				"name", collectorKey, "version", config.Version)
			return
		}

		m.logger.Info("updating collector", "name", collectorKey, "version", config.Version)
		instance.cancel()
		delete(m.runningCollectors, collectorKey)
	} else {
		m.logger.Info("starting new collector", "name", collectorKey, "version", config.Version)
	}

	cancel, err := m.startCollector(ctx, config)
	if err != nil {
		m.logger.Error(err, "failed to start collector", "name", collectorKey, "version", config.Version)
		return
	}

	m.runningCollectors[collectorKey] = &collectorInstance{
		version: config.Version,
		cancel:  cancel,
	}
}

func (m *manager) startCollector(ctx context.Context, config config.Instance) (context.CancelFunc, error) {
	configObj, ok := config.Object.(*agentv1.HostStatsCollectionConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type: expected HostStatsCollectionConfig, got %T", config.Object)
	}

	collectorName := configObj.GetCollector()
	if collectorName == "" {
		return nil, fmt.Errorf("collector field is empty in config")
	}
	metricType := performance.MetricType(collectorName)

	collectionConfig := performance.CollectionConfig{
		HostProcPath: m.procPath,
		HostSysPath:  m.sysPath,
		HostDevPath:  m.devPath,
		Interval:     time.Duration(configObj.GetIntervalSeconds()) * time.Second,
	}
	collectionConfig.ApplyDefaults()

	collector, err := performance.GetCollector(metricType)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s collector: %w", metricType, err)
	}

	collectorLogger := m.logger.WithName(config.Name)
	c, err := collector(collectorLogger, collectionConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s collector instance: %w", metricType, err)
	}

	collectorCtx, cancel := context.WithCancel(ctx)

	events, err := c.Start(collectorCtx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start %s collector: %w", metricType, err)
	}

	m.perfEvents.Add(events)

	return cancel, nil
}
