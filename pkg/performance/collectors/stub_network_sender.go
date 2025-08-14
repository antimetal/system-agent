// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	pb "github.com/antimetal/agent/pkg/performance/proto"
)

// StubNetworkSender simulates the network layer for testing
// It reads events, encodes them, and prints details instead of sending via gRPC
type StubNetworkSender struct {
	pool    *MinimalPool
	encoder *ProtobufEncoder
	
	// Statistics
	totalBatches   uint64
	totalEvents    uint64
	totalDropped   uint64
	totalBytes     uint64
	poolExhausted  uint64
	
	// Configuration
	verbose        bool
	printInterval  time.Duration
}

// NewStubNetworkSender creates a new stub sender for testing
func NewStubNetworkSender(nodeName string, verbose bool) *StubNetworkSender {
	return &StubNetworkSender{
		pool:          NewMinimalPool(),
		encoder:       NewProtobufEncoder(nodeName),
		verbose:       verbose,
		printInterval: 10 * time.Second,
	}
}

// SendBatch simulates sending a batch of events over the network
func (s *StubNetworkSender) SendBatch(ctx context.Context, events []ProfileEvent) error {
	// Get buffer from pool
	buf := s.pool.Get()
	if buf == nil {
		atomic.AddUint64(&s.poolExhausted, 1)
		if s.verbose {
			log.Printf("[STUB] Buffer pool exhausted - would drop %d events", len(events))
		}
		return ErrPoolExhausted
	}
	defer s.pool.Put(buf)

	// Encode to protobuf
	batch, bytesWritten, err := s.encoder.EncodeToProtobuf(events, buf)
	if err != nil {
		return fmt.Errorf("encoding failed: %w", err)
	}

	// Update statistics
	atomic.AddUint64(&s.totalBatches, 1)
	atomic.AddUint64(&s.totalEvents, uint64(batch.EventCount))
	atomic.AddUint64(&s.totalDropped, uint64(batch.DroppedEvents))
	atomic.AddUint64(&s.totalBytes, uint64(bytesWritten))

	// Print batch details
	if s.verbose {
		s.printBatchDetails(batch, bytesWritten)
	}

	// Decode and print individual events if very verbose
	if s.verbose && len(events) <= 5 {
		s.printDecodedEvents(batch)
	}

	return nil
}

// printBatchDetails prints information about the batch
func (s *StubNetworkSender) printBatchDetails(batch *pb.ProfileBatch, bytesWritten int) {
	fmt.Printf("\n=== STUB NETWORK SEND ===\n")
	fmt.Printf("Timestamp:     %s\n", time.Unix(0, int64(batch.TimestampNs)).Format(time.RFC3339Nano))
	fmt.Printf("Node:          %s\n", batch.NodeName)
	fmt.Printf("CPUs:          %d\n", batch.CpuCount)
	fmt.Printf("Events:        %d\n", batch.EventCount)
	fmt.Printf("Dropped:       %d\n", batch.DroppedEvents)
	fmt.Printf("Wire Size:     %d bytes\n", bytesWritten)
	fmt.Printf("Format Ver:    %d\n", batch.FormatVersion)
	
	// Show efficiency
	if batch.EventCount > 0 {
		bytesPerEvent := float64(bytesWritten) / float64(batch.EventCount)
		fmt.Printf("Bytes/Event:   %.1f\n", bytesPerEvent)
	}
}

// printDecodedEvents decodes and prints individual events
func (s *StubNetworkSender) printDecodedEvents(batch *pb.ProfileBatch) {
	events, err := DecodePackedEvents(batch)
	if err != nil {
		log.Printf("Failed to decode events: %v", err)
		return
	}

	fmt.Printf("\nDecoded Events:\n")
	for i, event := range events {
		fmt.Printf("  [%d] PID=%d TID=%d CPU=%d UserStack=%d KernelStack=%d",
			i, event.PID, event.TID, event.CPU, 
			event.UserStackID, event.KernelStackID)
		
		// Decode flags
		if event.Flags > 0 {
			fmt.Printf(" Flags=0x%x", event.Flags)
			if event.Flags&ProfileFlagUserStackTruncated != 0 {
				fmt.Printf(" (user_truncated)")
			}
			if event.Flags&ProfileFlagKernelStackTruncated != 0 {
				fmt.Printf(" (kernel_truncated)")
			}
		}
		fmt.Println()
	}
}

