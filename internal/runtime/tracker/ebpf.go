// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

package tracker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	"github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/ebpf/core"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/go-logr/logr"
	"golang.org/x/sys/unix"
)

// RuntimeEventType from eBPF
type RuntimeEventTypeEBPF uint32

const (
	RuntimeEventProcessFork RuntimeEventTypeEBPF = iota
	RuntimeEventProcessExec
	RuntimeEventProcessExit
	RuntimeEventCgroupCreate
	RuntimeEventCgroupDestroy
)

// RuntimeEventEBPF matches the structure in runtime_tracker.bpf.c
type RuntimeEventEBPF struct {
	Type      RuntimeEventTypeEBPF
	Timestamp uint64 // nanoseconds since boot
	PID       uint32
	PPID      uint32
	UID       uint32
	GID       uint32
	CgroupID  uint64
	Comm      [16]byte
	// Additional fields for exec events
	Filename [256]byte
	ArgsSize uint32
	Args     [512]byte // Must match MAX_ARGS_SIZE in runtime_types.h
}

// EBPFTracker implements RuntimeTracker using eBPF for real-time kernel events
type EBPFTracker struct {
	logger   logr.Logger
	config   TrackerConfig
	coreManager *core.Manager
	discovery   *containers.Discovery

	// eBPF resources
	collection *ebpf.Collection
	links      []link.Link
	ringBuffer *ringbuf.Reader

	// Event handling
	eventBatch []RuntimeEvent
	batchMu    sync.Mutex
	batchTimer *time.Timer

	// Reconciliation
	lastReconciliation time.Time
}

// NewEBPFTracker creates a new eBPF-based runtime tracker
func NewEBPFTracker(logger logr.Logger, config TrackerConfig) (*EBPFTracker, error) {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("removing memlock: %w", err)
	}

	coreManager, err := core.NewManager(logger)
	if err != nil {
		return nil, fmt.Errorf("creating CO-RE manager: %w", err)
	}

	if config.EventBufferSize == 0 {
		config.EventBufferSize = 10000
	}
	if config.DebounceInterval == 0 {
		config.DebounceInterval = 100 * time.Millisecond
	}
	if config.ReconciliationInterval == 0 {
		config.ReconciliationInterval = 5 * time.Minute
	}

	return &EBPFTracker{
		logger:      logger.WithName("ebpf-tracker"),
		config:      config,
		coreManager: coreManager,
		discovery:   containers.NewDiscovery(config.CgroupPath),
		eventBatch:  make([]RuntimeEvent, 0, 100),
	}, nil
}

// NewTracker creates a new eBPF runtime tracker
func NewTracker(logger logr.Logger, config TrackerConfig) (RuntimeTracker, error) {
	return NewEBPFTracker(logger, config)
}

// Run starts tracking runtime changes using eBPF and returns event channel
func (t *EBPFTracker) Run(ctx context.Context) (<-chan RuntimeEvent, error) {
	// Load eBPF program
	spec, err := ebpf.LoadCollectionSpec("ebpf/build/runtime_tracker.bpf.o")
	if err != nil {
		return nil, fmt.Errorf("loading eBPF spec: %w", err)
	}

	opts := ebpf.CollectionOptions{}
	t.collection, err = ebpf.NewCollection(spec, opts)
	if err != nil {
		return nil, fmt.Errorf("creating eBPF collection: %w", err)
	}

	// Open ring buffer
	t.ringBuffer, err = ringbuf.NewReader(t.collection.Maps["events"])
	if err != nil {
		t.collection.Close()
		return nil, fmt.Errorf("opening ring buffer: %w", err)
	}

	// Attach tracepoints
	if err := t.attachPrograms(); err != nil {
		t.ringBuffer.Close()
		t.collection.Close()
		return nil, fmt.Errorf("attaching eBPF programs: %w", err)
	}

	// Create event channel
	events := make(chan RuntimeEvent, t.config.EventBufferSize)

	t.logger.Info("eBPF tracker started successfully",
		"reconciliation_interval", t.config.ReconciliationInterval,
		"debounce_interval", t.config.DebounceInterval)

	// Start background goroutines
	go func() {
		defer close(events)
		defer t.cleanup()

		var wg sync.WaitGroup

		// Start event processor
		wg.Add(1)
		go func() {
			defer wg.Done()
			t.processEvents(ctx, events)
		}()

		// Start reconciliation loop
		wg.Add(1)
		go func() {
			defer wg.Done()
			t.reconciliationLoop(ctx)
		}()

		// Wait for context cancellation
		<-ctx.Done()
		t.logger.Info("eBPF tracker stopping")

		// Wait for all goroutines to finish
		wg.Wait()
		t.logger.Info("eBPF tracker stopped")
	}()

	return events, nil
}


