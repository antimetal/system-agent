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

// Manager coordinates collector registration and will eventually handle collection
type Manager struct {
	config      CollectionConfig
	logger      logr.Logger
	nodeName    string
	clusterName string
}

type ManagerOptions struct {
	Config      CollectionConfig
	Logger      logr.Logger
	NodeName    string
	ClusterName string
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
	if os.Getenv("HOST_CGROUP") != "" {
		config.HostCgroupPath = os.Getenv("HOST_CGROUP")
	}

	m := &Manager{
		config:      config,
		logger:      opts.Logger.WithName("performance-manager"),
		nodeName:    nodeName,
		clusterName: opts.ClusterName,
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

// TODO: Add methods for:
// - Starting/stopping collection based on external signals
// - Performing on-demand collection
// - Managing collector lifecycle
// - Integrating with BadgerDB for storage
// - Forwarding data to intake service
