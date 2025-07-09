// SPDX-License-Identifier: PolyForm
// eBPF Hello World Example
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/antimetal/agent/pkg/ebpf"
)

func main() {
	// Check if running as root
	if os.Geteuid() != 0 {
		log.Fatal("This program must be run as root")
	}

	log.Println("Starting eBPF hello world example...")
	log.Println("Tracing openat() syscalls. Press Ctrl+C to stop.")

	// Create and load the eBPF program
	prog, err := ebpf.NewHelloProgram()
	if err != nil {
		log.Fatalf("Failed to create eBPF program: %v", err)
	}
	defer prog.Close()

	// Set up signal handling
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	// Read events in a goroutine
	go func() {
		if err := prog.ReadEvents(); err != nil {
			log.Printf("Error reading events: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-sig
	log.Println("Received signal, exiting...")
}
