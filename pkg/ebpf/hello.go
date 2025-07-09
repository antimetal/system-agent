// SPDX-License-Identifier: PolyForm
// Package ebpf provides Go bindings for eBPF programs
package ebpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror -I../../ebpf/include" -target bpfel,bpfeb -go-package ebpf hello ../../ebpf/src/hello.bpf.c -- -I../../ebpf/include

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
)

// Event represents a file open event captured by the eBPF program
type Event struct {
	PID      uint32
	UID      uint32
	Filename [256]byte
}

// String returns a formatted string representation of the event
func (e *Event) String() string {
	filename := string(bytes.TrimRight(e.Filename[:], "\x00"))
	return fmt.Sprintf("PID: %d, UID: %d, Filename: %s", e.PID, e.UID, filename)
}

// HelloProgram manages the hello eBPF program
type HelloProgram struct {
	objs   helloObjects
	link   link.Link
	reader *perf.Reader
}

// NewHelloProgram creates and loads the hello eBPF program
func NewHelloProgram() (*HelloProgram, error) {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load pre-compiled programs and maps into the kernel
	objs := helloObjects{}
	if err := loadHelloObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("loading objects: %w", err)
	}

	// Attach the program to the tracepoint
	l, err := link.Tracepoint("syscalls", "sys_enter_openat", objs.TraceOpenat, nil)
	if err != nil {
		objs.Close()
		return nil, fmt.Errorf("opening tracepoint: %w", err)
	}

	// Open a perf event reader
	rd, err := perf.NewReader(objs.Events, 4096)
	if err != nil {
		l.Close()
		objs.Close()
		return nil, fmt.Errorf("opening perf event reader: %w", err)
	}

	return &HelloProgram{
		objs:   objs,
		link:   l,
		reader: rd,
	}, nil
}

// ReadEvents reads events from the eBPF program
func (p *HelloProgram) ReadEvents() error {
	for {
		record, err := p.reader.Read()
		if err != nil {
			if errors.Is(err, perf.ErrClosed) {
				return nil
			}
			return fmt.Errorf("reading from perf event reader: %w", err)
		}

		if record.LostSamples != 0 {
			log.Printf("perf event ring buffer full, dropped %d samples", record.LostSamples)
			continue
		}

		var event Event
		if err := binary.Read(bytes.NewBuffer(record.RawSample), binary.LittleEndian, &event); err != nil {
			log.Printf("parsing perf event: %s", err)
			continue
		}

		log.Printf("Event: %s", event.String())
	}
}

// Close cleans up the eBPF program resources
func (p *HelloProgram) Close() error {
	if err := p.reader.Close(); err != nil {
		return err
	}
	if err := p.link.Close(); err != nil {
		return err
	}
	return p.objs.Close()
}
