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

	// Start collector
	eventChan, err := collector.Start(ctx)
	if err != nil {
		log.Fatalf("Failed to start collector: %v", err)
	}

	// Process events
	eventCount := 0
	for {
		select {
		case <-ctx.Done():
			goto cleanup
		case event, ok := <-eventChan:
			if !ok {
				goto cleanup
			}
			if execEvent, ok := event.(*collectors.ExecEvent); ok {
				eventCount++
				fmt.Printf("[%d] PID=%d PPID=%d UID=%d CMD=%s ARGS=%v RetVal=%d\n",
					eventCount, execEvent.PID, execEvent.PPID, execEvent.UID,
					execEvent.Command, execEvent.Args, execEvent.RetVal)
				// Debug: show raw command
				if len(execEvent.Command) > 0 {
					fmt.Printf("    Debug: Raw command bytes: %q\n", execEvent.Command)
				}
			}
		}
	}

cleanup:
	err = collector.Stop()
	if err != nil {
		log.Printf("Error stopping collector: %v", err)
	}

	fmt.Printf("\nProcessed %d events\n", eventCount)
}
