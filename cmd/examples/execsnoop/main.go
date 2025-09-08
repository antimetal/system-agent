// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
)

// printReceiver implements performance.Receiver and prints exec events
type printReceiver struct {
	count *int
}

func (p *printReceiver) Accept(data any) error {
	if execEvent, ok := data.(*collectors.ExecEvent); ok {
		*p.count++
		fmt.Printf("[%d] PID=%d PPID=%d UID=%d CMD=%s ARGS=%v RetVal=%d\n",
			*p.count, execEvent.PID, execEvent.PPID, execEvent.UID,
			execEvent.Command, execEvent.Args, execEvent.RetVal)
		// Debug: show raw command
		if len(execEvent.Command) > 0 {
			fmt.Printf("    Debug: Raw command bytes: %q\n", execEvent.Command)
		}
	}
	return nil
}

func (p *printReceiver) Name() string {
	return "print-receiver"
}

func main() {
	// Parse command line flags
	bpfPath := flag.String("bpf-path", "", "Path to execsnoop.bpf.o file (defaults to /usr/local/lib/antimetal/ebpf/execsnoop.bpf.o)")
	flag.Parse()

	// Create logger
	logger := logr.Discard()

	// Create collector
	collector, err := collectors.NewExecSnoopCollector(logger, performance.DefaultCollectionConfig(), *bpfPath)
	if err != nil {
		log.Fatalf("Failed to create collector: %v", err)
	}

	fmt.Println("Tracing process executions... Press Ctrl+C to stop.")

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nStopping...")
		cancel()
	}()

	// Create a receiver that prints events
	eventCount := 0
	receiver := &printReceiver{count: &eventCount}

	// Start collector with receiver
	err = collector.Start(ctx, receiver)
	if err != nil {
		log.Fatalf("Failed to start collector: %v", err)
	}

	// Wait for context to be done
	<-ctx.Done()
	fmt.Printf("\nProcessed %d events\n", eventCount)
}
