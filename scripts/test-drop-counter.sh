#!/bin/bash
set -e

echo "=== Ring Buffer Drop Counter Test ==="
echo ""
echo "This test validates the drop counter mechanism under heavy load (10KHz)"
echo ""

# Build test binary for current architecture
echo "Building test binary..."
GOOS=linux GOARCH=amd64 go test -c -tags=integration \
    github.com/antimetal/agent/pkg/performance/collectors \
    -o /tmp/drop-counter-test

# Create minimal test program
cat > /tmp/test-drop-counter.go << 'EOF'
//go:build integration
// +build integration

package main

import (
    "context"
    "fmt"
    "runtime"
    "sync"
    "sync/atomic"
    "time"
    
    "github.com/antimetal/agent/pkg/performance/collectors"
)

func main() {
    fmt.Println("=== Drop Counter Validation Test ===")
    
    // Configure for 10KHz to force drops
    config := &collectors.ProfilerConfig{
        SamplingFrequency: 10000, // 10KHz
        PerfEvents: []collectors.PerfEventConfig{
            {Type: collectors.PERF_TYPE_SOFTWARE, Config: collectors.PERF_COUNT_SW_CPU_CLOCK},
        },
        RingBufferSize: 1024 * 1024, // 1MB - small to force drops
        Debug:          true,
    }
    
    collector, err := collectors.NewProfilerCollector(config)
    if err != nil {
        fmt.Printf("Failed to create collector: %v\n", err)
        return
    }
    defer collector.Close()
    
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    if err := collector.Start(ctx); err != nil {
        fmt.Printf("Failed to start collector: %v\n", err)
        return
    }
    
    // Generate heavy CPU load on all cores
    var wg sync.WaitGroup
    numCPU := runtime.NumCPU()
    
    fmt.Printf("Generating load on %d CPUs for 10 seconds...\n", numCPU)
    
    for i := 0; i < numCPU; i++ {
        wg.Add(1)
        go func(cpu int) {
            defer wg.Done()
            
            // Pin to CPU
            runtime.LockOSThread()
            defer runtime.UnlockOSThread()
            
            start := time.Now()
            iterations := uint64(0)
            
            for time.Since(start) < 10*time.Second {
                // CPU-intensive work
                sum := 0
                for j := 0; j < 1000; j++ {
                    sum += j * j
                }
                iterations++
                
                // No sleep - maximum load
            }
            
            fmt.Printf("CPU %d: %d iterations\n", cpu, iterations)
        }(i)
    }
    
    // Monitor drops in real-time
    ticker := time.NewTicker(1 * time.Second)
    go func() {
        for range ticker.C {
            stats := collector.GetStats()
            dropRate := float64(stats.EventsDropped) / float64(stats.EventsProcessed + stats.EventsDropped) * 100
            fmt.Printf("Events: %d, Drops: %d (%.2f%%)\n", 
                stats.EventsProcessed, stats.EventsDropped, dropRate)
        }
    }()
    
    wg.Wait()
    ticker.Stop()
    
    // Final statistics
    stats := collector.GetStats()
    totalEvents := stats.EventsProcessed + stats.EventsDropped
    dropRate := float64(stats.EventsDropped) / float64(totalEvents) * 100
    
    fmt.Println("\n=== Final Results ===")
    fmt.Printf("Total Events Generated: ~%d\n", totalEvents)
    fmt.Printf("Events Processed: %d\n", stats.EventsProcessed)
    fmt.Printf("Events Dropped: %d\n", stats.EventsDropped)
    fmt.Printf("Drop Rate: %.2f%%\n", dropRate)
    fmt.Printf("Ring Buffer Size: %d bytes\n", config.RingBufferSize)
    
    // Calculate theoretical capacity
    eventSize := 32 // bytes
    bufferCapacity := config.RingBufferSize / eventSize
    fmt.Printf("Buffer Capacity: %d events\n", bufferCapacity)
    fmt.Printf("Expected drops at 10KHz: Yes (by design)\n")
    
    if stats.EventsDropped > 0 {
        fmt.Println("\n✓ Drop counter is working correctly!")
        fmt.Println("  Drops were detected and counted under heavy load")
    } else if stats.EventsProcessed > 100000 {
        fmt.Println("\n⚠ No drops detected but processed many events")
        fmt.Println("  System may be handling load better than expected")
    } else {
        fmt.Println("\n✗ Test may need adjustment")
    }
}
EOF

# Compile the test
echo "Compiling drop counter test..."
GOOS=linux GOARCH=amd64 go build -tags=integration -o /tmp/drop-counter-test /tmp/test-drop-counter.go

echo ""
echo "Test binary created: /tmp/drop-counter-test"
echo ""
echo "To run on Hetzner:"
echo "  1. Copy to server: scp /tmp/drop-counter-test TestServer:/tmp/"
echo "  2. Run test: ssh TestServer 'sudo /tmp/drop-counter-test'"
echo ""
echo "To run locally (Linux):"
echo "  sudo /tmp/drop-counter-test"
echo ""

# If we're on Linux, offer to run immediately
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    read -p "Run test now? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        sudo /tmp/drop-counter-test
    fi
fi