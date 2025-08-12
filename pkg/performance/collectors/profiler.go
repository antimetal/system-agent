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
	"sync/atomic"
	"time"

	"github.com/antimetal/agent/pkg/ebpf/core"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/capabilities"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
	"github.com/go-logr/logr"
)

const (
	PERF_TYPE_HARDWARE   = 0
	PERF_TYPE_SOFTWARE   = 1
	PERF_TYPE_TRACEPOINT = 2
	PERF_TYPE_HW_CACHE   = 3
	PERF_TYPE_RAW        = 4
	
	// Default channel buffer size for profile output
	DefaultProfileChannelSize = 50

	PERF_COUNT_HW_CPU_CYCLES    = 0
	PERF_COUNT_HW_INSTRUCTIONS  = 1
	PERF_COUNT_HW_CACHE_MISSES  = 6
	PERF_COUNT_HW_BRANCH_MISSES = 5

	// Software events (work in virtualized environments)
	PERF_COUNT_SW_CPU_CLOCK        = 0
	PERF_COUNT_SW_TASK_CLOCK       = 1
	PERF_COUNT_SW_PAGE_FAULTS      = 2
	PERF_COUNT_SW_CONTEXT_SWITCHES = 3

	PERF_SAMPLE_RAW      = 1 << 10
	PERF_FLAG_FD_CLOEXEC = 0x8
)

// ProfilerEventType represents the type of perf event to profile
type ProfilerEventType int

const (
	ProfilerEventCPUCycles ProfilerEventType = iota
	ProfilerEventCacheMisses
	ProfilerEventCPUClock
	ProfilerEventPageFaults
)

// String returns human-readable event type name
func (t ProfilerEventType) String() string {
	switch t {
	case ProfilerEventCPUCycles:
		return "cpu-cycles"
	case ProfilerEventCacheMisses:
		return "cache-misses"
	case ProfilerEventCPUClock:
		return "cpu-clock"
	case ProfilerEventPageFaults:
		return "page-faults"
	default:
		return "unknown"
	}
}

// ProfilerConfig specifies how the profiler should be configured
type ProfilerConfig struct {
	EventType    ProfilerEventType // Type of perf event to profile
	SamplePeriod uint64            // Sample every N events (optional, uses default if 0)
	Interval     time.Duration     // Collection interval (optional, uses CollectionConfig.Interval if 0)
}

// ProfilerSetup interface for configuring profiler before starting
type ProfilerSetup interface {
	Setup(config ProfilerConfig) error
}

// PerfEventConfig defines a perf event configuration
type PerfEventConfig struct {
	Name         string // Human-readable name
	Type         uint32 // PERF_TYPE_*
	Config       uint64 // Event-specific config
	SamplePeriod uint64 // Sample every N events/nanoseconds
}

// Predefined perf event configurations
var (
	// Hardware events (require PMU access)
	HardwareCPUCycles = PerfEventConfig{
		Name:         "cpu-cycles",
		Type:         PERF_TYPE_HARDWARE,
		Config:       PERF_COUNT_HW_CPU_CYCLES,
		SamplePeriod: 1000000, // 1M cycles
	}

	HardwareCacheMisses = PerfEventConfig{
		Name:         "cache-misses",
		Type:         PERF_TYPE_HARDWARE,
		Config:       PERF_COUNT_HW_CACHE_MISSES,
		SamplePeriod: 10000, // 10K misses
	}

	// Software events (work in VMs, no PMU required)
	SoftwareCPUClock = PerfEventConfig{
		Name:         "cpu-clock",
		Type:         PERF_TYPE_SOFTWARE,
		Config:       PERF_COUNT_SW_CPU_CLOCK,
		SamplePeriod: 10000000, // 10ms in nanoseconds
	}

	SoftwareTaskClock = PerfEventConfig{
		Name:         "task-clock",
		Type:         PERF_TYPE_SOFTWARE,
		Config:       PERF_COUNT_SW_TASK_CLOCK,
		SamplePeriod: 10000000, // 10ms in nanoseconds
	}

	SoftwarePageFaults = PerfEventConfig{
		Name:         "page-faults",
		Type:         PERF_TYPE_SOFTWARE,
		Config:       PERF_COUNT_SW_PAGE_FAULTS,
		SamplePeriod: 1000, // 1K faults
	}
)

