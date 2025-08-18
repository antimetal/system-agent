// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-I../../../ebpf/include -Wall -Werror -g -O2 -D__TARGET_ARCH_x86 -fdebug-types-section -fno-stack-protector" -target bpfel profiler ../../../ebpf/src/profiler.bpf.c -- -I../../../ebpf/include

import (
	"bytes"
	"context"
	"encoding/binary"
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
	"github.com/cilium/ebpf/ringbuf"
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

	// Hardware events - match linux/perf_event.h PERF_COUNT_HW_* definitions
	PERF_COUNT_HW_CPU_CYCLES          = 0
	PERF_COUNT_HW_INSTRUCTIONS        = 1
	PERF_COUNT_HW_CACHE_REFERENCES    = 2
	PERF_COUNT_HW_CACHE_MISSES        = 3
	PERF_COUNT_HW_BRANCH_INSTRUCTIONS = 4
	PERF_COUNT_HW_BRANCH_MISSES       = 5
	PERF_COUNT_HW_BUS_CYCLES          = 6

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
	// Event configuration:
	Event PerfEventConfig // Perf event to profile (required)

	// Optional configuration:
	Interval time.Duration // Profile collection interval (optional, uses CollectionConfig.Interval if 0)
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
	SamplePeriod uint64 // Sample every N events (or nanoseconds) (required, must be > 0)
}

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
	ringReader    *ringbuf.Reader
	outputChan    chan any
	stopChan      chan struct{}
	wg            sync.WaitGroup

	// Configuration
	sysPath             string           // Path to /sys filesystem
	channelSize         int              // Output channel buffer size
	interval            time.Duration    // Collection interval
	setupCalled         bool             // Whether Setup() has been called
	profilerConfig      ProfilerConfig   // User-provided configuration
	resolvedEventConfig *PerfEventConfig // Resolved event configuration

	// Statistics
	droppedSamples  uint64 // Atomic counter for dropped samples
	eventsProcessed uint64 // Atomic counter for processed events
	ringBufferFull  uint64 // Atomic counter for ring buffer full events
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

// Setup configures the profiler with the specified event
func (c *ProfilerCollector) Setup(config ProfilerConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Validate event configuration
	if config.Event.Name == "" {
		return fmt.Errorf("event name is required")
	}
	if config.Event.SamplePeriod == 0 {
		return fmt.Errorf("sample period must be greater than zero")
	}

	// Copy event config to avoid modification of the original
	eventConfig := &PerfEventConfig{
		Name:         config.Event.Name,
		Type:         config.Event.Type,
		Config:       config.Event.Config,
		SamplePeriod: config.Event.SamplePeriod,
	}

	// Check if event is supported (fail-fast validation)
	if err := c.validateEventSupport(eventConfig); err != nil {
		return fmt.Errorf("event %q not supported: %w", eventConfig.Name, err)
	}

	// Store configuration (last Setup() call wins)
	c.profilerConfig = config
	c.resolvedEventConfig = eventConfig
	c.setupCalled = true

	c.Logger().V(1).Info("profiler configured",
		"event_name", eventConfig.Name,
		"event_type", eventConfig.Type,
		"event_config", fmt.Sprintf("0x%x", eventConfig.Config),
		"sample_period", eventConfig.SamplePeriod)

	return nil
}

// Helper function to get predefined event by type (for backwards compatibility)
func GetEventConfigByType(eventType ProfilerEventType) (PerfEventConfig, error) {
	switch eventType {
	case ProfilerEventCPUCycles:
		return CPUCyclesEvent, nil
	case ProfilerEventCacheMisses:
		return CacheMissesEvent, nil
	case ProfilerEventCPUClock:
		return CPUClockEvent, nil
	case ProfilerEventPageFaults:
		return PageFaultsEvent, nil
	default:
		return PerfEventConfig{}, fmt.Errorf("unknown event type: %d", eventType)
	}
}

// Predefined PerfEventConfig instances for common events
var (
	// Hardware events (require PMU access)
	CPUCyclesEvent = PerfEventConfig{
		Name:         "cpu-cycles",
		Type:         PERF_TYPE_HARDWARE,
		Config:       PERF_COUNT_HW_CPU_CYCLES,
		SamplePeriod: 1000000, // 1M cycles
	}

	CacheMissesEvent = PerfEventConfig{
		Name:         "cache-misses",
		Type:         PERF_TYPE_HARDWARE,
		Config:       PERF_COUNT_HW_CACHE_MISSES,
		SamplePeriod: 100000, // 100K cache misses
	}

	// Software events (work in VMs)
	CPUClockEvent = PerfEventConfig{
		Name:         "cpu-clock",
		Type:         PERF_TYPE_SOFTWARE,
		Config:       PERF_COUNT_SW_CPU_CLOCK,
		SamplePeriod: 10000000, // 10ms
	}

	PageFaultsEvent = PerfEventConfig{
		Name:         "page-faults",
		Type:         PERF_TYPE_SOFTWARE,
		Config:       PERF_COUNT_SW_PAGE_FAULTS,
		SamplePeriod: 1000, // 1K page faults
	}
)

