// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux
// +build linux

package collectors

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/go-logr/logr"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-I../../../ebpf/include -Wall -Werror -g -O2 -D__TARGET_ARCH_x86 -fdebug-types-section -fno-stack-protector" -target bpfel memgrowth ../../../ebpf/src/memgrowth.bpf.c -- -I../../../ebpf/include

func init() {
	performance.Register(performance.MetricTypeProcessMemoryGrowth,
		func(logger logr.Logger, config performance.CollectionConfig) (performance.ContinuousCollector, error) {
			return NewMemoryGrowthEBPFCollector(logger, config)
		},
	)
}

// Compile-time interface check
var _ performance.ContinuousCollector = (*MemoryGrowthEBPFCollector)(nil)

// ProcessMemoryGrowthConfig contains configuration for memory growth monitoring
type ProcessMemoryGrowthConfig struct {
	GrowthRateThreshold uint64        // bytes/second - alert if growth exceeds this
	MinProcessAge       time.Duration // minimum age before tracking growth
	MinRSSForTracking   uint64        // minimum RSS to start tracking (bytes)
	ConfidenceThreshold uint8         // minimum confidence for alerts (0-100)
}

// ProcessMemoryState tracks memory usage for a single process
// All historical tracking is done in the eBPF program in kernel space
type ProcessMemoryState struct {
	PID        int32
	Name       string
	StartTime  time.Time
	LastUpdate time.Time
	CurrentRSS uint64
	PeakRSS    uint64
	InitialRSS uint64

	// Growth analysis (calculated by eBPF)
	GrowthRate     uint64 // bytes/second
	TotalGrowth    uint64 // bytes since start
	LeakConfidence uint8  // 0-100 confidence this is a leak
	PatternType    string // "stable", "linear", "accelerating", "oscillating"

	// Memory breakdown (from eBPF)
	HeapSize  uint64
	StackSize uint64
	TotalVM   uint64
}

// ProcessMemoryGrowthStats aggregates memory growth statistics across all processes
type ProcessMemoryGrowthStats struct {
	Timestamp             time.Time
	MonitoredProcessCount uint32
	GrowingProcessCount   uint32
	HighRiskProcessCount  uint32
	TotalGrowthRate       uint64
	ProcessDetails        []ProcessMemoryState
}

// MemoryGrowthEBPFCollector uses eBPF for real-time memory growth detection
type MemoryGrowthEBPFCollector struct {
	performance.BaseContinuousCollector

	// eBPF objects
	objs   *memgrowthObjects
	links  []link.Link
	reader *ringbuf.Reader

	// Configuration
	config ProcessMemoryGrowthConfig

	// State tracking
	mu            sync.RWMutex
	processStates map[uint32]*ProcessMemoryState

	// Channel management
	ch      chan any
	stopped chan struct{}
	wg      sync.WaitGroup
}

func NewMemoryGrowthEBPFCollector(logger logr.Logger, config performance.CollectionConfig) (*MemoryGrowthEBPFCollector, error) {

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    false,
		SupportsContinuous: true,
		RequiresRoot:       true,
		RequiresEBPF:       true,
		MinKernelVersion:   "5.8.0",
	}

	// Default configuration
	growthConfig := ProcessMemoryGrowthConfig{
		GrowthRateThreshold: 1024 * 1024, // 1MB/second
		MinProcessAge:       30 * time.Second,
		MinRSSForTracking:   10 * 1024 * 1024, // 10MB minimum
		ConfidenceThreshold: 60,
	}

	return &MemoryGrowthEBPFCollector{
		BaseContinuousCollector: performance.NewBaseContinuousCollector(
			performance.MetricTypeProcessMemoryGrowth,
			"Memory Growth eBPF Collector",
			logger,
			config,
			capabilities,
		),
		config:        growthConfig,
		processStates: make(map[uint32]*ProcessMemoryState),
	}, nil
}

