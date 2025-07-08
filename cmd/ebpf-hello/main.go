package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/antimetal/agent/pkg/ebpf"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	fmt.Println("Starting eBPF Hello World...")
	fmt.Println("============================")

	// Create monitor
	monitor, err := ebpf.NewHelloMonitor()
	if err != nil {
		log.Fatalf("Failed to create monitor: %v", err)
	}
	defer monitor.Close()

	// Attach eBPF program
	if err := monitor.Attach(); err != nil {
		log.Fatalf("Failed to attach eBPF program: %v", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start reading events
	events, errors := monitor.ReadEvents(ctx)

	// Counter ticker
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	fmt.Println("\nMonitoring openat syscalls (press Ctrl+C to exit)...")
	fmt.Println("Try running: ls, cat, or any file operations to see events!")
	fmt.Println("")

	eventCount := 0

	// Main event loop
	for {
		select {
		case sig := <-sigCh:
			fmt.Printf("\nReceived signal: %v\n", sig)
			fmt.Printf("Total events received: %d\n", eventCount)
			return

		case event := <-events:
			if event == nil {
				return
			}
			eventCount++
			fmt.Printf("Event #%d: %s\n", eventCount, event)

		case err := <-errors:
			if err != nil {
				log.Printf("Error: %v", err)
			}

		case <-ticker.C:
			counter, err := monitor.GetCounter()
			if err != nil {
				log.Printf("Failed to get counter: %v", err)
			} else {
				fmt.Printf("\n--- Stats ---\n")
				fmt.Printf("Events in userspace: %d\n", eventCount)
				fmt.Printf("Events in kernel: %d\n", counter)
				fmt.Printf("-------------\n\n")
			}
		}
	}
}