// Helper functions for creating ProfilerConfig with predefined events
func NewProfilerConfig(event PerfEventConfig) ProfilerConfig {
	return ProfilerConfig{
		Event: event,
	}
}

// Helper function to create ProfilerConfig with custom sample period
func NewProfilerConfigWithSamplePeriod(event PerfEventConfig, samplePeriod uint64) ProfilerConfig {
	event.SamplePeriod = samplePeriod
	return ProfilerConfig{
		Event: event,
	}
}

// validateEventSupport checks if the event type is supported on this system
func (c *ProfilerCollector) validateEventSupport(eventConfig *PerfEventConfig) error {
	// Test if the specific event is available on this system
	if !isPerfEventAvailable(eventConfig.Type, eventConfig.Config) {
		// Provide helpful error message with available alternatives
		availableEvents, enumErr := GetAvailablePerfEventNames()
		if enumErr == nil && len(availableEvents) > 0 {
			return fmt.Errorf("perf event %q (type=%d, config=%d) not available on this system. Available events: %v",
				eventConfig.Name, eventConfig.Type, eventConfig.Config, availableEvents)
		}
		return fmt.Errorf("perf event %q (type=%d, config=%d) not available on this system",
			eventConfig.Name, eventConfig.Type, eventConfig.Config)
	}

	// Log successful validation with context
	if eventConfig.Type == PERF_TYPE_HARDWARE {
		c.Logger().V(1).Info("hardware event validated - PMU access available",
			"event", eventConfig.Name)
	} else {
		c.Logger().V(1).Info("software event validated",
			"event", eventConfig.Name)
	}

	return nil
}

// EnumerateSupportedEvents returns all perf events supported on this system
func (c *ProfilerCollector) EnumerateSupportedEvents() ([]PerfEventInfo, error) {
	return EnumerateAvailablePerfEvents()
}

// GetSupportedEventNames returns just the names of supported perf events
func (c *ProfilerCollector) GetSupportedEventNames() ([]string, error) {
	return GetAvailablePerfEventNames()
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

	// Set up ring buffer reader
	eventsMap, ok := c.objs.Maps["events"]
	if !ok {
		c.cleanup()
		return nil, errors.New("events ring buffer map not found")
	}

	c.ringReader, err = ringbuf.NewReader(eventsMap)
	if err != nil {
		c.cleanup()
		return nil, fmt.Errorf("creating ring buffer reader: %w", err)
	}

	c.outputChan = make(chan any, c.channelSize)
	c.wg.Add(2) // One for ring buffer reader, one for periodic collection
	go c.readRingBuffer(ctx)
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
	// Close ring buffer reader
	if c.ringReader != nil {
		c.ringReader.Close()
		c.ringReader = nil
	}

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

// readRingBuffer continuously reads events from the ring buffer
func (c *ProfilerCollector) readRingBuffer(ctx context.Context) {
	defer c.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		default:
			record, err := c.ringReader.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				c.Logger().Error(err, "reading from ring buffer")
				continue
			}

			// Parse the event
			if len(record.RawSample) < 32 {
				c.Logger().Error(nil, "event too small", "size", len(record.RawSample))
				continue
			}

			// Parse event using binary.Read for proper alignment
			var event ProfileEvent
			reader := bytes.NewReader(record.RawSample)
			if err := binary.Read(reader, binary.LittleEndian, &event); err != nil {
				c.Logger().Error(err, "parsing event")
				continue
			}

			atomic.AddUint64(&c.eventsProcessed, 1)

			// Process event (we'll aggregate in collect method)
			// For now, just count the events
			if event.Flags&ProfileFlagUserStackTruncated != 0 {
				c.Logger().V(2).Info("user stack truncated", "pid", event.PID)
			}
			if event.Flags&ProfileFlagKernelStackTruncated != 0 {
				c.Logger().V(2).Info("kernel stack truncated", "pid", event.PID)
			}
		}
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

			// Add ring buffer statistics to existing profile
			profile.LostSamples = atomic.LoadUint64(&c.ringBufferFull)

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

			// Reset start time for next collection
			startTime = time.Now()
		}
	}
}

