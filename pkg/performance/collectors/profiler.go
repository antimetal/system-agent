// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-I../../../ebpf/include -Wall -Werror -g -O2 -D__TARGET_ARCH_x86 -fdebug-types-section -fno-stack-protector" -target bpfel profiler ../../../ebpf/src/profiler.bpf.c -- -I../../../ebpf/include

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/ebpf/core"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/capabilities"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
	"github.com/go-logr/logr"
)

// Perf event constants for macOS compatibility
const (
	PERF_TYPE_HARDWARE   = 0
	PERF_TYPE_SOFTWARE   = 1
	PERF_TYPE_TRACEPOINT = 2
	PERF_TYPE_HW_CACHE   = 3
	PERF_TYPE_RAW        = 4

	PERF_COUNT_HW_CPU_CYCLES    = 0
	PERF_COUNT_HW_INSTRUCTIONS  = 1
	PERF_COUNT_HW_CACHE_MISSES  = 6
	PERF_COUNT_HW_BRANCH_MISSES = 5

	PERF_SAMPLE_RAW      = 1 << 10
	PERF_FLAG_FD_CLOEXEC = 0x8
)

func init() {
	// Register CPU profiler by default
	performance.Register(performance.MetricTypeProfile,
		func(logger logr.Logger, config performance.CollectionConfig) (performance.ContinuousCollector, error) {
			return NewCPUProfiler(logger, config)
		},
	)
}

// ProfilerCollector is a continuous collector that performs perf event-based
// profiling using eBPF. It attaches to hardware or software perf events and
// captures stack traces on a periodic basis.
type ProfilerCollector struct {
	performance.BaseContinuousCollector

	mu            sync.Mutex
	bpfObjectPath string
	coreManager   *core.Manager
	objs          *ebpf.Collection
	perfLinks     []link.Link
	outputChan    chan any
	stopChan      chan struct{}
	wg            sync.WaitGroup

	// Configuration
	eventType    uint32 // PERF_TYPE_*
	eventConfig  uint64 // Event-specific config
	samplePeriod uint64 // Sample every N events
	eventName    string // Human-readable event name
	sysPath      string // Path to /sys filesystem
}

// NewProfilerCollector creates a new profiler collector with the specified configuration
func NewProfilerCollector(logger logr.Logger, config performance.CollectionConfig, eventName string, eventType uint32, eventConfig uint64, samplePeriod uint64) (*ProfilerCollector, error) {
	bpfObjectPath := os.Getenv("ANTIMETAL_BPF_PATH")
	if bpfObjectPath != "" {
		bpfObjectPath = filepath.Join(bpfObjectPath, "profiler.bpf.o")
	} else {
		bpfObjectPath = "/usr/local/lib/antimetal/ebpf/profiler.bpf.o"
	}

	collector := &ProfilerCollector{
		BaseContinuousCollector: performance.NewBaseContinuousCollector(
			performance.MetricTypeProfile,
			"profiler",
			logger,
			config,
			performance.CollectorCapabilities{
				SupportsOneShot:      false,
				SupportsContinuous:   true,
				RequiredCapabilities: capabilities.GetEBPFCapabilities(),
				MinKernelVersion:     "4.9", // perf_event BPF support
			},
		),
		bpfObjectPath: bpfObjectPath,
		eventName:     eventName,
		eventType:     eventType,
		eventConfig:   eventConfig,
		samplePeriod:  samplePeriod,
		sysPath:       config.HostSysPath,
		stopChan:      make(chan struct{}),
	}

	return collector, nil
}

// Common profiler configurations
func NewCPUProfiler(logger logr.Logger, config performance.CollectionConfig) (*ProfilerCollector, error) {
	// CPU cycles with period of 1M cycles
	return NewProfilerCollector(logger, config, "cpu-cycles",
		PERF_TYPE_HARDWARE,
		PERF_COUNT_HW_CPU_CYCLES,
		1000000)
}

func NewCacheMissProfiler(logger logr.Logger, config performance.CollectionConfig) (*ProfilerCollector, error) {
	// LLC cache misses with period of 10K misses
	return NewProfilerCollector(logger, config, "cache-misses",
		PERF_TYPE_HARDWARE,
		PERF_COUNT_HW_CACHE_MISSES,
		10000)
}

func (c *ProfilerCollector) Start(ctx context.Context) (<-chan any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Status() == performance.CollectorStatusActive {
		return nil, errors.New("collector already running")
	}

	// Remove memory limit for BPF operations
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("removing memlock: %w", err)
	}

	// Create CO-RE manager if not already created
	if c.coreManager == nil {
		manager, err := core.NewManager(c.Logger())
		if err != nil {
			return nil, fmt.Errorf("creating CO-RE manager: %w", err)
		}
		c.coreManager = manager
	}

	// Load pre-compiled BPF program
	coll, err := c.coreManager.LoadCollection(c.bpfObjectPath)
	if err != nil {
		return nil, fmt.Errorf("loading BPF collection: %w", err)
	}
	c.objs = coll

	// Get BPF program
	prog, ok := c.objs.Programs["profile"]
	if !ok {
		c.cleanup()
		return nil, errors.New("profile program not found")
	}

	// Attach to perf events on all CPUs
	cpus, err := c.onlineCPUs()
	if err != nil {
		c.cleanup()
		return nil, fmt.Errorf("getting online CPUs: %w", err)
	}

	for _, cpu := range cpus {
		perfLink, err := c.attachPerfEvent(prog, cpu)
		if err != nil {
			c.cleanup()
			return nil, fmt.Errorf("attaching perf event on CPU %d: %w", cpu, err)
		}
		c.perfLinks = append(c.perfLinks, perfLink)
	}

	// Create output channel
	c.outputChan = make(chan any, 10)

	// Start collection routine
	c.wg.Add(1)
	go c.collect(ctx)

	c.SetStatus(performance.CollectorStatusActive)
	return c.outputChan, nil
}

