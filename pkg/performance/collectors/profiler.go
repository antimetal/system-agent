// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

package collectors

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/antimetal/agent/pkg/ebpf/core"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/capabilities"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/go-logr/logr"
	"golang.org/x/sys/unix"
)

// Event flags for profiler events
const (
	ProfileFlagUserStackTruncated   = 1 << 0
	ProfileFlagKernelStackTruncated = 1 << 1
	ProfileFlagStackCollision       = 1 << 2
)

// ProfilerCollector implements CPU profiling using eBPF with ring buffer streaming
type ProfilerCollector struct {
	performance.BaseContinuousCollector

	mu            sync.RWMutex
	bpfObjectPath string
	coreManager   *core.Manager
	objs          *ebpf.Collection
	perfEventFDs  []int // Store perf event FDs for cleanup (legacy attachment)
	ringReader    *ringbuf.Reader
	outputChan    chan any
	stopChan      chan struct{}
	wg            sync.WaitGroup
	isRunning     bool

	// Configuration
	samplePeriod uint64
	sysPath      string // Path to /sys filesystem
}

func init() {
	// Register the profiler collector
	performance.Register(performance.MetricTypeProfiler,
		func(logger logr.Logger, config performance.CollectionConfig) (performance.ContinuousCollector, error) {
			return NewProfilerCollector(logger, config)
		},
	)
}

// NewProfilerCollector creates a new CPU profiler collector
func NewProfilerCollector(logger logr.Logger, config performance.CollectionConfig) (*ProfilerCollector, error) {
	// Determine BPF object path
	bpfObjectPath := ""
	envPath := os.Getenv("ANTIMETAL_BPF_PATH")
	if envPath != "" {
		bpfObjectPath = filepath.Join(envPath, "profiler.bpf.o")
	} else {
		bpfObjectPath = "/usr/local/lib/antimetal/ebpf/profiler.bpf.o"
	}

	collector := &ProfilerCollector{
		bpfObjectPath: bpfObjectPath,
		samplePeriod:  1000000, // 1M cycles
		stopChan:      make(chan struct{}),
		sysPath:       "/sys",
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      false,
		SupportsContinuous:   true,
		RequiredCapabilities: capabilities.GetEBPFCapabilities(),
		MinKernelVersion:     "5.8", // Ring buffer support (works with legacy perf attachment)
	}

	collector.BaseContinuousCollector = performance.NewBaseContinuousCollector(
		performance.MetricTypeProfiler,
		"profiler",
		logger,
		config,
		capabilities,
	)

	return collector, nil
}

