// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// HardwareGraphReceiver receives hardware information and builds a hardware graph
type HardwareGraphReceiver struct {
	logger      logr.Logger
	builder     *Builder
	nodeName    string
	clusterName string

	// Current collection data
	mu           sync.RWMutex
	currentData  map[string]any // Store data by type name
	lastSnapshot *performance.Snapshot
}

// NewHardwareGraphReceiver creates a new hardware graph receiver
func NewHardwareGraphReceiver(logger logr.Logger, builder *Builder, nodeName, clusterName string) *HardwareGraphReceiver {
	return &HardwareGraphReceiver{
		logger:      logger.WithName("hardware-graph-receiver"),
		builder:     builder,
		nodeName:    nodeName,
		clusterName: clusterName,
		currentData: make(map[string]any),
	}
}

// Accept processes hardware information from collectors
func (r *HardwareGraphReceiver) Accept(data any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Store data based on its type
	switch v := data.(type) {
	case *performance.CPUInfo:
		r.logger.V(2).Info("Accepting CPU info")
		r.currentData["cpu_info"] = v
	case *performance.MemoryInfo:
		r.logger.V(2).Info("Accepting memory info")
		r.currentData["memory_info"] = v
	case []performance.DiskInfo:
		r.logger.V(2).Info("Accepting disk info")
		r.currentData["disk_info"] = v
	case []performance.NetworkInfo:
		r.logger.V(2).Info("Accepting network info")
		r.currentData["network_info"] = v
	case *performance.NUMAStatistics:
		r.logger.V(2).Info("Accepting NUMA stats")
		r.currentData["numa_stats"] = v
	default:
		return fmt.Errorf("unsupported data type for hardware graph: %T", data)
	}

	return nil
}

// Name returns the receiver's name
func (r *HardwareGraphReceiver) Name() string {
	return "hardware-graph-receiver"
}

// BuildGraph builds the hardware graph from the currently collected data
func (r *HardwareGraphReceiver) BuildGraph(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Create a snapshot from the current data
	snapshot := &performance.Snapshot{
		Timestamp:   time.Now(),
		NodeName:    r.nodeName,
		ClusterName: r.clusterName,
		CollectorRun: performance.CollectorRunInfo{
			CollectorStats: make(map[performance.MetricType]performance.CollectorStat),
		},
		Metrics: performance.Metrics{},
	}

	// Populate snapshot with collected data
	if cpuInfo, ok := r.currentData["cpu_info"].(*performance.CPUInfo); ok {
		snapshot.Metrics.CPUInfo = cpuInfo
	}
	if memInfo, ok := r.currentData["memory_info"].(*performance.MemoryInfo); ok {
		snapshot.Metrics.MemoryInfo = memInfo
	}
	if diskInfo, ok := r.currentData["disk_info"].([]performance.DiskInfo); ok {
		snapshot.Metrics.DiskInfo = diskInfo
	}
	if netInfo, ok := r.currentData["network_info"].([]performance.NetworkInfo); ok {
		snapshot.Metrics.NetworkInfo = netInfo
	}
	if numaStats, ok := r.currentData["numa_stats"].(*performance.NUMAStatistics); ok {
		snapshot.Metrics.NUMAStats = numaStats
	}

	// Store the last snapshot
	r.lastSnapshot = snapshot

	// Build the hardware graph from the snapshot
	return r.builder.BuildFromSnapshot(ctx, snapshot)
}

// GetSnapshot returns the last built snapshot
func (r *HardwareGraphReceiver) GetSnapshot(ctx context.Context) (*performance.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.lastSnapshot == nil {
		return nil, fmt.Errorf("no snapshot available yet")
	}

	return r.lastSnapshot, nil
}

// Clear resets the collected data for a new collection cycle
func (r *HardwareGraphReceiver) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.currentData = make(map[string]any)
}
