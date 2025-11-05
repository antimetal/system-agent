// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/antimetal/agent/internal/metrics"
	"github.com/antimetal/agent/internal/metrics/consumers/perfdata"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

var (
	eventName      string
	samplePeriod   uint64
	duration       time.Duration
	interval       time.Duration
	outputFile     string
	verbose        bool
	listEvents     bool
	enablePerfdata bool
	perfdataPath   string
)

func init() {
	flag.StringVar(&eventName, "event", "cpu-clock", "Perf event name (cpu-cycles, cpu-clock, cache-misses, etc.)")
	flag.Uint64Var(&samplePeriod, "sample-period", 10000000, "Sample period (cycles for HW events, ns for SW events)")
	flag.DurationVar(&duration, "duration", 5*time.Minute, "Run duration (e.g., 5m, 1h, 24h)")
	flag.DurationVar(&interval, "interval", time.Second, "Profile aggregation interval")
	flag.StringVar(&outputFile, "output", "", "Output file for profiles (JSON, stdout if empty)")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose logging")
	flag.BoolVar(&listEvents, "list-events", false, "List available perf events and exit")
	flag.BoolVar(&enablePerfdata, "enable-perfdata", false, "Enable perf.data file writer consumer")
	flag.StringVar(&perfdataPath, "perfdata-path", "/var/lib/antimetal/profiles", "Output directory for perf.data files")
}

func main() {
	flag.Parse()

	// Setup logging
	var logger logr.Logger
	if verbose {
		zapLog, _ := zap.NewDevelopment()
		logger = zapr.NewLogger(zapLog)
	} else {
		logger = logr.Discard()
	}

	// List available events if requested
	if listEvents {
		listAvailableEvents()
		return
	}

	// Validate we're on Linux
	if runtime.GOOS != "linux" {
		fmt.Fprintf(os.Stderr, "Error: profiler only runs on Linux\n")
		os.Exit(1)
	}

	// Check if running as root
	if os.Geteuid() != 0 {
		fmt.Fprintf(os.Stderr, "Warning: profiler requires root or CAP_BPF+CAP_PERFMON capabilities\n")
	}

	fmt.Printf("=== Standalone eBPF Profiler ===\n")
	fmt.Printf("Event: %s\n", eventName)
	fmt.Printf("Sample Period: %d\n", samplePeriod)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Interval: %v\n", interval)
	fmt.Printf("Output: %s\n", getOutputTarget())
	if enablePerfdata {
		fmt.Printf("PerfData: %s\n", perfdataPath)
	}
	fmt.Printf("\n")

	// Setup perfdata consumer if enabled
	var perfdataConsumer *perfdata.Consumer
	if enablePerfdata {
		perfdataConfig := perfdata.DefaultConfig()
		perfdataConfig.OutputPath = perfdataPath
		var err error
		perfdataConsumer, err = perfdata.NewConsumer(perfdataConfig, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating perfdata consumer: %v\n", err)
			os.Exit(1)
		}

		ctx := context.Background()
		if err := perfdataConsumer.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting perfdata consumer: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✅ PerfData consumer started\n")
	}

	// Create profiler
	config := performance.CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		Interval:     interval,
	}

	profiler, err := collectors.NewProfiler(logger, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating profiler: %v\n", err)
		os.Exit(1)
	}

	// Find event
	eventInfo, err := collectors.FindPerfEventByName(eventName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: event %q not found: %v\n", eventName, err)
		fmt.Fprintf(os.Stderr, "Run with --list-events to see available events\n")
		os.Exit(1)
	}

	// Convert PerfEventInfo to PerfEventConfig
	eventConfig := collectors.PerfEventConfig{
		Name:         eventInfo.Name,
		Type:         eventInfo.Type,
		Config:       eventInfo.Config,
		SamplePeriod: samplePeriod,
	}

	// Setup profiler
	profilerConfig := collectors.NewProfilerConfigWithSamplePeriod(eventConfig, samplePeriod)
	if err := profiler.Setup(profilerConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up profiler: %v\n", err)
		os.Exit(1)
	}

	// Setup context with duration and signal handling
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Printf("\nReceived interrupt, shutting down...\n")
		cancel()
	}()

	// Start profiler
	fmt.Printf("Starting profiler...\n")
	ch, err := profiler.Start(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting profiler: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Profiler started successfully\n")
	fmt.Printf("Collecting profiles for %v... (Ctrl+C to stop early)\n\n", duration)

	// Collect and output profiles
	profileCount := 0
	totalSamples := uint64(0)
	startTime := time.Now()

	var output *os.File
	if outputFile != "" {
		output, err = os.Create(outputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
			os.Exit(1)
		}
		defer output.Close()
	} else {
		output = os.Stdout
	}

	// Write profiles as JSON Lines
	encoder := json.NewEncoder(output)

	for {
		select {
		case <-ctx.Done():
			elapsed := time.Since(startTime)
			fmt.Printf("\n=== Profiling Complete ===\n")
			fmt.Printf("Duration: %v\n", elapsed)
			fmt.Printf("Profiles Collected: %d\n", profileCount)
			fmt.Printf("Total Samples: %d\n", totalSamples)
			if profileCount > 0 {
				fmt.Printf("Avg Samples/Profile: %.0f\n", float64(totalSamples)/float64(profileCount))
			}

			// Stop perfdata consumer to flush buffered writes
			if perfdataConsumer != nil {
				fmt.Printf("Flushing perf.data files...\n")
				if err := perfdataConsumer.Stop(); err != nil {
					fmt.Fprintf(os.Stderr, "Error stopping perfdata consumer: %v\n", err)
				}
			}
			return

		case event, ok := <-ch:
			if !ok {
				return
			}

			profileStats, ok := event.Data.(*performance.ProfileStats)
			if !ok {
				continue
			}

			profileCount++
			totalSamples += profileStats.SampleCount

			// Send to perfdata consumer if enabled
			if perfdataConsumer != nil {
				metricEvent := metrics.MetricEvent{
					Timestamp:   time.Now(),
					Source:      "profiler-standalone",
					NodeName:    "lima-test",
					ClusterName: "standalone",
					MetricType:  metrics.MetricTypeProfile,
					Data:        profileStats,
				}
				if err := perfdataConsumer.HandleEvent(metricEvent); err != nil {
					fmt.Fprintf(os.Stderr, "Error sending to perfdata consumer: %v\n", err)
				}
			}

			// Log summary
			if verbose || profileCount%10 == 0 {
				fmt.Printf("[%s] Profile #%d: %d samples, %d stacks, %d processes\n",
					time.Now().Format("15:04:05"),
					profileCount,
					profileStats.SampleCount,
					len(profileStats.Stacks),
					len(profileStats.Processes))
			}

			// Write profile as JSON (if output file specified or perfdata not enabled)
			if outputFile != "" || !enablePerfdata {
				if err := encoder.Encode(profileStats); err != nil {
					fmt.Fprintf(os.Stderr, "Error writing profile: %v\n", err)
				}
			}
		}
	}
}