// Start begins continuous CPU profiling
func (p *ProfilerCollector) Start(ctx context.Context) (<-chan any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isRunning {
		return nil, errors.New("profiler is already running")
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("failed to remove memory limit: %w", err)
	}

	// Check ring buffer support (requires kernel 5.8+)
	if err := core.CheckRingBufferSupport(); err != nil {
		return nil, fmt.Errorf("profiler requires ring buffer support: %w", err)
	}

	// Create CO-RE manager if not already created
	if p.coreManager == nil {
		manager, err := core.NewManager(p.Logger())
		if err != nil {
			return nil, fmt.Errorf("creating CO-RE manager: %w", err)
		}
		p.coreManager = manager

		// Log CO-RE support status
		features := p.coreManager.GetKernelFeatures()
		p.Logger().Info("CO-RE support detected",
			"kernel", features.KernelVersion,
			"btf", features.HasBTF,
			"support", features.CORESupport,
		)
	}

	// Check if BPF object file exists
	if _, err := os.Stat(p.bpfObjectPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("BPF object file not found at %s: set ANTIMETAL_BPF_PATH or ensure the file is installed", p.bpfObjectPath)
	}

	// Load pre-compiled BPF program with CO-RE support
	coll, err := p.coreManager.LoadCollection(p.bpfObjectPath)
	if err != nil {
		return nil, fmt.Errorf("loading BPF collection from %s: %w", p.bpfObjectPath, err)
	}
	p.objs = coll

	// Get the events map from the collection
	eventsMap, ok := p.objs.Maps["events"]
	if !ok {
		p.cleanup()
		return nil, errors.New("events map not found in BPF collection")
	}

	// Create ring buffer reader
	p.ringReader, err = ringbuf.NewReader(eventsMap)
	if err != nil {
		p.cleanup()
		return nil, fmt.Errorf("failed to create ring buffer reader: %w", err)
	}

	// Get online CPUs
	cpus, err := p.onlineCPUs()
	if err != nil {
		p.cleanup()
		return nil, fmt.Errorf("failed to get online CPUs: %w", err)
	}

	// Get the profile program from the collection
	profileProg, ok := p.objs.Programs["profile_cpu_cycles"]
	if !ok {
		p.cleanup()
		return nil, errors.New("profile_cpu_cycles program not found in BPF collection")
	}

	// Attach to perf events on all CPUs
	// Use profile_cpu_cycles program which is designed for software CPU clock events
	for _, cpu := range cpus {
		err := p.attachPerfEvent(profileProg, cpu)
		if err != nil {
			p.cleanup()
			return nil, fmt.Errorf("failed to attach perf event on CPU %d: %w", cpu, err)
		}
		p.Logger().V(1).Info("Attached profiler to CPU", "cpu", cpu)
	}

	// Create output channel
	p.outputChan = make(chan any, 1000)

	// Start event processing
	p.wg.Add(1)
	go p.processEvents(ctx)

	p.isRunning = true
	p.SetStatus(performance.CollectorStatusActive)

	p.Logger().Info("profiler started successfully",
		"sample_freq", 99,
		"ring_buffer_size", "8MB")

	return p.outputChan, nil
}

// Stop halts profiling and cleans up resources
func (p *ProfilerCollector) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.isRunning {
		return nil
	}

	// Signal stop
	close(p.stopChan)

	// Wait for goroutines
	p.wg.Wait()

	// Cleanup resources
	p.cleanup()

	p.isRunning = false
	p.SetStatus(performance.CollectorStatusDisabled)

	p.Logger().Info("profiler stopped")
	return nil
}

// processEvents reads events from the ring buffer and forwards them
func (p *ProfilerCollector) processEvents(ctx context.Context) {
	defer p.wg.Done()
	defer close(p.outputChan)

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopChan:
			return
		default:
			record, err := p.ringReader.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				p.Logger().Error(err, "error reading from ring buffer")
				continue
			}

			// Parse profile event
			event, err := p.parseEvent(record.RawSample)
			if err != nil {
				p.Logger().Error(err, "error parsing profile event")
				continue
			}

			// Send event to output channel
			select {
			case p.outputChan <- event:
			case <-ctx.Done():
				return
			case <-p.stopChan:
				return
			default:
				// Channel full, drop event
				p.Logger().V(2).Info("dropping profile event, channel full")
			}
		}
	}
}

// parseEvent converts raw bytes to ProfileEvent struct
func (p *ProfilerCollector) parseEvent(data []byte) (*ProfileEvent, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("event data too short: %d bytes", len(data))
	}

	var event ProfileEvent
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, binary.LittleEndian, &event); err != nil {
		return nil, fmt.Errorf("failed to parse event: %w", err)
	}

	return &event, nil
}

// cleanup closes all resources
func (p *ProfilerCollector) cleanup() {
	// Close ring buffer reader
	if p.ringReader != nil {
		p.ringReader.Close()
		p.ringReader = nil
	}

	// Close perf event FDs (legacy attachment for kernels < 5.15)
	for _, fd := range p.perfEventFDs {
		unix.Close(fd)
	}
	p.perfEventFDs = nil

	// Close eBPF objects
	if p.objs != nil {
		p.objs.Close()
		p.objs = nil
	}

	// Close CO-RE manager
	if p.coreManager != nil {
		p.coreManager = nil
	}

	// Close output channel if needed
	if p.outputChan != nil {
		// Channel is closed by processEvents goroutine
		p.outputChan = nil
	}
}

