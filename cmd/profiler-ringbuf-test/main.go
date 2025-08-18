// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

// profiler-ringbuf-test is a comprehensive test program for validating the streaming
// eBPF profiler with ring buffer implementation. It tests various sampling rates
// and monitors for data loss and memory stability.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

var (
	duration     = flag.Duration("duration", 10*time.Second, "Test duration for each sampling rate")
	samplingRate = flag.String("rate", "all", "Sampling rate to test: 100hz, 1khz, 10khz, or all")
	verbose      = flag.Bool("verbose", false, "Enable verbose logging")
	cpuLoad      = flag.Bool("cpu-load", true, "Generate CPU load during test")
)

type testConfig struct {
	name         string
	eventType    string
	samplePeriod uint64
}

func main() {
	flag.Parse()

	// Configure logger
	var logger logr.Logger
	if *verbose {
		zapLog, _ := zap.NewDevelopment()
		logger = zapr.NewLogger(zapLog)
	} else {
		logger = logr.Discard()
	}

	printSystemInfo()

	// Define test configurations
	allTests := []testConfig{
		{"100Hz CPU Clock", "cpu-clock", 10000000},  // 10ms = 100Hz
		{"1KHz CPU Clock", "cpu-clock", 1000000},    // 1ms = 1KHz
		{"10KHz CPU Clock", "cpu-clock", 100000},    // 0.1ms = 10KHz
	}

	// Filter tests based on flag
	var tests []testConfig
	switch strings.ToLower(*samplingRate) {
	case "100hz":
		tests = []testConfig{allTests[0]}
	case "1khz":
		tests = []testConfig{allTests[1]}
	case "10khz":
		tests = []testConfig{allTests[2]}
	case "all":
		tests = allTests
	default:
		log.Fatalf("Invalid sampling rate: %s", *samplingRate)
	}

	// Run tests
	var failed int
	for _, tc := range tests {
		fmt.Printf("\n%s\n", strings.Repeat("=", 60))
		fmt.Printf("Test: %s\n", tc.name)
		fmt.Printf("%s\n", strings.Repeat("=", 60))
		
		if err := runTest(logger, tc, *duration); err != nil {
			log.Printf("❌ Test failed: %v", err)
			failed++
		} else {
			fmt.Printf("✅ Test passed\n")
		}
	}

	// Exit with error if any test failed
	if failed > 0 {
		fmt.Printf("\n❌ %d/%d tests failed\n", failed, len(tests))
		os.Exit(1)
	} else {
		fmt.Printf("\n✅ All %d tests passed\n", len(tests))
	}
}

func printSystemInfo() {
	fmt.Printf("=== System Information ===\n")
	fmt.Printf("OS: %s\n", runtime.GOOS)
	fmt.Printf("Architecture: %s\n", runtime.GOARCH)
	fmt.Printf("Go Version: %s\n", runtime.Version())
	fmt.Printf("CPUs: %d\n", runtime.NumCPU())
	
	// Print kernel version
	if data, err := os.ReadFile("/proc/version"); err == nil {
		fmt.Printf("Kernel: %s", data)
	}
	
	// Print BPF path
	bpfPath := os.Getenv("ANTIMETAL_BPF_PATH")
	if bpfPath == "" {
		bpfPath = "/usr/local/lib/antimetal/ebpf"
	}
	fmt.Printf("BPF Path: %s\n", bpfPath)
	
	fmt.Println()
}

func runTest(logger logr.Logger, tc testConfig, duration time.Duration) error {
	// Create profiler
	config := performance.CollectionConfig{
		Interval:    time.Second,
		HostSysPath: "/sys",
	}

	profiler, err := collectors.NewProfiler(logger, config)
	if err != nil {
		return fmt.Errorf("creating profiler: %w", err)
	}
	defer profiler.Stop()

	// Setup with specified event
	setupConfig := collectors.ProfilerConfig{
		Event: collectors.PerfEventConfig{
			Name:         tc.eventType,
			Type:         collectors.PERF_TYPE_SOFTWARE,
			Config:       collectors.PERF_COUNT_SW_CPU_CLOCK,
			SamplePeriod: tc.samplePeriod,
		},
	}

	if err := profiler.Setup(setupConfig); err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}

	// Record initial memory
	var m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Start profiling
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	outputChan, err := profiler.Start(ctx)
	if err != nil {
		return fmt.Errorf("starting profiler: %w", err)
	}

	// Generate CPU load if requested
	if *cpuLoad {
		go cpuBurner(ctx)
	}

	// Collect statistics
	stats := collectStats(ctx, outputChan, duration)

	// Final memory check
	var m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m2)

	// Print results
	printResults(tc, duration, stats, m1, m2)

	// Validate results
	return validateResults(stats, m1, m2)
}

