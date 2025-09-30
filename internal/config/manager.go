// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package config

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
)

// ManagerOption configures Manager
type ManagerOption func(m *Manager)

// WithLoader configures Manager Loader with loader
func WithLoader(loader Loader) ManagerOption {
	return func(m *Manager) {
		m.loader = loader
	}
}

// WithLogger configures Manager logger
func WithLogger(logger logr.Logger) ManagerOption {
	return func(m *Manager) {
		m.logger = logger
	}
}

// NewManager creates a new Manager
func NewManager(opts ...ManagerOption) (*Manager, error) {
	m := &Manager{}

	for _, opt := range opts {
		opt(m)
	}

	if m.loader == nil {
		loader, err := getDefaultLoader(m.logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create config loader: %w", err)
		}
		m.loader = loader
	}

	m.logger = m.logger.WithName("config.manager")

	return m, nil
}

// Manager implements controller-runtime's manager.Runnable interface
// and acts as a passthrough to a single configured Loader
type Manager struct {
	loader Loader
	logger logr.Logger
}

// Start implements manager.Runnable interface.
// This starts Manager and blocks until the context is cancelled
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("starting config manager")

	<-ctx.Done()

	m.logger.Info("config manager stopping due to context cancellation")

	if closer, ok := m.loader.(LoaderCloser); ok {
		return closer.Close()
	}
	return nil
}

// Implements sigs.k8s.io/controller-runtime/pkg/manager.LeaderElectionRunnable interface
// Always returns false to disable leader election.
func (m *Manager) NeedLeaderElection() bool {
	return false
}

// ListConfigs retrieves available configs with optional filters.
//
// If a new version of a config is received that is invalid, it will
// return the most recent valid config, otherwise it will return the
// current config with StatusInvalid.
func (m *Manager) ListConfigs(opts Options) (map[string][]Instance, error) {
	return m.loader.ListConfigs(opts)
}

// GetConfig gets a config object identified as name of type configType.
// It returns an error if no config is found.
//
// If a new version of a config is received that is invalid, it will
// return the most recent valid config, otherwise it will return the
// current config with StatusInvalid.
func (m *Manager) GetConfig(configType, name string) (Instance, error) {
	return m.loader.GetConfig(configType, name)
}

// Watch returns a channel that receives configuration objects as they change.
// Each invocation of Watch returns a separate channel instance.
//
// Instances received on the channel supports AT LEAST ONCE semantics;
// duplicate Instances may be received on the channel. The client
// is responsible for handling potentially duplicate Instances.
//
// The channel will not send Instances with a Version lower than a
// previously received Instance with the same TypeUrl and Name.
//
// The channel will be closed once the Manager terminates.
func (m *Manager) Watch(opts Options) <-chan Instance {
	return m.loader.Watch(opts)
}
