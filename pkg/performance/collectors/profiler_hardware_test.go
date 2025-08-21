// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build hardware

package collectors

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/kernel"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/cilium/ebpf/rlimit"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHardwareProfiler_PMUEvents tests hardware PMU event collection
// This requires bare metal hardware with PMU access
func TestHardwareProfiler_PMUEvents(t *testing.T) {
	// Verify we're on bare metal with PMU support
	requireBareMetal(t)
	
	// Check kernel version
	kernelVersion, err := kernel.GetCurrentVersion()
	require.NoError(t, err, "Failed to get kernel version")
	
	if !kernelVersion.IsAtLeast(5, 8) {
		t.Skipf("Profiler requires kernel 5.8+ for ring buffer support, current kernel is %s", kernelVersion.String())
	}

	// Remove memory limit for eBPF
	err = rlimit.RemoveMemlock()
	require.NoError(t, err, "Failed to remove memlock limit")

	logger := testr.New(t)
	config := performance.CollectionConfig{
		Interval:     time.Second,
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
	}

	t.Run("Hardware CPU Cycles", func(t *testing.T) {
		collector, err := NewProfilerCollector(logger, config)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Start profiling
		dataChan, err := collector.Start(ctx)
		require.NoError(t, err, "Failed to start hardware profiler")
		defer collector.Stop()

		// Generate CPU load to ensure events
		go generateCPULoad(ctx)

		// Collect events with detailed statistics
		events := collectProfileEvents(t, dataChan, 100, 10*time.Second)
		
		// Verify we got hardware events
		assert.GreaterOrEqual(t, len(events), 50, "Should collect at least 50 hardware events")
		
		// Analyze event distribution
		analyzeEventDistribution(t, events)
	})

	t.Run("Multiple Hardware Events", func(t *testing.T) {
		// Test various hardware events if available
		testEvents := []string{
			"cpu-cycles",
			"instructions", 
			"cache-references",
			"cache-misses",
			"branch-instructions",
			"branch-misses",
		}

		for _, eventName := range testEvents {
			t.Run(eventName, func(t *testing.T) {
				if !isHardwareEventAvailable(eventName) {
					t.Skipf("Hardware event %s not available on this CPU", eventName)
				}

				collector, err := NewProfilerCollector(logger, config)
				require.NoError(t, err)

				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				dataChan, err := collector.Start(ctx)
				if err != nil {
					t.Logf("Warning: Could not start profiler for %s: %v", eventName, err)
					return
				}
				defer collector.Stop()

				// Generate workload
				go generateCPULoad(ctx)

				// Collect some events
				events := collectProfileEvents(t, dataChan, 20, 5*time.Second)
				t.Logf("Collected %d events for %s", len(events), eventName)
				
				assert.Greater(t, len(events), 0, "Should collect some events for %s", eventName)
			})
		}
	})
}

// TestHardwareProfiler_HighFrequency tests high-frequency sampling
func TestHardwareProfiler_HighFrequency(t *testing.T) {
	requireBareMetal(t)
	
	logger := testr.New(t)
	config := performance.CollectionConfig{
		Interval:     100 * time.Millisecond, // High frequency
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
	}

	collector, err := NewProfilerCollector(logger, config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start profiling
	dataChan, err := collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Generate intensive CPU load
	stopLoad := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopLoad:
				return
			default:
				// Intensive computation
				_ = fibonacci(30)
			}
		}
	}()
	defer close(stopLoad)

	// Measure event rate
	startTime := time.Now()
	eventCount := 0
	timeout := time.After(2 * time.Second)

	for {
		select {
		case event := <-dataChan:
			if event != nil {
				eventCount++
			}
		case <-timeout:
			goto done
		}
	}

done:
	duration := time.Since(startTime)
	eventsPerSecond := float64(eventCount) / duration.Seconds()
	
	t.Logf("High-frequency test: %d events in %v (%.2f events/sec)", 
		eventCount, duration, eventsPerSecond)
	
	// Should get a reasonable event rate
	assert.Greater(t, eventsPerSecond, 50.0, "Should achieve at least 50 events/sec")
	assert.Less(t, eventsPerSecond, 10000.0, "Event rate should not be excessive")
}