type statistics struct {
	totalEvents   int64
	totalDropped  uint64
	maxHeap       uint64
	collections   int
	lastEventTime time.Time
}

func collectStats(ctx context.Context, outputChan <-chan any, duration time.Duration) statistics {
	var stats statistics
	startTime := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return stats
		case data := <-outputChan:
			if profile, ok := data.(*performance.ProfileStats); ok {
				stats.totalEvents += int64(profile.SampleCount)
				stats.totalDropped += profile.DroppedSamples
				stats.collections++
				stats.lastEventTime = time.Now()
			}
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > stats.maxHeap {
				stats.maxHeap = m.HeapAlloc
			}
			
			// Print periodic update
			elapsed := time.Since(startTime)
			fmt.Printf("[%3.0fs] Events: %d, Dropped: %d, Heap: %.2f MB\n",
				elapsed.Seconds(),
				stats.totalEvents,
				stats.totalDropped,
				float64(m.HeapAlloc)/(1024*1024))
		}
	}
}

func printResults(tc testConfig, duration time.Duration, stats statistics, m1, m2 runtime.MemStats) {
	// Calculate expected events
	expectedRate := 1.0 / (float64(tc.samplePeriod) / 1e9)
	expectedEvents := expectedRate * duration.Seconds()
	
	fmt.Printf("\n--- Results ---\n")
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Event Type: %s\n", tc.eventType)
	fmt.Printf("  Sample Period: %d ns (%.1f Hz)\n", tc.samplePeriod, expectedRate)
	fmt.Printf("  Duration: %v\n", duration)
	fmt.Printf("\nPerformance:\n")
	fmt.Printf("  Expected Events: ~%.0f\n", expectedEvents)
	fmt.Printf("  Actual Events: %d\n", stats.totalEvents)
	fmt.Printf("  Collections: %d\n", stats.collections)
	fmt.Printf("  Dropped Events: %d\n", stats.totalDropped)
	fmt.Printf("  Drop Rate: %.4f%%\n", calculateDropRate(stats))
	fmt.Printf("  Events/sec: %.0f\n", float64(stats.totalEvents)/duration.Seconds())
	fmt.Printf("\nMemory:\n")
	fmt.Printf("  Initial Heap: %.2f MB\n", float64(m1.HeapAlloc)/(1024*1024))
	fmt.Printf("  Final Heap: %.2f MB\n", float64(m2.HeapAlloc)/(1024*1024))
	fmt.Printf("  Max Heap: %.2f MB\n", float64(stats.maxHeap)/(1024*1024))
	fmt.Printf("  Heap Growth: %.2f MB\n", float64(int64(m2.HeapAlloc)-int64(m1.HeapAlloc))/(1024*1024))
	fmt.Printf("  Goroutines: %d\n", runtime.NumGoroutine())
}

func calculateDropRate(stats statistics) float64 {
	total := float64(stats.totalEvents + int64(stats.totalDropped))
	if total == 0 {
		return 0
	}
	return float64(stats.totalDropped) / total * 100
}

func validateResults(stats statistics, m1, m2 runtime.MemStats) error {
	// Check for dropped events
	if stats.totalDropped > 0 {
		return fmt.Errorf("dropped %d events (%.2f%%)", 
			stats.totalDropped, calculateDropRate(stats))
	}

	// Check for excessive memory growth (allow some initial allocation)
	maxGrowth := uint64(50 * 1024 * 1024) // 50MB max growth
	growth := m2.HeapAlloc - m1.HeapAlloc
	if m2.HeapAlloc > m1.HeapAlloc && growth > maxGrowth {
		return fmt.Errorf("excessive memory growth: %.2f MB", 
			float64(growth)/(1024*1024))
	}

	// Check that we received events
	if stats.totalEvents == 0 {
		return fmt.Errorf("no events received")
	}

	return nil
}

func cpuBurner(ctx context.Context) {
	// Generate CPU activity to ensure profiling events
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Busy loop with occasional yield
			for i := 0; i < 1000000; i++ {
				_ = i * i
			}
			time.Sleep(time.Microsecond)
		}
	}
}