func (c *MemoryGrowthEBPFCollector) Start(ctx context.Context) (<-chan any, error) {
	if c.Status() != performance.CollectorStatusDisabled {
		return nil, fmt.Errorf("collector already running")
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load eBPF program
	spec, err := loadMemgrowth()
	if err != nil {
		return nil, fmt.Errorf("failed to load eBPF spec: %w", err)
	}

	objs := memgrowthObjects{}
	if err := spec.LoadAndAssign(&objs, nil); err != nil {
		return nil, fmt.Errorf("failed to load eBPF objects: %w", err)
	}
	c.objs = &objs

	// Configure eBPF program
	configKey := uint32(0)
	config := memgrowthMemgrowthConfig{
		MinRssThreshold:     c.config.MinRSSForTracking,
		GrowthRateThreshold: c.config.GrowthRateThreshold,
		MinProcessAge:       uint32(c.config.MinProcessAge.Milliseconds()),
		SampleInterval:      1000, // 1 second
		ConfidenceThreshold: c.config.ConfidenceThreshold,
		Enabled:             1,
	}

	if err := objs.ConfigMap.Put(configKey, config); err != nil {
		objs.Close()
		return nil, fmt.Errorf("failed to configure eBPF program: %w", err)
	}

	// Attach tracepoints
	// 1. RSS stat changes for exact memory tracking
	tpRssStat, err := link.Tracepoint("kmem", "rss_stat", objs.TraceRssStat, nil)
	if err != nil {
		objs.Close()
		return nil, fmt.Errorf("failed to attach rss_stat tracepoint: %w", err)
	}
	c.links = append(c.links, tpRssStat)

	// 2. Process exit for cleanup
	tpProcessExit, err := link.Tracepoint("sched", "sched_process_exit", objs.TraceProcessExit, nil)
	if err != nil {
		objs.Close()
		return nil, fmt.Errorf("failed to attach process_exit tracepoint: %w", err)
	}
	c.links = append(c.links, tpProcessExit)

	// Create ring buffer reader
	c.reader, err = ringbuf.NewReader(objs.Events)
	if err != nil {
		c.cleanup()
		return nil, fmt.Errorf("failed to create ringbuf reader: %w", err)
	}

	c.ch = make(chan any, 100) // Buffered channel to prevent blocking
	c.stopped = make(chan struct{})
	c.SetStatus(performance.CollectorStatusActive)

	// Start event processing
	c.wg.Add(1)
	go c.processEvents(ctx)

	return c.ch, nil
}

func (c *MemoryGrowthEBPFCollector) Stop() error {
	if c.Status() == performance.CollectorStatusDisabled {
		return nil
	}

	// Signal stop
	if c.stopped != nil {
		close(c.stopped)
	}

	// Wait for goroutines
	c.wg.Wait()

	// Cleanup eBPF resources
	c.cleanup()

	// Close channel
	if c.ch != nil {
		close(c.ch)
		c.ch = nil
	}

	c.SetStatus(performance.CollectorStatusDisabled)
	return nil
}

func (c *MemoryGrowthEBPFCollector) cleanup() {
	// Close reader
	if c.reader != nil {
		c.reader.Close()
		c.reader = nil
	}

	// Detach programs
	for _, l := range c.links {
		l.Close()
	}
	c.links = nil

	// Close eBPF objects
	if c.objs != nil {
		c.objs.Close()
		c.objs = nil
	}
}

func (c *MemoryGrowthEBPFCollector) processEvents(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopped:
			return
		case <-ticker.C:
			// Send periodic summary
			c.sendSummary()
		default:
			// Read events from ring buffer
			record, err := c.reader.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				// Log error but continue
				c.Logger().V(2).Info("Error reading from ringbuf", "error", err)
				continue
			}

			// Parse event
			var event MemoryGrowthEvent
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
				c.Logger().V(2).Info("Error parsing event", "error", err)
				continue
			}

			// Process event
			c.processEvent(&event)
		}
	}
}

func (c *MemoryGrowthEBPFCollector) processEvent(event *MemoryGrowthEvent) {
	var shouldAlert bool
	var alertState ProcessMemoryState

	c.mu.Lock()
	// Convert to our ProcessMemoryState
	state, exists := c.processStates[event.PID]
	if !exists {
		state = &ProcessMemoryState{
			PID:        int32(event.PID),
			Name:       string(bytes.TrimRight(event.Comm[:], "\x00")),
			StartTime:  time.Now(),
			InitialRSS: event.CurrentRss,
			PeakRSS:    event.CurrentRss,
		}
		c.processStates[event.PID] = state
	}

	// Update state from event
	state.CurrentRSS = event.CurrentRss
	state.TotalGrowth = event.TotalGrowth
	state.GrowthRate = event.GrowthRate
	state.LeakConfidence = event.LeakConfidence
	state.HeapSize = event.HeapSize
	state.StackSize = event.StackSize
	state.TotalVM = event.TotalVm
	state.LastUpdate = time.Now()

	// Update peak RSS if needed
	if state.CurrentRSS > state.PeakRSS {
		state.PeakRSS = state.CurrentRSS
	}

	// Set pattern type from eBPF data
	// The eBPF code sets pattern_type as: 0=stable, 1=steady, 2=accelerating
	switch event.PatternType {
	case 0:
		state.PatternType = "stable"
	case 1:
		state.PatternType = "steady"
	case 2:
		state.PatternType = "accelerating"
	default:
		state.PatternType = "unknown"
	}

	// Handle event types for alerts
	switch event.EventType {
	case 3: // EVENT_HIGH_RISK
		shouldAlert = true
		alertState = *state
	case 4: // EVENT_PROCESS_EXIT
		delete(c.processStates, event.PID)
		c.mu.Unlock()
		return
	case 5: // EVENT_OOM_WARNING
		shouldAlert = true
		alertState = *state
	}
	c.mu.Unlock()

	// Send alert outside of lock if needed
	if shouldAlert {
		c.sendAlert(&alertState)
	}
}

func (c *MemoryGrowthEBPFCollector) sendAlert(state *ProcessMemoryState) {
	// Create a copy to send
	alert := *state

	select {
	case c.ch <- &ProcessMemoryGrowthStats{
		Timestamp:             time.Now(),
		MonitoredProcessCount: 1,
		GrowingProcessCount:   1,
		HighRiskProcessCount:  1,
		TotalGrowthRate:       state.GrowthRate,
		ProcessDetails:        []ProcessMemoryState{alert},
	}:
	default:
		// Channel full, skip
	}
}

func (c *MemoryGrowthEBPFCollector) sendSummary() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := &ProcessMemoryGrowthStats{
		Timestamp:             time.Now(),
		MonitoredProcessCount: uint32(len(c.processStates)),
	}

	var totalGrowthRate uint64
	details := make([]ProcessMemoryState, 0, len(c.processStates))

	for _, state := range c.processStates {
		if state.GrowthRate > 0 {
			stats.GrowingProcessCount++
			totalGrowthRate += state.GrowthRate
		}
		if state.LeakConfidence >= c.config.ConfidenceThreshold {
			stats.HighRiskProcessCount++
		}

		// Include all monitored processes in details
		details = append(details, *state)
	}

	stats.TotalGrowthRate = totalGrowthRate
	stats.ProcessDetails = details

	select {
	case c.ch <- stats:
	default:
		// Channel full, skip
	}
}

// Compile-time interface check
var _ performance.ContinuousCollector = (*MemoryGrowthEBPFCollector)(nil)