func (c *ProfilerCollector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Status() != performance.CollectorStatusActive {
		return nil
	}

	// Signal stop
	close(c.stopChan)

	// Wait for collection to finish
	c.wg.Wait()

	// Cleanup BPF resources
	c.cleanup()

	// Close output channel
	if c.outputChan != nil {
		close(c.outputChan)
		c.outputChan = nil
	}

	c.SetStatus(performance.CollectorStatusDisabled)
	return nil
}

func (c *ProfilerCollector) cleanup() {
	// Close perf links
	for _, link := range c.perfLinks {
		if link != nil {
			link.Close()
		}
	}
	c.perfLinks = nil

	// Close BPF collection
	if c.objs != nil {
		c.objs.Close()
		c.objs = nil
	}
}

func (c *ProfilerCollector) collect(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(10 * time.Second) // Collect every 10 seconds
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case <-ticker.C:
			profile, err := c.readProfile(startTime)
			if err != nil {
				c.Logger().Error(err, "reading profile data")
				c.SetError(err)
				continue
			}

			select {
			case c.outputChan <- profile:
			case <-ctx.Done():
				return
			case <-c.stopChan:
				return
			default:
				// Channel full, drop profile
				c.Logger().V(1).Info("dropping profile, channel full")
			}
		}
	}
}

func (c *ProfilerCollector) readProfile(startTime time.Time) (*performance.ProfileStats, error) {
	// Get maps
	stackTraces, ok := c.objs.Maps["stack_traces"]
	if !ok {
		return nil, errors.New("stack_traces map not found")
	}

	stackCounts, ok := c.objs.Maps["stack_counts"]
	if !ok {
		return nil, errors.New("stack_counts map not found")
	}

	profile := &performance.ProfileStats{
		CollectionTime: startTime,
		Duration:       time.Since(startTime),
		EventName:      c.eventName,
		EventType:      c.eventType,
		EventConfig:    c.eventConfig,
		SamplePeriod:   c.samplePeriod,
		Processes:      make(map[int32]performance.ProfileProcess),
	}

	// Read all stack counts
	var key StackKeyEvent
	var value StackCountEvent
	iter := stackCounts.Iterate()

	for iter.Next(&key, &value) {
		// Read user and kernel stacks
		userStack := make([]uint64, MaxStackDepth)
		if key.UserStackId >= 0 {
			if err := stackTraces.Lookup(uint32(key.UserStackId), &userStack); err != nil {
				c.Logger().V(2).Info("failed to lookup user stack", "id", key.UserStackId, "error", err)
			}
		}

		kernelStack := make([]uint64, MaxStackDepth)
		if key.KernelStackId >= 0 {
			if err := stackTraces.Lookup(uint32(key.KernelStackId), &kernelStack); err != nil {
				c.Logger().V(2).Info("failed to lookup kernel stack", "id", key.KernelStackId, "error", err)
			}
		}

		// Trim stacks to actual size
		userStack = trimStack(userStack)
		kernelStack = trimStack(kernelStack)

		// Create stack entry
		stack := performance.ProfileStack{
			ID:          uint32(len(profile.Stacks)), // Simple incrementing ID
			UserStack:   userStack,
			KernelStack: kernelStack,
			PID:         key.PID,
			TID:         key.TID,
			CPU:         int32(value.Cpu),
			SampleCount: value.Count,
		}

		profile.Stacks = append(profile.Stacks, stack)
		profile.SampleCount += value.Count

		// Update process info
		if proc, exists := profile.Processes[key.PID]; exists {
			proc.SampleCount += value.Count
			proc.TopStacks = append(proc.TopStacks, stack.ID)
			profile.Processes[key.PID] = proc
		} else {
			// For now, we'll use a placeholder for command name
			// In a real implementation, we'd read from /proc/PID/comm
			profile.Processes[key.PID] = performance.ProfileProcess{
				PID:         key.PID,
				Command:     fmt.Sprintf("pid-%d", key.PID),
				SampleCount: value.Count,
				TopStacks:   []uint32{stack.ID},
				ThreadCount: 1,
			}
		}
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("iterating stack counts: %w", err)
	}

	// Calculate percentages
	for i := range profile.Stacks {
		profile.Stacks[i].Percentage = float64(profile.Stacks[i].SampleCount) / float64(profile.SampleCount) * 100
	}

	for pid, proc := range profile.Processes {
		proc.Percentage = float64(proc.SampleCount) / float64(profile.SampleCount) * 100
		profile.Processes[pid] = proc
	}

	// Clear maps for next collection
	iter = stackCounts.Iterate()
	for iter.Next(&key, nil) {
		stackCounts.Delete(key)
	}

	return profile, nil
}

func trimStack(stack []uint64) []uint64 {
	for i, addr := range stack {
		if addr == 0 {
			return stack[:i]
		}
	}
	return stack
}
