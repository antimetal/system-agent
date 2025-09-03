//go:build !linux

// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/resource"
	"github.com/go-logr/logr"
)

// Manager coordinates runtime (container/process) discovery and graph building (stub implementation)
type Manager struct {
	logger      logr.Logger
	store       resource.Store
	perfManager *performance.Manager
	
	lastUpdate time.Time
	mu         sync.RWMutex
	
	// Metrics for monitoring discovery performance
	metrics *DiscoveryMetrics
}

// DiscoveryMetrics tracks performance metrics for runtime discovery
type DiscoveryMetrics struct {
	mu                     sync.RWMutex
	LastDiscoveryDuration  time.Duration
	LastContainerCount     int
	LastProcessCount       int
	LastDiscoveryErrors    int
	TotalDiscoveries       uint64
	TotalDiscoveryErrors   uint64
	LastDiscoveryTimestamp time.Time
}

type ManagerConfig struct {
	UpdateInterval     time.Duration
	Store              resource.Store
	PerformanceManager *performance.Manager
	CgroupPath         string
	EventBufferSize    int
	DebounceInterval   time.Duration
}

func NewManager(logger logr.Logger, config ManagerConfig) (*Manager, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("resource store is required")
	}
	if config.PerformanceManager == nil {
		return nil, fmt.Errorf("performance manager is required")
	}

	return &Manager{
		logger:      logger.WithName("runtime-manager-stub"),
		store:       config.Store,
		perfManager: config.PerformanceManager,
		metrics:     &DiscoveryMetrics{},
	}, nil
}

// Start begins runtime discovery and graph building (stub implementation)
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting runtime manager (stub implementation - no-op on non-Linux platforms)")
	
	// Just wait for context cancellation
	<-ctx.Done()
	return nil
}

// Stop gracefully stops the manager (stub implementation)
func (m *Manager) Stop() error {
	m.logger.Info("Stopping runtime manager (stub implementation)")
	return nil
}

// GetLastUpdateTime returns the last update time
func (m *Manager) GetLastUpdateTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastUpdate
}

// ForceUpdate forces a full reconciliation (stub implementation)
func (m *Manager) ForceUpdate(ctx context.Context) error {
	m.logger.Info("ForceUpdate called (stub implementation - no-op)")
	return nil
}

// GetMetrics returns a copy of the current discovery metrics (stub implementation)
func (m *Manager) GetMetrics() DiscoveryMetrics {
	m.metrics.mu.RLock()
	defer m.metrics.mu.RUnlock()

	return DiscoveryMetrics{
		LastDiscoveryDuration:  0,
		LastContainerCount:     0,
		LastProcessCount:       0,
		LastDiscoveryErrors:    0,
		TotalDiscoveries:       0,
		TotalDiscoveryErrors:   0,
		LastDiscoveryTimestamp: time.Time{},
	}
}