func listAvailableEvents() {
	fmt.Println("=== Available Perf Events ===")

	events, err := collectors.EnumerateAvailablePerfEvents()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error enumerating events: %v\n", err)
		os.Exit(1)
	}

	// Group events by source
	softwareEvents := []collectors.PerfEventInfo{}
	hardwareEvents := []collectors.PerfEventInfo{}
	pmuEvents := []collectors.PerfEventInfo{}

	for _, event := range events {
		switch event.Source {
		case "software":
			softwareEvents = append(softwareEvents, event)
		case "hardware":
			hardwareEvents = append(hardwareEvents, event)
		case "pmu":
			pmuEvents = append(pmuEvents, event)
		}
	}

	// Software events (always available)
	fmt.Println("Software Events (work in VMs and containers):")
	for _, event := range softwareEvents {
		available := "✅"
		if !event.Available {
			available = "❌"
		}
		fmt.Printf("  %s %-20s %s\n", available, event.Name, event.Description)
	}

	// Hardware events
	fmt.Println("\nHardware Events (require PMU access, bare metal only):")
	for _, event := range hardwareEvents {
		available := "✅"
		if !event.Available {
			available = "❌"
		}
		fmt.Printf("  %s %-20s %s\n", available, event.Name, event.Description)
	}

	// PMU events
	if len(pmuEvents) > 0 {
		fmt.Println("\nPMU Device Events:")
		for _, event := range pmuEvents {
			available := "✅"
			if !event.Available {
				available = "❌"
			}
			fmt.Printf("  %s %-20s %s\n", available, event.Name, event.Description)
		}
	} else {
		fmt.Println("\n⚠️  No PMU devices found (running in VM or container?)")
	}

	fmt.Printf("\nTotal: %d software, %d hardware, %d PMU events\n",
		len(softwareEvents),
		len(hardwareEvents),
		len(pmuEvents))
}

func getOutputTarget() string {
	if outputFile != "" {
		return outputFile
	}
	return "stdout (JSON Lines)"
}
