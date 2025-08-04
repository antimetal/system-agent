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
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

var (
	mode     = flag.String("mode", "basic", "Test mode: basic, event, load, stacks, overhead")
	event    = flag.String("event", "cpu-cycles", "Event type for event mode")
	duration = flag.Duration("duration", 10*time.Second, "Test duration")
	bpfPath  = flag.String("bpf-path", "", "Path to BPF object file")
	verbose  = flag.Bool("v", false, "Verbose output")
)

func main() {
	flag.Parse()

	// Setup logger
	zapConfig := zap.NewDevelopmentConfig()
	if !*verbose {
		zapConfig.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	}
	zapLog, err := zapConfig.Build()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	logger := zapr.NewLogger(zapLog)

	// Set BPF path directory
	if *bpfPath != "" {
		// Extract directory from the full path
		bpfDir := filepath.Dir(*bpfPath)
		os.Setenv("ANTIMETAL_BPF_PATH", bpfDir)
	}

	// Configuration
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
	}

	// Run appropriate test
	switch *mode {
	case "basic":
		err = testBasicProfiling(logger, config)
	case "event":
		err = testEventProfiling(logger, config, *event)
	case "load":
		err = testProfilingUnderLoad(logger, config)
	case "stacks":
		err = testStackTraces(logger, config)
	case "overhead":
		err = testOverhead(logger, config)
	default:
		err = fmt.Errorf("unknown mode: %s", *mode)
	}

	if err != nil {
		log.Fatalf("Test failed: %v", err)
	}

	fmt.Println("Test passed!")
}

func testBasicProfiling(logger logr.Logger, config performance.CollectionConfig) error {
	fmt.Println("Testing basic CPU profiling...")

	// Create CPU profiler
	profiler, err := collectors.NewCPUProfiler(logger, config)
	if err != nil {
		return fmt.Errorf("creating profiler: %w", err)
	}

	// Start profiling
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	ch, err := profiler.Start(ctx)
	if err != nil {
		return fmt.Errorf("starting profiler: %w", err)
	}
	defer profiler.Stop()

	// Collect at least one profile
	select {
	case data := <-ch:
		profile, ok := data.(*performance.ProfileStats)
		if !ok {
			return fmt.Errorf("unexpected data type: %T", data)
		}

		fmt.Printf("Collected profile:\n")
		fmt.Printf("  Event: %s\n", profile.EventName)
		fmt.Printf("  Samples: %d\n", profile.SampleCount)
		fmt.Printf("  Stacks: %d\n", len(profile.Stacks))
		fmt.Printf("  Processes: %d\n", len(profile.Processes))

		if profile.SampleCount == 0 {
			return fmt.Errorf("no samples collected")
		}

		return nil

	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for profile data")
	}
}

func testEventProfiling(logger logr.Logger, config performance.CollectionConfig, eventName string) error {
	fmt.Printf("Testing %s profiling...\n", eventName)

	var profiler *collectors.ProfilerCollector
	var err error

	// Create profiler based on event type
	switch eventName {
	case "cpu-cycles":
		profiler, err = collectors.NewCPUProfiler(logger, config)
	case "cache-misses":
		profiler, err = collectors.NewCacheMissProfiler(logger, config)
	case "branch-misses":
		profiler, err = collectors.NewProfilerCollector(logger, config, "branch-misses",
			0, // PERF_TYPE_HARDWARE
			5, // PERF_COUNT_HW_BRANCH_MISSES
			10000)
	case "instructions":
		profiler, err = collectors.NewProfilerCollector(logger, config, "instructions",
			0, // PERF_TYPE_HARDWARE
			1, // PERF_COUNT_HW_INSTRUCTIONS
			1000000)
	default:
		return fmt.Errorf("unknown event type: %s", eventName)
	}

	if err != nil {
		return fmt.Errorf("creating profiler: %w", err)
	}

	// Run basic test
	return runProfilerTest(profiler)
}

