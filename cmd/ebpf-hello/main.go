// SPDX-License-Identifier: PolyForm
// Simple test program for the eBPF hello world example
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/antimetal/agent/pkg/ebpf"
)

func main() {
	// Create and load the eBPF program
	prog, err := ebpf.NewHelloProgram()
	if err != nil {
		log.Fatalf("Failed to create eBPF program: %v", err)
	}
	defer prog.Close()

	// Set up signal handling for graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	fmt.Println("eBPF Hello World - Tracing openat() system calls")
	fmt.Println("Press Ctrl+C to exit")
	fmt.Println()

	// Start reading events in a goroutine
	done := make(chan error, 1)
	go func() {
		done <- prog.ReadEvents()
	}()

	// Wait for signal or error
	select {
	case <-sig:
		fmt.Println("\nReceived signal, shutting down...")
	case err := <-done:
		if err != nil {
			log.Fatalf("Error reading events: %v", err)
		}
	}
}