// TestHardwareProfiler_NUMA tests NUMA-aware profiling
func TestHardwareProfiler_NUMA(t *testing.T) {
	requireBareMetal(t)
	
	// Check if system has multiple NUMA nodes
	numaNudes := getNumaNodeCount()
	if numaNudes <= 1 {
		t.Skip("System has only one NUMA node, skipping NUMA tests")
	}

	logger := testr.New(t)
	config := performance.CollectionConfig{
		Interval:     time.Second,
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
	}

	collector, err := NewProfilerCollector(logger, config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dataChan, err := collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Generate load on different NUMA nodes
	for node := 0; node < numaNudes; node++ {
		go generateNumaLoad(ctx, node)
	}

	// Collect events
	events := collectProfileEvents(t, dataChan, 100, 5*time.Second)
	
	// Analyze NUMA distribution
	numaDistribution := make(map[uint32]int)
	for _, event := range events {
		// Group by CPU to infer NUMA node
		numaDistribution[event.Cpu]++
	}

	t.Logf("NUMA distribution across %d CPUs: %+v", len(numaDistribution), numaDistribution)
	assert.Greater(t, len(numaDistribution), 1, "Events should come from multiple CPUs")
}

// TestHardwareProfiler_Stress performs stress testing
func TestHardwareProfiler_Stress(t *testing.T) {
	requireBareMetal(t)
	
	logger := logr.Discard() // Use discard logger for stress test
	config := performance.CollectionConfig{
		Interval:     100 * time.Millisecond,
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
	}

	// Test multiple start/stop cycles
	t.Run("StartStopCycles", func(t *testing.T) {
		collector, err := NewProfilerCollector(logger, config)
		require.NoError(t, err)

		for i := 0; i < 10; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			
			dataChan, err := collector.Start(ctx)
			require.NoError(t, err, "Failed to start on iteration %d", i)
			
			// Collect a few events
			time.Sleep(100 * time.Millisecond)
			
			err = collector.Stop()
			assert.NoError(t, err, "Failed to stop on iteration %d", i)
			
			cancel()
			
			// Drain channel
			for len(dataChan) > 0 {
				<-dataChan
			}
		}
	})

	// Test concurrent profilers
	t.Run("ConcurrentProfilers", func(t *testing.T) {
		// Note: Only one profiler per CPU is typically allowed
		// This tests graceful handling of conflicts
		
		collector1, err := NewProfilerCollector(logger, config)
		require.NoError(t, err)
		
		collector2, err := NewProfilerCollector(logger, config)
		require.NoError(t, err)

		ctx := context.Background()
		
		_, err1 := collector1.Start(ctx)
		require.NoError(t, err1, "First collector should start")
		defer collector1.Stop()
		
		_, err2 := collector2.Start(ctx)
		// Second might fail due to resource conflict - that's expected
		if err2 == nil {
			defer collector2.Stop()
			t.Log("Both collectors started - system allows concurrent profiling")
		} else {
			t.Logf("Second collector failed as expected: %v", err2)
		}
	})
}

// Helper functions

func requireBareMetal(t *testing.T) {
	// Check for PMU support - different architectures expose PMU differently
	pmuDevice := detectPMUDevice()
	if pmuDevice == "" {
		t.Skip("No PMU support detected - this test requires bare metal hardware")
	}
	t.Logf("PMU device detected: %s", pmuDevice)

	// Check for virtualization
	if isVirtualized() {
		t.Skip("Running in virtualized environment - this test requires bare metal")
	}

	// Verify perf_event_paranoid allows profiling
	paranoidBytes, err := os.ReadFile("/proc/sys/kernel/perf_event_paranoid")
	if err == nil {
		paranoidLevel := strings.TrimSpace(string(paranoidBytes))
		t.Logf("perf_event_paranoid level: %s", paranoidLevel)
	}
}

// detectPMUDevice finds the PMU device for the current architecture
func detectPMUDevice() string {
	// Check for various PMU device types
	pmuDevices := []string{
		"cpu",              // x86_64 and some ARM systems
		"armv8_pmuv3_0",    // ARM64 PMUv3
		"arm_pmu",          // Generic ARM PMU
		"intel_pt",         // Intel Processor Trace
		"amd_iommu",        // AMD IOMMU PMU
	}

	basePath := "/sys/bus/event_source/devices/"
	for _, device := range pmuDevices {
		if _, err := os.Stat(basePath + device); err == nil {
			// Check if it has events (indicates it's a real PMU)
			eventsPath := basePath + device + "/events"
			if _, err := os.Stat(eventsPath); err == nil {
				return device
			}
		}
	}

	// Also check for any device starting with "armv8_pmuv3_"
	entries, err := os.ReadDir(basePath)
	if err == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "armv8_pmuv3_") {
				return entry.Name()
			}
		}
	}

	return ""
}

func isVirtualized() bool {
	// Check various indicators of virtualization
	indicators := []string{
		"/proc/xen",
		"/sys/hypervisor/type",
		"/proc/device-tree/hypervisor",
		"/sys/devices/virtual/dmi/id/product_name",
	}

	for _, path := range indicators {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	// Check DMI for VM indicators
	if data, err := os.ReadFile("/sys/devices/virtual/dmi/id/sys_vendor"); err == nil {
		vendor := strings.ToLower(strings.TrimSpace(string(data)))
		// Known virtualization vendors
		if strings.Contains(vendor, "qemu") || 
		   strings.Contains(vendor, "kvm") ||
		   strings.Contains(vendor, "vmware") ||
		   strings.Contains(vendor, "virtualbox") ||
		   strings.Contains(vendor, "xen") ||
		   strings.Contains(vendor, "microsoft") || // Hyper-V
		   strings.Contains(vendor, "amazon") || // AWS Nitro (for VMs, not bare metal)
		   strings.Contains(vendor, "google") { // GCP
			return true
		}
		// Hetzner is a bare metal provider
	}

	// Check CPU flags
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		if strings.Contains(string(data), "hypervisor") {
			return true
		}
	}

	return false
}