func init() {
	// Register flexible profiler that requires Setup() before use
	performance.Register(performance.MetricTypeProfile,
		func(logger logr.Logger, config performance.CollectionConfig) (performance.ContinuousCollector, error) {
			return NewProfiler(logger, config)
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
	sysPath       string // Path to /sys filesystem
	channelSize   int    // Output channel buffer size
	interval      time.Duration // Collection interval
	setupCalled   bool   // Whether Setup() has been called
	profilerConfig ProfilerConfig // User-provided configuration
	resolvedEventConfig *PerfEventConfig // Resolved event configuration
	
	// Statistics
	droppedSamples uint64 // Atomic counter for dropped samples
}

// NewProfiler creates a new profiler collector that requires Setup() before use
func NewProfiler(logger logr.Logger, config performance.CollectionConfig) (*ProfilerCollector, error) {
	bpfObjectPath := os.Getenv("ANTIMETAL_BPF_PATH")
	if bpfObjectPath != "" {
		bpfObjectPath = filepath.Join(bpfObjectPath, "profiler.bpf.o")
	} else {
		bpfObjectPath = "/usr/local/lib/antimetal/ebpf/profiler.bpf.o"
	}

	// Validate that interval is positive (required for ticker)
	if config.Interval <= 0 {
		return nil, fmt.Errorf("profiler requires positive collection interval, got: %v", config.Interval)
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
				MinKernelVersion:     "5.15", // Stable BPF perf event link support
			},
		),
		bpfObjectPath: bpfObjectPath,
		sysPath:       config.HostSysPath,
		channelSize:   DefaultProfileChannelSize,
		interval:      config.Interval,
		setupCalled:   false,
		stopChan:      make(chan struct{}),
	}

	return collector, nil
}

// Setup configures the profiler with the specified event type and options
func (c *ProfilerCollector) Setup(config ProfilerConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Convert ProfilerEventType to PerfEventConfig
	eventConfig, err := c.eventTypeToConfig(config.EventType)
	if err != nil {
		return fmt.Errorf("invalid event type %v: %w", config.EventType, err)
	}
	
	// Apply defaults if not specified
	if config.SamplePeriod == 0 {
		config.SamplePeriod = eventConfig.SamplePeriod
	} else {
		// Override the default sample period
		eventConfig.SamplePeriod = config.SamplePeriod
	}
	
	// Validate sample period
	if config.SamplePeriod == 0 {
		return fmt.Errorf("sample period cannot be zero")
	}
	
	// Check if event is supported (fail-fast validation)
	if err := c.validateEventSupport(eventConfig); err != nil {
		return fmt.Errorf("event type %v not supported: %w", config.EventType, err)
	}
	
	// Store configuration (last Setup() call wins)
	c.profilerConfig = config
	c.resolvedEventConfig = eventConfig
	c.setupCalled = true
	
	c.Logger().V(1).Info("profiler configured", 
		"event_type", config.EventType.String(),
		"sample_period", eventConfig.SamplePeriod)
	
	return nil
}

// eventTypeToConfig converts ProfilerEventType to PerfEventConfig
func (c *ProfilerCollector) eventTypeToConfig(eventType ProfilerEventType) (*PerfEventConfig, error) {
	switch eventType {
	case ProfilerEventCPUCycles:
		return &HardwareCPUCycles, nil
	case ProfilerEventCacheMisses:
		return &HardwareCacheMisses, nil
	case ProfilerEventCPUClock:
		return &SoftwareCPUClock, nil
	case ProfilerEventPageFaults:
		return &SoftwarePageFaults, nil
	default:
		return nil, fmt.Errorf("unknown event type: %d", eventType)
	}
}

// validateEventSupport checks if the event type is supported on this system
func (c *ProfilerCollector) validateEventSupport(eventConfig *PerfEventConfig) error {
	// For now, basic validation - could be enhanced to actually test perf_event_open
	// Hardware events require PMU access (typically bare metal)
	if eventConfig.Type == PERF_TYPE_HARDWARE {
		c.Logger().V(1).Info("hardware event selected - requires PMU access", 
			"event", eventConfig.Name)
	}
	
	// Software events should work everywhere
	if eventConfig.Type == PERF_TYPE_SOFTWARE {
		c.Logger().V(1).Info("software event selected - should work in all environments", 
			"event", eventConfig.Name)
	}
	
	return nil
}

func (c *ProfilerCollector) Start(ctx context.Context) (<-chan any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.setupCalled {
		return nil, errors.New("Setup() must be called before Start()")
	}

	if c.Status() == performance.CollectorStatusActive {
		return nil, errors.New("collector already running")
	}

	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("removing memlock: %w", err)
	}

	if c.coreManager == nil {
		manager, err := core.NewManager(c.Logger())
		if err != nil {
			return nil, fmt.Errorf("creating CO-RE manager: %w", err)
		}
		c.coreManager = manager
	}

	coll, err := c.coreManager.LoadCollection(c.bpfObjectPath)
	if err != nil {
		return nil, fmt.Errorf("loading BPF collection: %w", err)
	}
	c.objs = coll

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

	c.outputChan = make(chan any, c.channelSize)
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

	close(c.stopChan)
	c.wg.Wait()
	c.cleanup()

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

	ticker := time.NewTicker(c.interval)
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
				// Channel full, drop profile and increment counter
				atomic.AddUint64(&c.droppedSamples, 1)
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
		EventName:      c.resolvedEventConfig.Name,
		EventType:      c.resolvedEventConfig.Type,
		EventConfig:    c.resolvedEventConfig.Config,
		SamplePeriod:   c.resolvedEventConfig.SamplePeriod,
		DroppedSamples: atomic.LoadUint64(&c.droppedSamples),
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
	var deleteValue StackCountEvent
	for iter.Next(&key, &deleteValue) {
		if err := stackCounts.Delete(key); err != nil {
			c.Logger().V(2).Info("Failed to delete stack count entry", "error", err)
		}
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