// Snapshot returns the current full state of runtime
func (t *EBPFTracker) Snapshot(ctx context.Context) (*RuntimeSnapshot, error) {
	// Use the existing discovery mechanism for full snapshot
	// This is similar to what the polling tracker would do
	
	snapshot := &RuntimeSnapshot{
		Timestamp:  time.Now(),
		Containers: make([]*runtimev1.Container, 0),
		Processes:  make([]*runtimev1.Process, 0),
	}

	// Discover containers
	version, err := t.discovery.DetectCgroupVersion()
	if err != nil {
		return nil, fmt.Errorf("detecting cgroup version: %w", err)
	}

	containersFound, err := t.discovery.DiscoverContainers("cpu", version)
	if err != nil {
		t.logger.Error(err, "Failed to discover containers")
		// Continue with empty container list
		containersFound = []containers.Container{}
	}

	// Convert to proto format
	for _, c := range containersFound {
		snapshot.Containers = append(snapshot.Containers, &runtimev1.ContainerNode{
			ContainerId:   c.ID,
			Runtime:       c.Runtime,
			CgroupPath:    c.CgroupPath,
			CgroupVersion: runtimev1.CgroupVersion(c.CgroupVersion),
		})
	}

	// TODO: Add process discovery from /proc
	// For now, we'll rely on eBPF events to populate processes

	return snapshot, nil
}

// attachPrograms attaches eBPF programs to kernel tracepoints
func (t *EBPFTracker) attachPrograms() error {
	// Attach to process lifecycle tracepoints
	progFork, ok := t.collection.Programs["tracepoint__sched__sched_process_fork"]
	if ok {
		l, err := link.AttachTracepoint(link.TracepointOptions{
			Group:   "sched",
			Name:    "sched_process_fork",
			Program: progFork,
		})
		if err != nil {
			return fmt.Errorf("attaching fork tracepoint: %w", err)
		}
		t.links = append(t.links, l)
	}

	progExec, ok := t.collection.Programs["tracepoint__syscalls__sys_enter_execve"]
	if ok {
		l, err := link.AttachTracepoint(link.TracepointOptions{
			Group:   "syscalls",
			Name:    "sys_enter_execve",
			Program: progExec,
		})
		if err != nil {
			return fmt.Errorf("attaching execve tracepoint: %w", err)
		}
		t.links = append(t.links, l)
	}

	progExit, ok := t.collection.Programs["tracepoint__sched__sched_process_exit"]
	if ok {
		l, err := link.AttachTracepoint(link.TracepointOptions{
			Group:   "sched",
			Name:    "sched_process_exit",
			Program: progExit,
		})
		if err != nil {
			return fmt.Errorf("attaching exit tracepoint: %w", err)
		}
		t.links = append(t.links, l)
	}

	// Attach to cgroup lifecycle kprobes
	progCgroupCreate, ok := t.collection.Programs["kprobe__cgroup_mkdir"]
	if ok {
		l, err := link.AttachKprobe(link.KprobeOptions{
			Symbol:  "cgroup_mkdir",
			Program: progCgroupCreate,
		})
		if err != nil {
			// Cgroup monitoring is optional, don't fail
			t.logger.Info("Failed to attach cgroup_mkdir kprobe, container events may be delayed", "error", err)
		} else {
			t.links = append(t.links, l)
		}
	}

	return nil
}

// cleanup releases eBPF resources
func (t *EBPFTracker) cleanup() {
	// Close ring buffer to unblock reader
	if t.ringBuffer != nil {
		t.ringBuffer.Close()
	}

	// Clean up eBPF resources
	for _, l := range t.links {
		l.Close()
	}
	t.links = nil

	if t.collection != nil {
		t.collection.Close()
		t.collection = nil
	}
}

// processEvents reads events from the ring buffer and processes them
func (t *EBPFTracker) processEvents(ctx context.Context, events chan<- RuntimeEvent) {
	for {
		select {
		case <-ctx.Done():
			t.flushBatch(events)
			return
		default:
		}

		record, err := t.ringBuffer.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				t.flushBatch(events)
				return
			}
			// Log and continue on temporary errors
			t.logger.Error(err, "Reading from ring buffer")
			continue
		}

		// Parse eBPF event
		var ebpfEvent RuntimeEventEBPF
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &ebpfEvent); err != nil {
			t.logger.Error(err, "Parsing eBPF event")
			continue
		}

		// Convert to RuntimeEvent
		event := t.convertEvent(&ebpfEvent)
		if event != nil {
			t.addEventToBatch(*event, events)
		}
	}
}