func testProfilingUnderLoad(logger logr.Logger, config performance.CollectionConfig) error {
	fmt.Println("Testing profiling under CPU load...")

	profiler, err := collectors.NewCPUProfiler(logger, config)
	if err != nil {
		return fmt.Errorf("creating profiler: %w", err)
	}

	// Start CPU load
	var wg sync.WaitGroup
	stopLoad := make(chan struct{})

	// Spin up some CPU-intensive goroutines
	numCPU := runtime.NumCPU()
	for i := 0; i < numCPU; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopLoad:
					return
				default:
					// CPU-intensive work
					sum := 0
					for j := 0; j < 1000000; j++ {
						sum += j
					}
					_ = sum
				}
			}
		}()
	}

	// Give load time to start
	time.Sleep(1 * time.Second)

	// Run profiler test
	err = runProfilerTest(profiler)

	// Stop load
	close(stopLoad)
	wg.Wait()

	return err
}

func testStackTraces(logger logr.Logger, config performance.CollectionConfig) error {
	fmt.Println("Testing stack trace collection...")

	profiler, err := collectors.NewCPUProfiler(logger, config)
	if err != nil {
		return fmt.Errorf("creating profiler: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	ch, err := profiler.Start(ctx)
	if err != nil {
		return fmt.Errorf("starting profiler: %w", err)
	}
	defer profiler.Stop()

	// Create some recognizable stack traces
	go generateStackTraces()

	// Collect profile
	select {
	case data := <-ch:
		profile, ok := data.(*performance.ProfileStats)
		if !ok {
			return fmt.Errorf("unexpected data type: %T", data)
		}

		fmt.Printf("Stack trace analysis:\n")
		fmt.Printf("  Total stacks: %d\n", len(profile.Stacks))

		hasUserStacks := false
		hasKernelStacks := false

		for _, stack := range profile.Stacks {
			if len(stack.UserStack) > 0 {
				hasUserStacks = true
			}
			if len(stack.KernelStack) > 0 {
				hasKernelStacks = true
			}
		}

		fmt.Printf("  Has user stacks: %v\n", hasUserStacks)
		fmt.Printf("  Has kernel stacks: %v\n", hasKernelStacks)

		if !hasUserStacks && !hasKernelStacks {
			return fmt.Errorf("no stack traces collected")
		}

		return nil

	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for profile data")
	}
}

func testOverhead(logger logr.Logger, config performance.CollectionConfig) error {
	fmt.Println("Testing profiler overhead...")

	// Benchmark without profiler
	start := time.Now()
	workWithoutProfiler()
	baselineDuration := time.Since(start)

	fmt.Printf("Baseline (no profiler): %v\n", baselineDuration)

	// Create and start profiler
	profiler, err := collectors.NewCPUProfiler(logger, config)
	if err != nil {
		return fmt.Errorf("creating profiler: %w", err)
	}

	ctx := context.Background()
	_, err = profiler.Start(ctx)
	if err != nil {
		return fmt.Errorf("starting profiler: %w", err)
	}
	defer profiler.Stop()

	// Benchmark with profiler
	start = time.Now()
	workWithoutProfiler()
	profiledDuration := time.Since(start)

	fmt.Printf("With profiler: %v\n", profiledDuration)

	// Calculate overhead
	overhead := float64(profiledDuration-baselineDuration) / float64(baselineDuration) * 100
	fmt.Printf("Overhead: %.2f%%\n", overhead)

	// Fail if overhead is too high
	if overhead > 10.0 {
		return fmt.Errorf("overhead too high: %.2f%%", overhead)
	}

	return nil
}

func runProfilerTest(profiler *collectors.ProfilerCollector) error {
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	ch, err := profiler.Start(ctx)
	if err != nil {
		return fmt.Errorf("starting profiler: %w", err)
	}
	defer profiler.Stop()

	// Wait for data
	select {
	case data := <-ch:
		profile, ok := data.(*performance.ProfileStats)
		if !ok {
			return fmt.Errorf("unexpected data type: %T", data)
		}

		if profile.SampleCount == 0 {
			return fmt.Errorf("no samples collected")
		}

		fmt.Printf("Successfully collected %d samples\n", profile.SampleCount)
		return nil

	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for profile data")
	}
}

func generateStackTraces() {
	for i := 0; i < 1000000; i++ {
		recursiveFunction(10)
		time.Sleep(1 * time.Microsecond)
	}
}

func recursiveFunction(depth int) int {
	if depth == 0 {
		return 1
	}
	return depth * recursiveFunction(depth-1)
}

func workWithoutProfiler() {
	// Simulate some CPU-intensive work
	sum := 0
	for i := 0; i < 100000000; i++ {
		sum += i * i
	}
	_ = sum
}