// StartPeriodicStats starts a goroutine that prints statistics periodically
func (s *StubNetworkSender) StartPeriodicStats(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.printInterval)
		defer ticker.Stop()

		startTime := time.Now()
		var lastEvents uint64
		var lastBytes uint64

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.printStats(startTime, &lastEvents, &lastBytes)
			}
		}
	}()
}

// printStats prints current statistics
func (s *StubNetworkSender) printStats(startTime time.Time, lastEvents, lastBytes *uint64) {
	batches := atomic.LoadUint64(&s.totalBatches)
	events := atomic.LoadUint64(&s.totalEvents)
	dropped := atomic.LoadUint64(&s.totalDropped)
	bytes := atomic.LoadUint64(&s.totalBytes)
	exhausted := atomic.LoadUint64(&s.poolExhausted)

	elapsed := time.Since(startTime).Seconds()
	
	// Calculate rates
	eventsPerSec := float64(events) / elapsed
	bytesPerSec := float64(bytes) / elapsed
	batchesPerSec := float64(batches) / elapsed

	// Calculate deltas
	deltaEvents := events - *lastEvents
	deltaBytes := bytes - *lastBytes
	*lastEvents = events
	*lastBytes = bytes

	// Get pool stats
	poolStats := s.pool.GetStats()

	fmt.Printf("\n=== STUB NETWORK STATISTICS ===\n")
	fmt.Printf("Runtime:       %.1f seconds\n", elapsed)
	fmt.Printf("Batches:       %d (%.1f/sec)\n", batches, batchesPerSec)
	fmt.Printf("Events:        %d (%.1f/sec, Δ%d)\n", events, eventsPerSec, deltaEvents)
	fmt.Printf("Dropped:       %d (%.1f%%)\n", dropped, float64(dropped)/float64(events+dropped)*100)
	fmt.Printf("Bytes:         %d (%.1f KB/sec, Δ%d)\n", bytes, bytesPerSec/1024, deltaBytes)
	fmt.Printf("Pool Stats:    Gets=%d Puts=%d Misses=%d InUse=%d\n", 
		poolStats.Gets, poolStats.Puts, poolStats.Misses, poolStats.InUse)
	fmt.Printf("Pool MissRate: %.1f%%\n", s.pool.MissRate())
	fmt.Printf("Pool Exhausted:%d times\n", exhausted)
	
	if events > 0 {
		fmt.Printf("Avg Batch:     %.1f events\n", float64(events)/float64(batches))
		fmt.Printf("Avg Event:     %.1f bytes\n", float64(bytes)/float64(events))
	}
}

// GetStats returns current statistics
func (s *StubNetworkSender) GetStats() (batches, events, dropped, bytes uint64) {
	return atomic.LoadUint64(&s.totalBatches),
		atomic.LoadUint64(&s.totalEvents),
		atomic.LoadUint64(&s.totalDropped),
		atomic.LoadUint64(&s.totalBytes)
}

// Close cleans up resources
func (s *StubNetworkSender) Close() {
	// Print final stats
	fmt.Printf("\n=== FINAL STUB NETWORK STATISTICS ===\n")
	batches, events, dropped, bytes := s.GetStats()
	fmt.Printf("Total Batches: %d\n", batches)
	fmt.Printf("Total Events:  %d\n", events)
	fmt.Printf("Total Dropped: %d\n", dropped)
	fmt.Printf("Total Bytes:   %d (%.2f MB)\n", bytes, float64(bytes)/(1024*1024))
	
	poolStats := s.pool.GetStats()
	fmt.Printf("Pool Final:    Gets=%d Puts=%d Misses=%d\n",
		poolStats.Gets, poolStats.Puts, poolStats.Misses)
}