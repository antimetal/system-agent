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
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
)

var (
	verbose     = flag.Bool("verbose", false, "Enable verbose output")
	benchMode   = flag.Bool("bench", false, "Run performance benchmarks")
	iterations  = flag.Int("iterations", 10, "Number of benchmark iterations")
	showData    = flag.Bool("show-data", false, "Show collected data (can be large)")
	filterTypes = flag.String("filter", "", "Comma-separated list of collector types to run (cpu_info,memory_info,disk_info,network_info)")
	timeout     = flag.Duration("timeout", 30*time.Second, "Timeout for individual collectors")
)

type CollectorResult struct {
	Name       string
	Type       performance.MetricType
	Success    bool
	Error      error
	Duration   time.Duration
	DataSize   int
	Data       interface{}
	Benchmarks []time.Duration
}

func main() {
	flag.Parse()

	fmt.Printf("🔧 Hardware Collector Benchmark Tool\n")
	fmt.Printf("=====================================\n")
	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Go version: %s\n\n", runtime.Version())

	if runtime.GOOS != "linux" {
		fmt.Printf("❌ Error: This tool requires Linux systems.\n")
		fmt.Printf("   Hardware collectors depend on /proc and /sys filesystems.\n")
		fmt.Printf("   Current platform '%s' is not supported.\n\n", runtime.GOOS)
		fmt.Printf("   Please run this tool on a Linux system or Lima VM.\n")
		os.Exit(1)
	}

	config := performance.DefaultCollectionConfig()
	logger := logr.Discard()

	// Create all collectors
	cpuInfo, err := collectors.NewCPUInfoCollector(logger, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create CPU info collector: %v\n", err)
		os.Exit(1)
	}
	memInfo, err := collectors.NewMemoryInfoCollector(logger, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create memory info collector: %v\n", err)
		os.Exit(1)
	}
	diskInfo, err := collectors.NewDiskInfoCollector(logger, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create disk info collector: %v\n", err)
		os.Exit(1)
	}
	netInfo, err := collectors.NewNetworkInfoCollector(logger, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create network info collector: %v\n", err)
		os.Exit(1)
	}

	collectors := []performance.PointCollector{
		cpuInfo,
		memInfo,
		diskInfo,
		netInfo,
	}

	// Filter collectors if requested
	if *filterTypes != "" {
		collectors = filterCollectors(collectors, *filterTypes)
	}

	// Run tests
	results := make([]CollectorResult, 0, len(collectors))
	for _, collector := range collectors {
		result := testCollector(collector)
		results = append(results, result)
	}

	// Print results
	printResults(results)

	// Run benchmarks if requested
	if *benchMode {
		fmt.Printf("\n🏃 Running Performance Benchmarks (%d iterations)\n", *iterations)
		fmt.Printf("================================================\n")
		runBenchmarks(collectors, results)
	}

	// Summary
	printSummary(results)
}

func filterCollectors(allCollectors []performance.PointCollector, filter string) []performance.PointCollector {
	types := strings.Split(filter, ",")
	typeMap := make(map[string]bool)
	for _, t := range types {
		typeMap[strings.TrimSpace(t)] = true
	}

	var filtered []performance.PointCollector
	for _, collector := range allCollectors {
		if typeMap[string(collector.Type())] {
			filtered = append(filtered, collector)
		}
	}
	return filtered
}

