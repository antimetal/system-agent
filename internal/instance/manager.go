// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package instance

import (
	"context"
	"time"

	"github.com/go-logr/logr"

	"github.com/antimetal/agent/internal/resource"
	"github.com/antimetal/agent/internal/runtime"
)

const (
	defaultRefreshInterval = 1 * time.Minute
)

// Manager handles periodic Instance resource publishing to CloudInventory
type Manager struct {
	logger   logr.Logger
	store    resource.Store
	interval time.Duration
}

// NewManager creates a new Instance manager
func NewManager(logger logr.Logger, store resource.Store) (*Manager, error) {
	return &Manager{
		logger:   logger,
		store:    store,
		interval: defaultRefreshInterval,
	}, nil
}

// Start implements controller-runtime's Runnable interface
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Instance manager starting", "refreshInterval", m.interval)

	// Initial publish
	if err := runtime.PublishInstance(ctx, m.store, m.logger); err != nil {
		m.logger.Error(err, "Failed initial instance publish, will retry periodically")
	}

	// Periodic refresh
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Instance manager stopped")
			return nil
		case <-ticker.C:
			if err := runtime.PublishInstance(ctx, m.store, m.logger); err != nil {
				m.logger.V(1).Error(err, "Failed to refresh instance resource")
			}
		}
	}
}

// NeedLeaderElection implements controller-runtime's LeaderElectionRunnable interface
// Instance publishing should happen on every agent, not just the leader
func (m *Manager) NeedLeaderElection() bool {
	return false
}