func (c *ProfilerCollector) readProfile(startTime time.Time) (*performance.ProfileStats, error) {
	// Get dropped events counter
	droppedEventsMap, ok := c.objs.Maps["dropped_events"]
	if !ok {
		c.Logger().V(1).Info("dropped_events map not found")
	}

	// Check BPF dropped events counter
	var bpfDropped uint64
	if droppedEventsMap != nil {
		var zero uint32
		var droppedPerCPU []uint64
		if err := droppedEventsMap.Lookup(&zero, &droppedPerCPU); err == nil {
			for _, v := range droppedPerCPU {
				bpfDropped += v
			}
		}
	}

	// Read stack traces from BPF maps
	stacks, processes := c.readStackTraces()

	profile := &performance.ProfileStats{
		CollectionTime: startTime,
		Duration:       time.Since(startTime),
		EventName:      c.resolvedEventConfig.Name,
		EventType:      c.resolvedEventConfig.Type,
		EventConfig:    c.resolvedEventConfig.Config,
		SamplePeriod:   c.resolvedEventConfig.SamplePeriod,
		DroppedSamples: bpfDropped,
		SampleCount:    atomic.LoadUint64(&c.eventsProcessed),
		Stacks:         stacks,
		Processes:      processes,
	}

	c.Logger().V(1).Info("profile collection",
		"events_processed", profile.SampleCount,
		"stacks_collected", len(stacks),
		"processes", len(processes),
		"bpf_dropped", bpfDropped,
		"duration", profile.Duration)

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

// readStackTraces reads accumulated stack traces from BPF maps
func (c *ProfilerCollector) readStackTraces() ([]performance.ProfileStack, map[int32]performance.ProfileProcess) {
	stacks := []performance.ProfileStack{}
	processes := make(map[int32]performance.ProfileProcess)

	// Read stack counts map if available
	stackCountsMap, ok := c.objs.Maps["stack_counts"]
	if !ok {
		c.Logger().V(2).Info("stack_counts map not found")
		return stacks, processes
	}

	// Read user and kernel stack trace maps
	userStacksMap, hasUserStacks := c.objs.Maps["user_stacks"]
	kernelStacksMap, hasKernelStacks := c.objs.Maps["kernel_stacks"]

	if !hasUserStacks && !hasKernelStacks {
		c.Logger().V(2).Info("no stack trace maps found")
		return stacks, processes
	}

	// Iterate through stack counts to build profile data
	var stackKey StackKeyEvent
	var stackCount StackCountEvent
	iter := stackCountsMap.Iterate()

	for iter.Next(&stackKey, &stackCount) {
		profileStack := performance.ProfileStack{
			PID:         stackKey.PID,
			TID:         stackKey.TID,
			CPU:         int32(stackCount.Cpu),
			SampleCount: stackCount.Count,
		}

		// Read user stack if available
		if hasUserStacks && stackKey.UserStackId >= 0 {
			var userStack [MaxStackDepth]uint64
			if err := userStacksMap.Lookup(&stackKey.UserStackId, &userStack); err == nil {
				profileStack.UserStack = trimStack(userStack[:])
			}
		}

		// Read kernel stack if available
		if hasKernelStacks && stackKey.KernelStackId >= 0 {
			var kernelStack [MaxStackDepth]uint64
			if err := kernelStacksMap.Lookup(&stackKey.KernelStackId, &kernelStack); err == nil {
				profileStack.KernelStack = trimStack(kernelStack[:])
			}
		}

		// Update process info
		if _, exists := processes[stackKey.PID]; !exists {
			// TODO: Read actual command name from /proc or BPF comm map
			processes[stackKey.PID] = performance.ProfileProcess{
				PID:         stackKey.PID,
				Command:     fmt.Sprintf("pid-%d", stackKey.PID),
				SampleCount: stackCount.Count,
			}
		} else {
			proc := processes[stackKey.PID]
			proc.SampleCount += stackCount.Count
			processes[stackKey.PID] = proc
		}

		stacks = append(stacks, profileStack)
	}

	return stacks, processes
}

// GetEventSummary returns statistics about available perf events
func (c *ProfilerCollector) GetEventSummary() (*PerfEventSummary, error) {
	return GetPerfEventSummary()
}

// FindEventByName looks up a perf event by name
func (c *ProfilerCollector) FindEventByName(name string) (*PerfEventInfo, error) {
	return FindPerfEventByName(name)
}

// IsEventSupported checks if a specific event is supported on this system
func (c *ProfilerCollector) IsEventSupported(eventConfig *PerfEventConfig) bool {
	if eventConfig == nil {
		return false
	}
	return isPerfEventAvailable(eventConfig.Type, eventConfig.Config)
}