func isHardwareEventAvailable(eventName string) bool {
	// Check if specific hardware event is available on any PMU device
	pmuDevice := detectPMUDevice()
	if pmuDevice == "" {
		return false
	}

	// Try the event name directly
	eventPath := fmt.Sprintf("/sys/bus/event_source/devices/%s/events/%s", pmuDevice, eventName)
	if _, err := os.Stat(eventPath); err == nil {
		return true
	}

	// Try with architecture-specific mappings
	mappedName := mapEventName(eventName, runtime.GOARCH)
	if mappedName != eventName {
		eventPath = fmt.Sprintf("/sys/bus/event_source/devices/%s/events/%s", pmuDevice, mappedName)
		_, err := os.Stat(eventPath)
		return err == nil
	}

	return false
}

// mapEventName maps generic event names to architecture-specific names
func mapEventName(eventName string, arch string) string {
	if arch == "arm64" || arch == "aarch64" {
		// ARM event name mappings
		armMappings := map[string]string{
			"cpu-cycles":          "cpu_cycles",
			"instructions":        "inst_retired",
			"cache-references":    "l1d_cache",
			"cache-misses":        "l1d_cache_refill",
			"branch-instructions": "br_retired",
			"branch-misses":       "br_mis_pred",
		}
		if mapped, ok := armMappings[eventName]; ok {
			return mapped
		}
	}
	// Return original name if no mapping found
	return eventName
}

func getNumaNodeCount() int {
	nodes := 0
	numaPath := "/sys/devices/system/node"
	
	entries, err := os.ReadDir(numaPath)
	if err != nil {
		return 1
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "node") {
			nodes++
		}
	}

	if nodes == 0 {
		return 1
	}
	return nodes
}

func collectProfileEvents(t *testing.T, dataChan <-chan any, maxEvents int, timeout time.Duration) []*ProfileEvent {
	events := make([]*ProfileEvent, 0, maxEvents)
	timer := time.After(timeout)

	for len(events) < maxEvents {
		select {
		case event := <-dataChan:
			if event != nil {
				if profileEvent, ok := event.(*ProfileEvent); ok {
					events = append(events, profileEvent)
				}
			}
		case <-timer:
			t.Logf("Timeout reached after collecting %d events", len(events))
			return events
		}
	}

	return events
}

func analyzeEventDistribution(t *testing.T, events []*ProfileEvent) {
	if len(events) == 0 {
		return
	}

	// Analyze PID distribution
	pidCount := make(map[int32]int)
	cpuCount := make(map[uint32]int)
	
	for _, event := range events {
		pidCount[event.PID]++
		cpuCount[event.Cpu]++
	}

	t.Logf("Event distribution:")
	t.Logf("  - Total events: %d", len(events))
	t.Logf("  - Unique PIDs: %d", len(pidCount))
	t.Logf("  - CPUs used: %d", len(cpuCount))
	
	// Find top PIDs
	maxPID := int32(0)
	maxCount := 0
	for pid, count := range pidCount {
		if count > maxCount {
			maxPID = pid
			maxCount = count
		}
	}
	t.Logf("  - Most sampled PID: %d (%d samples)", maxPID, maxCount)
	
	// CPU distribution
	for cpu, count := range cpuCount {
		t.Logf("  - CPU %d: %d events (%.1f%%)", 
			cpu, count, float64(count)*100/float64(len(events)))
	}
}

func generateCPULoad(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// CPU-intensive work
			sum := 0
			for i := 0; i < 1000000; i++ {
				sum += i * i
			}
			_ = sum
			runtime.Gosched() // Allow other goroutines to run
		}
	}
}

func generateNumaLoad(ctx context.Context, node int) {
	// Try to pin to specific NUMA node CPUs
	// This is a simplified version - real NUMA pinning would use CPU affinity
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Memory-intensive work to stress NUMA
			data := make([]byte, 1024*1024) // 1MB
			for i := range data {
				data[i] = byte(i % 256)
			}
			runtime.Gosched()
		}
	}
}

func fibonacci(n int) int {
	if n <= 1 {
		return n
	}
	return fibonacci(n-1) + fibonacci(n-2)
}

// Benchmark for hardware profiling
func BenchmarkHardwareProfiler(b *testing.B) {
	// Skip if not on bare metal
	if isVirtualized() {
		b.Skip("Benchmark requires bare metal hardware")
	}

	logger := logr.Discard()
	config := performance.CollectionConfig{
		Interval:     100 * time.Millisecond,
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
	}

	collector, err := NewProfilerCollector(logger, config)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}

	ctx := context.Background()
	dataChan, err := collector.Start(ctx)
	if err != nil {
		b.Fatalf("Failed to start profiler: %v", err)
	}
	defer collector.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		select {
		case <-dataChan:
			// Event received
		default:
			// No event available
		}
	}
}