func testCollector(collector performance.PointCollector) CollectorResult {
	result := CollectorResult{
		Name: collector.Name(),
		Type: collector.Type(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	start := time.Now()
	data, err := collector.Collect(ctx)
	duration := time.Since(start)

	result.Duration = duration
	result.Success = err == nil
	result.Error = err
	result.Data = data

	if data.Data != nil {
		result.DataSize = estimateDataSize(data)
	} else {
		result.DataSize = 0
	}

	return result
}

func estimateDataSize(data interface{}) int {
	// Simple estimation - in real implementation you might use reflection
	// or serialization to get accurate size
	switch v := data.(type) {
	case *performance.CPUInfo:
		if v == nil {
			return 0
		}
		return len(v.ModelName) + len(v.VendorID) + len(v.Flags)*10 + len(v.Cores)*50
	case *performance.MemoryInfo:
		if v == nil {
			return 0
		}
		return 100 + len(v.NUMANodes)*50
	case []*performance.DiskInfo:
		return len(v) * 100 // Simple estimate for slice
	case []*performance.NetworkInfo:
		return len(v) * 50 // Simple estimate for slice
	default:
		return 0
	}
}

func printResults(results []CollectorResult) {
	fmt.Printf("📊 Collector Test Results\n")
	fmt.Printf("=========================\n")

	for _, result := range results {
		status := "✅ PASS"
		if !result.Success {
			status = "❌ FAIL"
		}

		fmt.Printf("%s %s (%s)\n", status, result.Name, result.Type)
		fmt.Printf("   Duration: %v\n", result.Duration)
		fmt.Printf("   Data Size: ~%d bytes\n", result.DataSize)

		if result.Error != nil {
			fmt.Printf("   Error: %v\n", result.Error)
		}

		if result.Success && *showData {
			fmt.Printf("   Data:\n")
			printCollectorData(result.Data, "     ")
		}
		fmt.Println()
	}
}

func printCollectorData(data interface{}, indent string) {
	switch v := data.(type) {
	case *performance.CPUInfo:
		fmt.Printf("%sModel: %s\n", indent, v.ModelName)
		fmt.Printf("%sVendor: %s\n", indent, v.VendorID)
		fmt.Printf("%sPhysical Cores: %d\n", indent, v.PhysicalCores)
		fmt.Printf("%sLogical Cores: %d\n", indent, v.LogicalCores)
		fmt.Printf("%sCPU MHz: %.2f\n", indent, v.CPUMHz)
		fmt.Printf("%sFamily: %d, Model: %d, Stepping: %d\n", indent, v.CPUFamily, v.Model, v.Stepping)
		fmt.Printf("%sFeatures: %d flags\n", indent, len(v.Flags))
		if *verbose && len(v.Flags) > 0 {
			fmt.Printf("%sFlags: %s\n", indent, strings.Join(v.Flags, ", "))
		}

	case *performance.MemoryInfo:
		fmt.Printf("%sTotal Memory: %.2f GB\n", indent, float64(v.TotalBytes)/(1024*1024*1024))
		fmt.Printf("%sNUMA Nodes: %d\n", indent, len(v.NUMANodes))
		for _, node := range v.NUMANodes {
			fmt.Printf("%s  Node %d: %.2f GB, CPUs: %v\n", indent, node.NodeID,
				float64(node.TotalBytes)/(1024*1024*1024), node.CPUs)
		}

	case []*performance.DiskInfo:
		fmt.Printf("%sDisks: %d\n", indent, len(v))
		if len(v) == 0 && *verbose {
			fmt.Printf("%s  (No disks detected - check /sys/block/ for available devices)\n", indent)
		}
		for _, disk := range v {
			diskType := "SSD"
			if disk.Rotational {
				diskType = "HDD"
			}
			fmt.Printf("%s  %s: %s %s, %.2f GB (%s)\n", indent,
				disk.Device, disk.Vendor, disk.Model,
				float64(disk.SizeBytes)/(1024*1024*1024), diskType)
			if len(disk.Partitions) > 0 {
				fmt.Printf("%s    Partitions: %d\n", indent, len(disk.Partitions))
				if *verbose {
					for _, part := range disk.Partitions {
						fmt.Printf("%s      %s: %.2f GB\n", indent, part.Name,
							float64(part.SizeBytes)/(1024*1024*1024))
					}
				}
			}
		}

	case []*performance.NetworkInfo:
		fmt.Printf("%sInterfaces: %d\n", indent, len(v))
		for _, iface := range v {
			fmt.Printf("%s  %s (%s): %s\n", indent, iface.Interface, iface.Type, iface.MACAddress)
			if iface.Speed > 0 {
				fmt.Printf("%s    Speed: %d Mbps, MTU: %d\n", indent, iface.Speed, iface.MTU)
			}
			if iface.Driver != "" {
				fmt.Printf("%s    Driver: %s\n", indent, iface.Driver)
			}
		}

	default:
		fmt.Printf("%sUnknown data type: %T\n", indent, data)
	}
}

func runBenchmarks(collectors []performance.PointCollector, results []CollectorResult) {
	for i, collector := range collectors {
		fmt.Printf("\n🏃 Benchmarking %s\n", collector.Name())
		fmt.Printf("   Running %d iterations...\n", *iterations)

		var durations []time.Duration
		successCount := 0

		for j := 0; j < *iterations; j++ {
			ctx, cancel := context.WithTimeout(context.Background(), *timeout)
			start := time.Now()
			_, err := collector.Collect(ctx)
			duration := time.Since(start)
			cancel()

			durations = append(durations, duration)
			if err == nil {
				successCount++
			}

			if *verbose {
				status := "✅"
				if err != nil {
					status = "❌"
				}
				fmt.Printf("   Iteration %d: %v %s\n", j+1, duration, status)
			}
		}

		// Calculate statistics
		sort.Slice(durations, func(i, j int) bool {
			return durations[i] < durations[j]
		})

		var total time.Duration
		for _, d := range durations {
			total += d
		}

		avg := total / time.Duration(len(durations))
		min := durations[0]
		max := durations[len(durations)-1]
		median := durations[len(durations)/2]

		fmt.Printf("   Results:\n")
		fmt.Printf("     Success Rate: %d/%d (%.1f%%)\n", successCount, *iterations,
			float64(successCount)/float64(*iterations)*100)
		fmt.Printf("     Average: %v\n", avg)
		fmt.Printf("     Median:  %v\n", median)
		fmt.Printf("     Min:     %v\n", min)
		fmt.Printf("     Max:     %v\n", max)

		// Update result with benchmark data
		results[i].Benchmarks = durations
	}
}

func printSummary(results []CollectorResult) {
	fmt.Printf("\n📋 Summary\n")
	fmt.Printf("==========\n")

	passed := 0
	totalDuration := time.Duration(0)

	for _, result := range results {
		if result.Success {
			passed++
		}
		totalDuration += result.Duration
	}

	fmt.Printf("Collectors tested: %d\n", len(results))
	fmt.Printf("Passed: %d\n", passed)
	fmt.Printf("Failed: %d\n", len(results)-passed)
	fmt.Printf("Total execution time: %v\n", totalDuration)

	if passed == len(results) {
		fmt.Printf("🎉 All collectors working correctly!\n")
	} else {
		fmt.Printf("⚠️  Some collectors failed. Check errors above.\n")
		os.Exit(1)
	}
}
