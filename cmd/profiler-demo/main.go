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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/antimetal/agent/pkg/performance/collectors"
)

var (
	verbose   = flag.Bool("verbose", false, "Enable verbose output")
	rate      = flag.Int("rate", 100, "Events per second to generate")
	batchSize = flag.Int("batch", 10, "Events per batch")
	duration  = flag.Duration("duration", 30*time.Second, "How long to run")
)

func main() {
	flag.Parse()

	fmt.Println("=== Profiler Pipeline Demo ===")
	fmt.Printf("Rate: %d events/sec\n", *rate)
	fmt.Printf("Batch: %d events\n", *batchSize)
	fmt.Printf("Duration: %v\n", *duration)
	fmt.Println()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Create stub network sender
	sender := collectors.NewStubNetworkSender("demo-node", *verbose)
	
	// Start periodic statistics
	sender.StartPeriodicStats(ctx)

	// Simulate event generation
	var eventCounter uint64
	go generateEvents(ctx, sender, &eventCounter)

	// Wait for completion
	<-ctx.Done()

	// Print summary
	time.Sleep(100 * time.Millisecond) // Let final batches complete
	
	fmt.Println("\n=== Demo Complete ===")
	batches, events, dropped, bytes := sender.GetStats()
	
	fmt.Printf("Generated:     %d events\n", atomic.LoadUint64(&eventCounter))
	fmt.Printf("Sent Batches:  %d\n", batches)
	fmt.Printf("Sent Events:   %d\n", events)
	fmt.Printf("Dropped:       %d\n", dropped)
	fmt.Printf("Total Bytes:   %d (%.2f MB)\n", bytes, float64(bytes)/(1024*1024))
	
	if events > 0 {
		fmt.Printf("Efficiency:    %.1f bytes/event\n", float64(bytes)/float64(events))
	}
	
	sender.Close()
}

func generateEvents(ctx context.Context, sender *collectors.StubNetworkSender, counter *uint64) {
	batch := make([]collectors.ProfileEvent, *batchSize)
	
	// Calculate sleep duration for target rate
	batchesPerSec := float64(*rate) / float64(*batchSize)
	sleepDuration := time.Duration(float64(time.Second) / batchesPerSec)
	
	if *verbose {
		fmt.Printf("Generating %d batches/sec (sleep %v between batches)\n", 
			int(batchesPerSec), sleepDuration)
	}

	pid := int32(os.Getpid())
	
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Generate batch of events
			now := uint64(time.Now().UnixNano())
			for i := range batch {
				batch[i] = collectors.ProfileEvent{
					Timestamp:     now + uint64(i), // Slightly different timestamps
					PID:           pid,
					TID:           pid + int32(i),
					UserStackID:   int32(i * 100),
					KernelStackID: int32(i * 200),
					CPU:           uint32(i % 8),
					Flags:         0,
				}
				
				// Randomly set some flags
				if i%3 == 0 {
					batch[i].Flags |= collectors.ProfileFlagUserStackTruncated
				}
				if i%5 == 0 {
					batch[i].Flags |= collectors.ProfileFlagKernelStackTruncated
				}
				
				atomic.AddUint64(counter, 1)
			}

			// Send batch
			err := sender.SendBatch(ctx, batch)
			if err != nil && err != collectors.ErrPoolExhausted {
				log.Printf("Send error: %v", err)
			}

			// Rate limiting
			time.Sleep(sleepDuration)
		}
	}
}