// attachPerfEvent attaches the eBPF program to a perf event on the specified CPU
// For kernels < 5.15, we use legacy ioctl-based attachment which doesn't support bpf_link
func (p *ProfilerCollector) attachPerfEvent(prog *ebpf.Program, cpu int) error {
	// Use software CPU clock event for VM compatibility
	// This is the simplified version without perf event enumeration
	// Configure perf event for CPU profiling
	// Use period-based sampling for better compatibility
	attr := unix.PerfEventAttr{
		Type:        unix.PERF_TYPE_SOFTWARE,
		Config:      unix.PERF_COUNT_SW_CPU_CLOCK,
		Size:        uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
		Sample:      1000000, // Sample every 1M CPU clock events
		Sample_type: unix.PERF_SAMPLE_RAW,
		Wakeup:      1, // Wake up on every sample
	}

	fd, err := unix.PerfEventOpen(&attr, -1, cpu, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return fmt.Errorf("perf_event_open failed on CPU %d: %w", cpu, err)
	}

	// For older kernels (< 5.15), use traditional ioctl attachment
	// For newer kernels, AttachRawLink would work but we use legacy for compatibility
	err = unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_SET_BPF, prog.FD())
	if err != nil {
		unix.Close(fd)
		return fmt.Errorf("attaching BPF program via ioctl: %w", err)
	}

	// Enable the perf event
	err = unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_ENABLE, 0)
	if err != nil {
		unix.Close(fd)
		return fmt.Errorf("enabling perf event: %w", err)
	}

	// Store the FD for cleanup during Stop
	// On kernels < 5.15, there's no bpf_link support for perf events
	p.perfEventFDs = append(p.perfEventFDs, fd)

	return nil
}

// onlineCPUs returns a list of online CPU numbers
func (p *ProfilerCollector) onlineCPUs() ([]int, error) {
	// Try to read from /sys/devices/system/cpu/online
	onlinePath := filepath.Join(p.sysPath, "devices/system/cpu/online")
	data, err := os.ReadFile(onlinePath)
	if err != nil {
		// Fallback: try to count CPU directories
		return p.countCPUDirs()
	}

	cpuList := strings.TrimSpace(string(data))
	return parseCPUList(cpuList)
}

// countCPUDirs counts cpu[0-9]+ directories as a fallback
func (p *ProfilerCollector) countCPUDirs() ([]int, error) {
	cpuPath := filepath.Join(p.sysPath, "devices/system/cpu")
	entries, err := os.ReadDir(cpuPath)
	if err != nil {
		return nil, fmt.Errorf("reading CPU directory: %w", err)
	}

	var cpus []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "cpu") && len(name) > 3 {
			cpuStr := name[3:]
			if cpu, err := strconv.Atoi(cpuStr); err == nil {
				cpus = append(cpus, cpu)
			}
		}
	}

	if len(cpus) == 0 {
		// Last resort: assume at least 1 CPU
		return []int{0}, nil
	}

	return cpus, nil
}

// parseCPUList parses CPU ranges like "0-3,5,7-8" into a slice of CPU numbers
func parseCPUList(cpuList string) ([]int, error) {
	var cpus []int

	// Handle empty or invalid input
	if cpuList == "" {
		return []int{0}, nil
	}

	// Split by comma
	ranges := strings.Split(cpuList, ",")
	for _, r := range ranges {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}

		// Check if it's a range (contains hyphen)
		if strings.Contains(r, "-") {
			parts := strings.Split(r, "-")
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid CPU range: %s", r)
			}

			start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid CPU range start: %s", parts[0])
			}

			end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid CPU range end: %s", parts[1])
			}

			if start > end {
				return nil, fmt.Errorf("invalid CPU range: start > end (%d > %d)", start, end)
			}

			for i := start; i <= end; i++ {
				cpus = append(cpus, i)
			}
		} else {
			// Single CPU number
			cpu, err := strconv.Atoi(r)
			if err != nil {
				return nil, fmt.Errorf("invalid CPU number: %s", r)
			}
			cpus = append(cpus, cpu)
		}
	}

	if len(cpus) == 0 {
		// Default to at least CPU 0
		return []int{0}, nil
	}

	return cpus, nil
}
