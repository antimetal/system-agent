// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tracker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// PollingTracker implements RuntimeTracker using traditional polling approach.
// This wraps the existing containers.Discovery and integrates with performance collectors.
type PollingTracker struct {
	logger    logr.Logger
	discovery *containers.Discovery
	config    TrackerConfig

	// Periodic update management
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Current state
	mu             sync.RWMutex
	lastSnapshot   *RuntimeSnapshot
	lastUpdateTime time.Time

	// Event channel (always nil for polling tracker)
	events chan RuntimeEvent
}

// NewPollingTracker creates a new polling-based runtime tracker
func NewPollingTracker(logger logr.Logger, config TrackerConfig) (*PollingTracker, error) {
	// Validate configuration
	if config.UpdateInterval <= 0 {
		config.UpdateInterval = 30 * time.Second
	}
	if config.CgroupPath == "" {
		config.CgroupPath = "/sys/fs/cgroup"
	}

	discovery := containers.NewDiscovery(config.CgroupPath)

	return &PollingTracker{
		logger:    logger.WithName("polling-tracker"),
		discovery: discovery,
		config:    config,
		events:    nil, // Polling tracker doesn't provide events
	}, nil
}

// Start begins polling for runtime changes
func (p *PollingTracker) Start(ctx context.Context) error {
	p.logger.Info("Starting polling runtime tracker", "interval", p.config.UpdateInterval)

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	// Do initial snapshot
	if err := p.updateSnapshot(); err != nil {
		p.logger.Error(err, "Failed initial runtime snapshot")
		// Don't fail startup on initial snapshot error
	}

	// Start periodic updates
	p.wg.Add(1)
	go p.runPeriodicUpdates(ctx)

	return nil
}

// Stop stops the polling tracker
func (p *PollingTracker) Stop() error {
	p.logger.Info("Stopping polling runtime tracker")
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	return nil
}

// GetSnapshot returns the current runtime state
func (p *PollingTracker) GetSnapshot() (*RuntimeSnapshot, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.lastSnapshot == nil {
		// If no snapshot yet, do a fresh collection
		p.mu.RUnlock()
		err := p.updateSnapshot()
		p.mu.RLock()
		if err != nil {
			return nil, fmt.Errorf("failed to collect runtime snapshot: %w", err)
		}
	}

	// Return a copy to avoid race conditions
	snapshot := &RuntimeSnapshot{
		Timestamp:    p.lastSnapshot.Timestamp,
		Containers:   make([]containers.Container, len(p.lastSnapshot.Containers)),
		ProcessStats: p.lastSnapshot.ProcessStats,
	}
	copy(snapshot.Containers, p.lastSnapshot.Containers)

	return snapshot, nil
}

// Events returns a channel of runtime events (always nil for polling)
func (p *PollingTracker) Events() <-chan RuntimeEvent {
	return p.events
}

// IsEventDriven returns false for polling tracker
func (p *PollingTracker) IsEventDriven() bool {
	return false
}

// runPeriodicUpdates runs the polling loop
func (p *PollingTracker) runPeriodicUpdates(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.updateSnapshot(); err != nil {
				p.logger.Error(err, "Failed to update runtime snapshot")
			}
		}
	}
}

// updateSnapshot collects a fresh runtime snapshot
func (p *PollingTracker) updateSnapshot() error {
	startTime := time.Now()

	// Discover all containers
	allContainers, err := p.discovery.DiscoverAllContainers()
	if err != nil {
		p.logger.Error(err, "Failed to discover containers")
		// Continue with empty containers list rather than failing completely
		allContainers = []containers.Container{}
	}

	p.logger.V(1).Info("Discovered containers", "count", len(allContainers))

	// For now, we'll use a placeholder for process information
	// In a full implementation, this would integrate with performance collectors
	processSnapshot := &performance.ProcessSnapshot{
		Timestamp: startTime,
		Processes: []performance.ProcessStats{},
	}

	snapshot := &RuntimeSnapshot{
		Timestamp:    startTime,
		Containers:   allContainers,
		ProcessStats: processSnapshot,
	}

	// Update stored snapshot
	p.mu.Lock()
	p.lastSnapshot = snapshot
	p.lastUpdateTime = startTime
	p.mu.Unlock()

	p.logger.V(1).Info("Runtime snapshot updated",
		"duration", time.Since(startTime),
		"containers", len(allContainers))

	return nil
}

// GetLastUpdateTime returns when the snapshot was last updated
func (p *PollingTracker) GetLastUpdateTime() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastUpdateTime
}