// convertEvent converts an eBPF event to a RuntimeEvent
func (t *EBPFTracker) convertEvent(ebpfEvent *RuntimeEventEBPF) *RuntimeEvent {
	event := &RuntimeEvent{
		Timestamp: time.Unix(0, int64(ebpfEvent.Timestamp)),
	}

	switch ebpfEvent.Type {
	case RuntimeEventProcessFork:
		event.Type = EventTypeProcessCreate
		event.ProcessPID = int32(ebpfEvent.PID)
		event.ProcessPPID = int32(ebpfEvent.PPID)
		event.ProcessUID = ebpfEvent.UID
		event.ProcessGID = ebpfEvent.GID
		event.ProcessComm = string(bytes.TrimRight(ebpfEvent.Comm[:], "\x00"))

	case RuntimeEventProcessExec:
		event.Type = EventTypeProcessCreate
		event.ProcessPID = int32(ebpfEvent.PID)
		event.ProcessPPID = int32(ebpfEvent.PPID)
		event.ProcessUID = ebpfEvent.UID
		event.ProcessGID = ebpfEvent.GID
		event.ProcessComm = string(bytes.TrimRight(ebpfEvent.Filename[:], "\x00"))
		
		// Parse arguments if available
		if ebpfEvent.ArgsSize > 0 {
			args := string(bytes.TrimRight(ebpfEvent.Args[:ebpfEvent.ArgsSize], "\x00"))
			event.ProcessArgs = parseArgs(args)
		}

	case RuntimeEventProcessExit:
		event.Type = EventTypeProcessExit
		event.ProcessPID = int32(ebpfEvent.PID)

	case RuntimeEventCgroupCreate:
		event.Type = EventTypeContainerCreate
		event.CgroupPath = fmt.Sprintf("/sys/fs/cgroup/%d", ebpfEvent.CgroupID)
		// Container ID will be extracted from cgroup path in graph builder

	case RuntimeEventCgroupDestroy:
		event.Type = EventTypeContainerDestroy
		event.CgroupPath = fmt.Sprintf("/sys/fs/cgroup/%d", ebpfEvent.CgroupID)

	default:
		return nil
	}

	return event
}

// addEventToBatch adds an event to the batch for debounced processing
func (t *EBPFTracker) addEventToBatch(event RuntimeEvent, events chan<- RuntimeEvent) {
	t.batchMu.Lock()
	defer t.batchMu.Unlock()

	t.eventBatch = append(t.eventBatch, event)

	// Reset or start the debounce timer
	if t.batchTimer != nil {
		t.batchTimer.Stop()
	}
	t.batchTimer = time.AfterFunc(t.config.DebounceInterval, func() {
		t.flushBatch(events)
	})
}

// flushBatch sends all batched events to the event channel
func (t *EBPFTracker) flushBatch(events chan<- RuntimeEvent) {
	t.batchMu.Lock()
	defer t.batchMu.Unlock()

	if t.batchTimer != nil {
		t.batchTimer.Stop()
		t.batchTimer = nil
	}

	for _, event := range t.eventBatch {
		select {
		case events <- event:
		default:
			// Channel full, log and drop event
			t.logger.Info("Event channel full, dropping event", "type", event.Type)
		}
	}

	t.eventBatch = t.eventBatch[:0]
}

// reconciliationLoop performs periodic full scans to ensure consistency
func (t *EBPFTracker) reconciliationLoop(ctx context.Context) {

	ticker := time.NewTicker(t.config.ReconciliationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.logger.Info("Starting reconciliation scan")
			t.lastReconciliation = time.Now()
			
			snapshot, err := t.Snapshot(ctx)
			if err != nil {
				t.logger.Error(err, "Reconciliation snapshot failed")
				continue
			}

			// TODO: Compare snapshot with current graph state and generate correction events
			t.logger.Info("Reconciliation completed",
				"containers", len(snapshot.Containers),
				"processes", len(snapshot.Processes))
		}
	}
}

// parseArgs parses null-separated argument string
func parseArgs(argStr string) []string {
	if argStr == "" {
		return nil
	}
	result := make([]string, 0)
	current := ""
	for _, b := range []byte(argStr) {
		if b == 0 {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(b)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}