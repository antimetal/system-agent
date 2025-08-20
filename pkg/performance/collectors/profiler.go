// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

package collectors

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-I../../../ebpf/include -Wall -Werror -g -O2 -D__TARGET_ARCH_x86 -fdebug-types-section -fno-stack-protector" -target bpfel profiler ../../../ebpf/src/profiler.bpf.c -- -I../../../ebpf/include

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/capabilities"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/go-logr/logr"
)

// ProfileEvent represents a single profile sample from the ring buffer
type ProfileEvent struct {
	Timestamp     uint64 // nanoseconds since boot
	PID           int32  // process ID
	TID           int32  // thread ID
	UserStackID   int32  // user stack trace ID
	KernelStackID int32  // kernel stack trace ID
	CPU           uint32 // CPU number
	Flags         uint32 // event flags
}

// Event flags
const (
	ProfileFlagUserStackTruncated   = 1 << 0
	ProfileFlagKernelStackTruncated = 1 << 1
	ProfileFlagStackCollision       = 1 << 2
)

// ProfilerCollector implements CPU profiling using eBPF with ring buffer streaming
type ProfilerCollector struct {
	performance.BaseContinuousCollector

	mu         sync.RWMutex
	objs       *profilerObjects
	links      []link.Link
	ringReader *ringbuf.Reader
	outputChan chan any
	stopChan   chan struct{}
	wg         sync.WaitGroup
	isRunning  bool

	// Configuration
	samplePeriod uint64
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
	collector := &ProfilerCollector{
		samplePeriod: 1000000, // 1M cycles
		stopChan:     make(chan struct{}),
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:      false,
		SupportsContinuous:   true,
		RequiredCapabilities: capabilities.GetEBPFCapabilities(),
		MinKernelVersion:     "4.18", // CO-RE support
	}

	collector.BaseContinuousCollector = *performance.NewBaseContinuousCollector(
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

	// Load eBPF objects
	spec, err := loadProfiler()
	if err != nil {
		return nil, fmt.Errorf("failed to load eBPF spec: %w", err)
	}

	p.objs = &profilerObjects{}
	if err := spec.LoadAndAssign(p.objs, nil); err != nil {
		return nil, fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Create ring buffer reader
	p.ringReader, err = ringbuf.NewReader(p.objs.Events)
	if err != nil {
		p.cleanup()
		return nil, fmt.Errorf("failed to create ring buffer reader: %w", err)
	}

	// Attach to perf events (hardware CPU cycles)
	perfLink, err := link.AttachPerfEvent(link.PerfEventOptions{
		PerfEvent: &link.PerfEvent{
			Type:   link.PerfTypeHardware,
			Config: link.PerfConfigHardwareCPUCycles,
		},
		Program:    p.objs.Profile,
		SampleFreq: 99, // 99Hz sampling
	})
	if err != nil {
		p.cleanup()
		return nil, fmt.Errorf("failed to attach perf event: %w", err)
	}
	p.links = append(p.links, perfLink)

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

	// Close links
	for _, link := range p.links {
		if link != nil {
			link.Close()
		}
	}
	p.links = nil

	// Close eBPF objects
	if p.objs != nil {
		p.objs.Close()
		p.objs = nil
	}

	// Close output channel if needed
	if p.outputChan != nil {
		// Channel is closed by processEvents goroutine
		p.outputChan = nil
	}
}
