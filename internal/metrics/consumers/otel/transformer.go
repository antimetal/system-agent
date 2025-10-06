// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otel

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/antimetal/agent/internal/metrics"
	"github.com/antimetal/agent/pkg/performance"
)

const (
	// MaxAttributesPerMetric limits the number of attributes per metric to prevent OOM
	MaxAttributesPerMetric = 50
	// MaxInstrumentCacheSize limits the instrument cache size
	MaxInstrumentCacheSize = 1000
	// ErrorThresholdForHealthCheck defines when to log high error rates
	ErrorThresholdForHealthCheck = 100
	// HeartbeatInterval defines how often to log processing heartbeat
	HeartbeatInterval = 1000
	// DefaultSectorSize is the standard disk sector size in bytes
	DefaultSectorSize = 512
)

// Transformer converts generic metrics events to OpenTelemetry metrics
type Transformer struct {
	meter          metric.Meter
	logger         logr.Logger
	serviceVersion string // Service version to add as attribute

	// Cached instruments for performance
	instruments map[string]interface{}
	// instrumentsMutex protects the instruments map
	instrumentsMutex sync.RWMutex
}

// NewTransformer creates a new OpenTelemetry metrics transformer
func NewTransformer(meter metric.Meter, logger logr.Logger, serviceVersion string) *Transformer {
	return &Transformer{
		meter:          meter,
		logger:         logger.WithName("otel-transformer"),
		serviceVersion: serviceVersion,
		instruments:    make(map[string]interface{}),
	}
}

// TransformAndRecord converts a metrics event to OpenTelemetry format and records it.
// It supports 14 different metric types including load, memory, CPU, process, disk, network, TCP,
// system, kernel, and NUMA statistics. Returns an error if the transformation fails.
//
// Context Usage Note:
// We use context.Background() internally rather than accepting a context parameter because:
//  1. This is a metrics-only pipeline without distributed tracing - there's no trace context to propagate
//  2. Metric recording is synchronous and instant - cancellation isn't needed
//  3. We're not passing baggage or metadata across service boundaries
//
// The context in OpenTelemetry's gauge.Record(ctx, value) calls is primarily for:
//   - Distributed tracing correlation (associating metrics with traces/spans)
//   - Propagating baggage (key-value pairs across service boundaries)
//   - Cancellation signals (though metrics recording is typically instant)
//
// Since we're doing pure metrics collection, context.Background() provides the required
// parameter for the OpenTelemetry API while keeping our API honest and simple.
func (t *Transformer) TransformAndRecord(event metrics.MetricEvent) error {
	ctx := context.Background()
	switch event.MetricType {
	case metrics.MetricTypeLoad:
		return t.transformLoadStats(ctx, event.Data, t.buildAttributes(event))
	case metrics.MetricTypeMemory:
		return t.transformMemoryStats(ctx, event.Data, t.buildAttributes(event))
	case metrics.MetricTypeCPU:
		return t.transformCPUStats(ctx, event.Data, t.buildAttributes(event))
	case metrics.MetricTypeProcess:
		return t.transformProcessStats(ctx, event.Data, t.buildAttributes(event))
	case metrics.MetricTypeDisk:
		return t.transformDiskStats(ctx, event.Data, t.buildAttributes(event))
	case metrics.MetricTypeNetwork:
		return t.transformNetworkStats(ctx, event.Data, t.buildAttributes(event))
	case metrics.MetricTypeTCP:
		return t.transformTCPStats(ctx, event.Data, t.buildAttributes(event))
	case metrics.MetricTypeSystem:
		return t.transformSystemStats(ctx, event.Data, t.buildAttributes(event))
	case metrics.MetricTypeKernel:
		return t.transformKernelMessages(ctx, event.Data, t.buildAttributes(event))
	case metrics.MetricTypeCPUInfo, metrics.MetricTypeMemoryInfo,
		metrics.MetricTypeDiskInfo, metrics.MetricTypeNetworkInfo:
		// Info metrics are typically handled as resource attributes, not time-series metrics
		return nil
	case metrics.MetricTypeNUMAStats:
		return t.transformNUMAStats(ctx, event.Data, t.buildAttributes(event))
	default:
		t.logger.V(1).Info("Unknown metric type", "type", event.MetricType)
		return nil
	}
}

// buildAttributes constructs OpenTelemetry attributes from the event.
// It extracts host.name, k8s.cluster.name, service.instance.id, and service.version
// attributes from the standard MetricEvent fields and transformer configuration.
func (t *Transformer) buildAttributes(event metrics.MetricEvent) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 10)

	// Standard attributes from MetricEvent
	if event.NodeName != "" {
		attrs = append(attrs, attribute.String("host.name", event.NodeName))
	}
	if event.ClusterName != "" {
		attrs = append(attrs, attribute.String("k8s.cluster.name", event.ClusterName))
	}
	if event.Source != "" {
		attrs = append(attrs, attribute.String("service.instance.id", event.Source))
	}

	// Add service version if configured
	if t.serviceVersion != "" {
		attrs = append(attrs, attribute.String("service.version", t.serviceVersion))
	}

	return attrs
}

// getOrCreateFloat64Gauge gets or creates a Float64Gauge instrument
func (t *Transformer) getOrCreateFloat64Gauge(name, description, unit string) (metric.Float64Gauge, error) {
	key := fmt.Sprintf("f64_gauge_%s", name)

	// First, try to read with read lock
	t.instrumentsMutex.RLock()
	if inst, exists := t.instruments[key]; exists {
		t.instrumentsMutex.RUnlock()
		return inst.(metric.Float64Gauge), nil
	}
	t.instrumentsMutex.RUnlock()

	// Need to create instrument, acquire write lock
	t.instrumentsMutex.Lock()
	defer t.instrumentsMutex.Unlock()

	// Double-check after acquiring write lock
	if inst, exists := t.instruments[key]; exists {
		return inst.(metric.Float64Gauge), nil
	}

	// Check cache size limit
	if len(t.instruments) >= MaxInstrumentCacheSize {
		t.logger.V(1).Info("Instrument cache size limit reached", "current_size", len(t.instruments), "limit", MaxInstrumentCacheSize)
		// Still create the instrument, but don't cache it
		return t.meter.Float64Gauge(name,
			metric.WithDescription(description),
			metric.WithUnit(unit))
	}

	gauge, err := t.meter.Float64Gauge(name,
		metric.WithDescription(description),
		metric.WithUnit(unit))
	if err != nil {
		return nil, err
	}

	t.instruments[key] = gauge
	return gauge, nil
}

// getOrCreateInt64Gauge gets or creates an Int64Gauge instrument
func (t *Transformer) getOrCreateInt64Gauge(name, description, unit string) (metric.Int64Gauge, error) {
	key := fmt.Sprintf("i64_gauge_%s", name)

	// First, try to read with read lock
	t.instrumentsMutex.RLock()
	if inst, exists := t.instruments[key]; exists {
		t.instrumentsMutex.RUnlock()
		return inst.(metric.Int64Gauge), nil
	}
	t.instrumentsMutex.RUnlock()

	// Need to create instrument, acquire write lock
	t.instrumentsMutex.Lock()
	defer t.instrumentsMutex.Unlock()

	// Double-check after acquiring write lock
	if inst, exists := t.instruments[key]; exists {
		return inst.(metric.Int64Gauge), nil
	}

	// Check cache size limit
	if len(t.instruments) >= MaxInstrumentCacheSize {
		t.logger.V(1).Info("Instrument cache size limit reached", "current_size", len(t.instruments), "limit", MaxInstrumentCacheSize)
		// Still create the instrument, but don't cache it
		return t.meter.Int64Gauge(name,
			metric.WithDescription(description),
			metric.WithUnit(unit))
	}

	gauge, err := t.meter.Int64Gauge(name,
		metric.WithDescription(description),
		metric.WithUnit(unit))
	if err != nil {
		return nil, err
	}

	t.instruments[key] = gauge
	return gauge, nil
}

// getOrCreateInt64Counter gets or creates an Int64Counter instrument
func (t *Transformer) getOrCreateInt64Counter(name, description, unit string) (metric.Int64Counter, error) {
	key := fmt.Sprintf("i64_counter_%s", name)

	// First, try to read with read lock
	t.instrumentsMutex.RLock()
	if inst, exists := t.instruments[key]; exists {
		t.instrumentsMutex.RUnlock()
		return inst.(metric.Int64Counter), nil
	}
	t.instrumentsMutex.RUnlock()

	// Need to create instrument, acquire write lock
	t.instrumentsMutex.Lock()
	defer t.instrumentsMutex.Unlock()

	// Double-check after acquiring write lock
	if inst, exists := t.instruments[key]; exists {
		return inst.(metric.Int64Counter), nil
	}

	// Check cache size limit
	if len(t.instruments) >= MaxInstrumentCacheSize {
		t.logger.V(1).Info("Instrument cache size limit reached", "current_size", len(t.instruments), "limit", MaxInstrumentCacheSize)
		// Still create the instrument, but don't cache it
		return t.meter.Int64Counter(name,
			metric.WithDescription(description),
			metric.WithUnit(unit))
	}

	counter, err := t.meter.Int64Counter(name,
		metric.WithDescription(description),
		metric.WithUnit(unit))
	if err != nil {
		return nil, err
	}

	t.instruments[key] = counter
	return counter, nil
}

// transformLoadStats transforms system load statistics
func (t *Transformer) transformLoadStats(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	stats, ok := data.(*performance.LoadStats)
	if !ok {
		return fmt.Errorf("invalid load stats data type")
	}

	// Load averages
	if gauge, err := t.getOrCreateFloat64Gauge("system.cpu.load_average.1m", "System load average over 1 minute", "1"); err == nil {
		gauge.Record(ctx, stats.Load1Min, metric.WithAttributes(attrs...))
	}

	if gauge, err := t.getOrCreateFloat64Gauge("system.cpu.load_average.5m", "System load average over 5 minutes", "1"); err == nil {
		gauge.Record(ctx, stats.Load5Min, metric.WithAttributes(attrs...))
	}

	if gauge, err := t.getOrCreateFloat64Gauge("system.cpu.load_average.15m", "System load average over 15 minutes", "1"); err == nil {
		gauge.Record(ctx, stats.Load15Min, metric.WithAttributes(attrs...))
	}

	// Process counts
	if gauge, err := t.getOrCreateInt64Gauge("system.processes.count", "Number of processes", "1"); err == nil {
		stateAttrs := append(attrs, attribute.String("state", "running"))
		gauge.Record(ctx, int64(stats.RunningProcs), metric.WithAttributes(stateAttrs...))
	}

	// Uptime
	if gauge, err := t.getOrCreateFloat64Gauge("system.uptime", "System uptime", "s"); err == nil {
		gauge.Record(ctx, stats.Uptime.Seconds(), metric.WithAttributes(attrs...))
	}

	return nil
}

// transformMemoryStats transforms memory usage statistics
func (t *Transformer) transformMemoryStats(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	stats, ok := data.(*performance.MemoryStats)
	if !ok {
		return fmt.Errorf("invalid memory stats data type")
	}

	// Memory usage by state (existing metric - kept for compatibility)
	memStates := map[string]uint64{
		"free":      stats.MemFree * 1024, // Convert kB to bytes
		"used":      (stats.MemTotal - stats.MemAvailable) * 1024,
		"available": stats.MemAvailable * 1024,
		"buffers":   stats.Buffers * 1024,
		"cached":    stats.Cached * 1024,
	}

	if gauge, err := t.getOrCreateInt64Gauge("system.memory.usage", "Memory usage by state", "By"); err == nil {
		for state, value := range memStates {
			stateAttrs := append(attrs, attribute.String("state", state))
			gauge.Record(ctx, int64(value), metric.WithAttributes(stateAttrs...))
		}
	}

	// Swap usage (existing metric - kept for compatibility)
	if stats.SwapTotal > 0 {
		if gauge, err := t.getOrCreateInt64Gauge("system.memory.swap_usage", "Swap usage", "By"); err == nil {
			freeAttrs := append(attrs, attribute.String("state", "free"))
			usedAttrs := append(attrs, attribute.String("state", "used"))
			gauge.Record(ctx, int64(stats.SwapFree*1024), metric.WithAttributes(freeAttrs...))
			gauge.Record(ctx, int64((stats.SwapTotal-stats.SwapFree)*1024), metric.WithAttributes(usedAttrs...))
		}
	}

	// Swap cached memory (gauge - instantaneous value)
	if gauge, err := t.getOrCreateInt64Gauge("system.memory.swap.cached", "Memory that was swapped out and is back in RAM", "By"); err == nil {
		gauge.Record(ctx, int64(stats.SwapCached*1024), metric.WithAttributes(attrs...))
	}

	// Active/Inactive memory (gauges - instantaneous values)
	if gauge, err := t.getOrCreateInt64Gauge("system.memory.active", "Memory used recently", "By"); err == nil {
		gauge.Record(ctx, int64(stats.Active*1024), metric.WithAttributes(attrs...))
	}

	if gauge, err := t.getOrCreateInt64Gauge("system.memory.inactive", "Memory not used recently", "By"); err == nil {
		gauge.Record(ctx, int64(stats.Inactive*1024), metric.WithAttributes(attrs...))
	}

	// Dirty/Writeback pages (gauges - instantaneous values)
	if gauge, err := t.getOrCreateInt64Gauge("system.memory.dirty", "Memory waiting to be written back to disk", "By"); err == nil {
		gauge.Record(ctx, int64(stats.Dirty*1024), metric.WithAttributes(attrs...))
	}

	if gauge, err := t.getOrCreateInt64Gauge("system.memory.writeback", "Memory actively being written back to disk", "By"); err == nil {
		gauge.Record(ctx, int64(stats.Writeback*1024), metric.WithAttributes(attrs...))
	}

	// Anonymous and mapped memory (gauges - instantaneous values)
	if gauge, err := t.getOrCreateInt64Gauge("system.memory.anon_pages", "Non-file backed pages mapped into userspace", "By"); err == nil {
		gauge.Record(ctx, int64(stats.AnonPages*1024), metric.WithAttributes(attrs...))
	}

	if gauge, err := t.getOrCreateInt64Gauge("system.memory.mapped", "Files mapped into memory", "By"); err == nil {
		gauge.Record(ctx, int64(stats.Mapped*1024), metric.WithAttributes(attrs...))
	}

	if gauge, err := t.getOrCreateInt64Gauge("system.memory.shmem", "Shared memory", "By"); err == nil {
		gauge.Record(ctx, int64(stats.Shmem*1024), metric.WithAttributes(attrs...))
	}

	// Slab allocator (gauge - instantaneous values)
	if gauge, err := t.getOrCreateInt64Gauge("system.memory.slab", "Kernel slab allocator memory by type", "By"); err == nil {
		reclaimableAttrs := append(attrs, attribute.String("type", "reclaimable"))
		unreclaimAttrs := append(attrs, attribute.String("type", "unreclaimable"))
		gauge.Record(ctx, int64(stats.SReclaimable*1024), metric.WithAttributes(reclaimableAttrs...))
		gauge.Record(ctx, int64(stats.SUnreclaim*1024), metric.WithAttributes(unreclaimAttrs...))
	}

	// Kernel memory (gauges - instantaneous values)
	if gauge, err := t.getOrCreateInt64Gauge("system.memory.kernel_stack", "Memory used by kernel stacks", "By"); err == nil {
		gauge.Record(ctx, int64(stats.KernelStack*1024), metric.WithAttributes(attrs...))
	}

	if gauge, err := t.getOrCreateInt64Gauge("system.memory.page_tables", "Memory used by page tables", "By"); err == nil {
		gauge.Record(ctx, int64(stats.PageTables*1024), metric.WithAttributes(attrs...))
	}

	// Memory commit (gauges - instantaneous values)
	if gauge, err := t.getOrCreateInt64Gauge("system.memory.commit", "Memory commit statistics by type", "By"); err == nil {
		limitAttrs := append(attrs, attribute.String("type", "limit"))
		committedAttrs := append(attrs, attribute.String("type", "committed_as"))
		gauge.Record(ctx, int64(stats.CommitLimit*1024), metric.WithAttributes(limitAttrs...))
		gauge.Record(ctx, int64(stats.CommittedAS*1024), metric.WithAttributes(committedAttrs...))
	}

	// Virtual memory allocator (gauges - instantaneous values)
	if gauge, err := t.getOrCreateInt64Gauge("system.memory.vmalloc", "Virtual memory allocator statistics by type", "By"); err == nil {
		totalAttrs := append(attrs, attribute.String("type", "total"))
		usedAttrs := append(attrs, attribute.String("type", "used"))
		gauge.Record(ctx, int64(stats.VmallocTotal*1024), metric.WithAttributes(totalAttrs...))
		gauge.Record(ctx, int64(stats.VmallocUsed*1024), metric.WithAttributes(usedAttrs...))
	}

	// HugePages (gauges - instantaneous values)
	if stats.HugePages_Total > 0 {
		if gauge, err := t.getOrCreateInt64Gauge("system.memory.huge_pages", "HugePages by state", "1"); err == nil {
			totalAttrs := append(attrs, attribute.String("state", "total"))
			freeAttrs := append(attrs, attribute.String("state", "free"))
			rsvdAttrs := append(attrs, attribute.String("state", "reserved"))
			surpAttrs := append(attrs, attribute.String("state", "surplus"))
			gauge.Record(ctx, int64(stats.HugePages_Total), metric.WithAttributes(totalAttrs...))
			gauge.Record(ctx, int64(stats.HugePages_Free), metric.WithAttributes(freeAttrs...))
			gauge.Record(ctx, int64(stats.HugePages_Rsvd), metric.WithAttributes(rsvdAttrs...))
			gauge.Record(ctx, int64(stats.HugePages_Surp), metric.WithAttributes(surpAttrs...))
		}

		// HugeTLB total memory consumed (gauge - instantaneous value)
		if gauge, err := t.getOrCreateInt64Gauge("system.memory.huge_pages.bytes", "Total memory consumed by huge pages", "By"); err == nil {
			gauge.Record(ctx, int64(stats.Hugetlb*1024), metric.WithAttributes(attrs...))
		}
	}

	// Counter metrics require delta calculations
	// Skip if delta data is not available (first collection or delta mode disabled)
	if stats.Delta == nil {
		t.logger.V(2).Info("Memory delta data not available, skipping swap I/O counter metrics")
		return nil
	}

	// Swap I/O activity (using deltas - these are page counts from /proc/vmstat)
	if counter, err := t.getOrCreateInt64Counter("system.memory.swap.io", "Pages swapped in/out by direction", "1"); err == nil {
		inAttrs := append(attrs, attribute.String("direction", "in"))
		outAttrs := append(attrs, attribute.String("direction", "out"))
		counter.Add(ctx, int64(stats.Delta.SwapIn), metric.WithAttributes(inAttrs...))
		counter.Add(ctx, int64(stats.Delta.SwapOut), metric.WithAttributes(outAttrs...))
	}

	return nil
}

// transformCPUStats transforms CPU usage statistics
func (t *Transformer) transformCPUStats(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	cpuStats, ok := data.([]*performance.CPUStats)
	if !ok {
		return fmt.Errorf("invalid CPU stats data type: expected []*performance.CPUStats, got %T", data)
	}

	gauge, err := t.getOrCreateFloat64Gauge("system.cpu.utilization", "CPU utilization by state", "1")
	if err != nil {
		return err
	}

	for _, cpu := range cpuStats {
		if err := t.recordCPUUtilization(ctx, gauge, cpu, attrs); err != nil {
			t.logger.V(1).Info("Failed to record CPU utilization", "cpu", cpu.CPUIndex, "error", err)
			// Continue processing other CPUs
		}
	}

	return nil
}

// recordCPUUtilization records CPU utilization metrics for a single CPU
func (t *Transformer) recordCPUUtilization(ctx context.Context, gauge metric.Float64Gauge, cpu *performance.CPUStats, baseAttrs []attribute.KeyValue) error {
	cpuAttrs := append(baseAttrs, attribute.String("cpu", strconv.Itoa(int(cpu.CPUIndex))))

	total := t.calculateCPUTotal(cpu)
	if total == 0 {
		return nil // Skip CPUs with no activity
	}

	states := t.calculateCPUStates(cpu, total)
	for state, value := range states {
		stateAttrs := append(cpuAttrs, attribute.String("state", state))
		gauge.Record(ctx, value, metric.WithAttributes(stateAttrs...))
	}

	return nil
}

// calculateCPUTotal calculates the total CPU time across all states
func (t *Transformer) calculateCPUTotal(cpu *performance.CPUStats) uint64 {
	return cpu.User + cpu.Nice + cpu.System + cpu.Idle + cpu.IOWait + cpu.IRQ + cpu.SoftIRQ + cpu.Steal + cpu.Guest + cpu.GuestNice
}

// calculateCPUStates calculates CPU utilization percentages by state
func (t *Transformer) calculateCPUStates(cpu *performance.CPUStats, total uint64) map[string]float64 {
	return map[string]float64{
		"user":   float64(cpu.User) / float64(total),
		"system": float64(cpu.System) / float64(total),
		"idle":   float64(cpu.Idle) / float64(total),
		"iowait": float64(cpu.IOWait) / float64(total),
		"steal":  float64(cpu.Steal) / float64(total),
	}
}

// transformProcessStats transforms process statistics
func (t *Transformer) transformProcessStats(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	processes, ok := data.([]*performance.ProcessStats)
	if !ok {
		return fmt.Errorf("invalid process stats data type: expected []*performance.ProcessStats, got %T", data)
	}

	// Aggregate metrics
	totalMemoryRSS := uint64(0)
	totalThreads := int64(0)

	for _, proc := range processes {
		totalMemoryRSS += proc.MemoryRSS
		totalThreads += int64(proc.Threads)
	}

	if gauge, err := t.getOrCreateInt64Gauge("system.processes.count", "Number of processes", "1"); err == nil {
		gauge.Record(ctx, int64(len(processes)), metric.WithAttributes(attrs...))
	}

	if gauge, err := t.getOrCreateInt64Gauge("system.memory.usage", "Memory usage by processes", "By"); err == nil {
		processAttrs := append(attrs, attribute.String("state", "processes"))
		gauge.Record(ctx, int64(totalMemoryRSS), metric.WithAttributes(processAttrs...))
	}

	return nil
}

// transformDiskStats transforms disk I/O statistics
func (t *Transformer) transformDiskStats(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	disks, ok := data.([]*performance.DiskStats)
	if !ok {
		return fmt.Errorf("invalid disk stats data type: expected []*performance.DiskStats, got %T", data)
	}

	for _, disk := range disks {
		diskAttrs := append(attrs, attribute.String("device", disk.Device))

		// I/O operations in progress (gauge - instantaneous value)
		if gauge, err := t.getOrCreateInt64Gauge("system.disk.io.in_progress", "I/O operations currently in progress", "1"); err == nil {
			gauge.Record(ctx, int64(disk.IOsInProgress), metric.WithAttributes(diskAttrs...))
		}

		// Counter metrics require delta calculations
		// Skip if delta data is not available (first collection or delta mode disabled)
		if disk.Delta == nil {
			t.logger.V(2).Info("Disk delta data not available, skipping counter metrics", "device", disk.Device)
			continue
		}

		// I/O operations completed (using deltas)
		if counter, err := t.getOrCreateInt64Counter("system.disk.operations", "Disk I/O operations completed", "1"); err == nil {
			readAttrs := append(diskAttrs, attribute.String("direction", "read"))
			writeAttrs := append(diskAttrs, attribute.String("direction", "write"))
			counter.Add(ctx, int64(disk.Delta.ReadsCompleted), metric.WithAttributes(readAttrs...))
			counter.Add(ctx, int64(disk.Delta.WritesCompleted), metric.WithAttributes(writeAttrs...))
		}

		// I/O operations merged (using deltas)
		if counter, err := t.getOrCreateInt64Counter("system.disk.operations.merged", "Disk I/O operations merged before queuing", "1"); err == nil {
			readAttrs := append(diskAttrs, attribute.String("direction", "read"))
			writeAttrs := append(diskAttrs, attribute.String("direction", "write"))
			counter.Add(ctx, int64(disk.Delta.ReadsMerged), metric.WithAttributes(readAttrs...))
			counter.Add(ctx, int64(disk.Delta.WritesMerged), metric.WithAttributes(writeAttrs...))
		}

		// I/O bytes (using deltas)
		if counter, err := t.getOrCreateInt64Counter("system.disk.io", "Disk I/O bytes", "By"); err == nil {
			readAttrs := append(diskAttrs, attribute.String("direction", "read"))
			writeAttrs := append(diskAttrs, attribute.String("direction", "write"))
			counter.Add(ctx, int64(disk.Delta.SectorsRead*DefaultSectorSize), metric.WithAttributes(readAttrs...))
			counter.Add(ctx, int64(disk.Delta.SectorsWritten*DefaultSectorSize), metric.WithAttributes(writeAttrs...))
		}

		// I/O operation time (using deltas - these are cumulative milliseconds)
		if counter, err := t.getOrCreateInt64Counter("system.disk.io.time", "Time spent on disk I/O operations", "ms"); err == nil {
			readAttrs := append(diskAttrs, attribute.String("direction", "read"))
			writeAttrs := append(diskAttrs, attribute.String("direction", "write"))
			counter.Add(ctx, int64(disk.Delta.ReadTime), metric.WithAttributes(readAttrs...))
			counter.Add(ctx, int64(disk.Delta.WriteTime), metric.WithAttributes(writeAttrs...))
		}

		// Active I/O time (using deltas - cumulative milliseconds)
		if counter, err := t.getOrCreateInt64Counter("system.disk.io.active_time", "Time disk was active doing I/O", "ms"); err == nil {
			counter.Add(ctx, int64(disk.Delta.IOTime), metric.WithAttributes(diskAttrs...))
		}

		// Weighted I/O time (using deltas - cumulative milliseconds)
		if counter, err := t.getOrCreateInt64Counter("system.disk.io.weighted_time", "Weighted time spent on disk I/O", "ms"); err == nil {
			counter.Add(ctx, int64(disk.Delta.WeightedIOTime), metric.WithAttributes(diskAttrs...))
		}
	}

	return nil
}

// transformNetworkStats transforms network interface statistics
func (t *Transformer) transformNetworkStats(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	interfaces, ok := data.([]*performance.NetworkStats)
	if !ok {
		return fmt.Errorf("invalid network stats data type: expected []*performance.NetworkStats, got %T", data)
	}

	for _, iface := range interfaces {
		ifaceAttrs := append(attrs, attribute.String("device", iface.Interface))

		// Interface state gauges (instantaneous values)
		if gauge, err := t.getOrCreateInt64Gauge("system.network.interface.speed", "Network interface link speed", "Mbit/s"); err == nil {
			gauge.Record(ctx, int64(iface.Speed), metric.WithAttributes(ifaceAttrs...))
		}

		if gauge, err := t.getOrCreateInt64Gauge("system.network.interface.up", "Network interface operational state", "1"); err == nil {
			// 1 if interface is up, 0 otherwise
			operState := int64(0)
			if iface.OperState == "up" {
				operState = 1
			}
			gauge.Record(ctx, operState, metric.WithAttributes(ifaceAttrs...))
		}

		if gauge, err := t.getOrCreateInt64Gauge("system.network.interface.carrier", "Network interface carrier detection", "1"); err == nil {
			// 1 if carrier detected, 0 otherwise
			carrier := int64(0)
			if iface.LinkDetected {
				carrier = 1
			}
			gauge.Record(ctx, carrier, metric.WithAttributes(ifaceAttrs...))
		}

		// Counter metrics require delta calculations
		// Skip if delta data is not available (first collection or delta mode disabled)
		if iface.Delta == nil {
			t.logger.V(2).Info("Network delta data not available, skipping counter metrics", "interface", iface.Interface)
			continue
		}

		// Network I/O bytes (using deltas)
		if counter, err := t.getOrCreateInt64Counter("system.network.io", "Network I/O bytes", "By"); err == nil {
			rxAttrs := append(ifaceAttrs, attribute.String("direction", "receive"))
			txAttrs := append(ifaceAttrs, attribute.String("direction", "transmit"))
			counter.Add(ctx, int64(iface.Delta.RxBytes), metric.WithAttributes(rxAttrs...))
			counter.Add(ctx, int64(iface.Delta.TxBytes), metric.WithAttributes(txAttrs...))
		}

		// Network packets (using deltas)
		if counter, err := t.getOrCreateInt64Counter("system.network.packets", "Network packets", "1"); err == nil {
			rxAttrs := append(ifaceAttrs, attribute.String("direction", "receive"))
			txAttrs := append(ifaceAttrs, attribute.String("direction", "transmit"))
			counter.Add(ctx, int64(iface.Delta.RxPackets), metric.WithAttributes(rxAttrs...))
			counter.Add(ctx, int64(iface.Delta.TxPackets), metric.WithAttributes(txAttrs...))
		}

		// Network errors (using deltas)
		if counter, err := t.getOrCreateInt64Counter("system.network.errors", "Network transmission errors", "1"); err == nil {
			rxAttrs := append(ifaceAttrs, attribute.String("direction", "receive"))
			txAttrs := append(ifaceAttrs, attribute.String("direction", "transmit"))
			counter.Add(ctx, int64(iface.Delta.RxErrors), metric.WithAttributes(rxAttrs...))
			counter.Add(ctx, int64(iface.Delta.TxErrors), metric.WithAttributes(txAttrs...))
		}

		// Network dropped packets (using deltas)
		if counter, err := t.getOrCreateInt64Counter("system.network.dropped", "Network packets dropped", "1"); err == nil {
			rxAttrs := append(ifaceAttrs, attribute.String("direction", "receive"))
			txAttrs := append(ifaceAttrs, attribute.String("direction", "transmit"))
			counter.Add(ctx, int64(iface.Delta.RxDropped), metric.WithAttributes(rxAttrs...))
			counter.Add(ctx, int64(iface.Delta.TxDropped), metric.WithAttributes(txAttrs...))
		}

		// Note: Additional error types (FIFO, Frame, Compressed, Multicast, Collisions, Carrier)
		// are not yet exported because NetworkDeltaData doesn't include delta calculations for them.
		// These should be added to NetworkDeltaData in pkg/performance/types.go first.
	}

	return nil
}

// transformTCPStats transforms TCP connection statistics
func (t *Transformer) transformTCPStats(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	stats, ok := data.(*performance.TCPStats)
	if !ok {
		return fmt.Errorf("invalid TCP stats data type")
	}

	// TCP connection counts by state (gauge - instantaneous values)
	if gauge, err := t.getOrCreateInt64Gauge("system.network.connections", "Network connections by protocol and state", "1"); err == nil {
		tcpAttrs := append(attrs, attribute.String("protocol", "tcp"))
		for state, count := range stats.ConnectionsByState {
			stateAttrs := append(tcpAttrs, attribute.String("state", state))
			gauge.Record(ctx, int64(count), metric.WithAttributes(stateAttrs...))
		}
	}

	// Current established connections (gauge - instantaneous value)
	if gauge, err := t.getOrCreateInt64Gauge("system.network.tcp.connection.established", "Current established TCP connections", "1"); err == nil {
		gauge.Record(ctx, int64(stats.CurrEstab), metric.WithAttributes(attrs...))
	}

	// Counter metrics require delta calculations
	// Skip if delta data is not available (first collection or delta mode disabled)
	if stats.Delta == nil {
		t.logger.V(2).Info("TCP delta data not available, skipping counter metrics")
		return nil
	}

	// TCP connection establishment metrics (using deltas)
	if counter, err := t.getOrCreateInt64Counter("system.network.tcp.connection.opens", "TCP connection openings by type", "1"); err == nil {
		activeAttrs := append(attrs, attribute.String("type", "active"))
		passiveAttrs := append(attrs, attribute.String("type", "passive"))
		counter.Add(ctx, int64(stats.Delta.ActiveOpens), metric.WithAttributes(activeAttrs...))
		counter.Add(ctx, int64(stats.Delta.PassiveOpens), metric.WithAttributes(passiveAttrs...))
	}

	// TCP connection failures (using deltas)
	if counter, err := t.getOrCreateInt64Counter("system.network.tcp.connection.attempt_fails", "Failed TCP connection attempts", "1"); err == nil {
		counter.Add(ctx, int64(stats.Delta.AttemptFails), metric.WithAttributes(attrs...))
	}

	// TCP connection resets from established state (using deltas)
	if counter, err := t.getOrCreateInt64Counter("system.network.tcp.connection.estab_resets", "TCP resets from established state", "1"); err == nil {
		counter.Add(ctx, int64(stats.Delta.EstabResets), metric.WithAttributes(attrs...))
	}

	// TCP segments (using deltas)
	if counter, err := t.getOrCreateInt64Counter("system.network.tcp.segments", "TCP segments by direction", "1"); err == nil {
		inAttrs := append(attrs, attribute.String("direction", "in"))
		outAttrs := append(attrs, attribute.String("direction", "out"))
		counter.Add(ctx, int64(stats.Delta.InSegs), metric.WithAttributes(inAttrs...))
		counter.Add(ctx, int64(stats.Delta.OutSegs), metric.WithAttributes(outAttrs...))
	}

	// TCP retransmissions (using deltas)
	if counter, err := t.getOrCreateInt64Counter("system.network.tcp.retrans_segs", "TCP segments retransmitted", "1"); err == nil {
		counter.Add(ctx, int64(stats.Delta.RetransSegs), metric.WithAttributes(attrs...))
	}

	// TCP errors (using deltas)
	if counter, err := t.getOrCreateInt64Counter("system.network.tcp.errors", "TCP errors by type", "1"); err == nil {
		inErrAttrs := append(attrs, attribute.String("type", "in_errors"))
		outRstAttrs := append(attrs, attribute.String("type", "out_resets"))
		csumAttrs := append(attrs, attribute.String("type", "checksum_errors"))
		counter.Add(ctx, int64(stats.Delta.InErrs), metric.WithAttributes(inErrAttrs...))
		counter.Add(ctx, int64(stats.Delta.OutRsts), metric.WithAttributes(outRstAttrs...))
		counter.Add(ctx, int64(stats.Delta.InCsumErrors), metric.WithAttributes(csumAttrs...))
	}

	// SYN cookies (using deltas)
	if counter, err := t.getOrCreateInt64Counter("system.network.tcp.syncookies", "SYN cookies by result", "1"); err == nil {
		sentAttrs := append(attrs, attribute.String("result", "sent"))
		recvAttrs := append(attrs, attribute.String("result", "received"))
		failedAttrs := append(attrs, attribute.String("result", "failed"))
		counter.Add(ctx, int64(stats.Delta.SyncookiesSent), metric.WithAttributes(sentAttrs...))
		counter.Add(ctx, int64(stats.Delta.SyncookiesRecv), metric.WithAttributes(recvAttrs...))
		counter.Add(ctx, int64(stats.Delta.SyncookiesFailed), metric.WithAttributes(failedAttrs...))
	}

	// Listen queue issues (using deltas)
	if counter, err := t.getOrCreateInt64Counter("system.network.tcp.listen_issues", "TCP listen queue issues by type", "1"); err == nil {
		overflowAttrs := append(attrs, attribute.String("type", "overflows"))
		dropAttrs := append(attrs, attribute.String("type", "drops"))
		counter.Add(ctx, int64(stats.Delta.ListenOverflows), metric.WithAttributes(overflowAttrs...))
		counter.Add(ctx, int64(stats.Delta.ListenDrops), metric.WithAttributes(dropAttrs...))
	}

	// TCP retransmission details (using deltas)
	if counter, err := t.getOrCreateInt64Counter("system.network.tcp.retrans_detail", "TCP retransmissions by type", "1"); err == nil {
		lostAttrs := append(attrs, attribute.String("type", "lost"))
		fastAttrs := append(attrs, attribute.String("type", "fast"))
		slowStartAttrs := append(attrs, attribute.String("type", "slow_start"))
		counter.Add(ctx, int64(stats.Delta.TCPLostRetransmit), metric.WithAttributes(lostAttrs...))
		counter.Add(ctx, int64(stats.Delta.TCPFastRetrans), metric.WithAttributes(fastAttrs...))
		counter.Add(ctx, int64(stats.Delta.TCPSlowStartRetrans), metric.WithAttributes(slowStartAttrs...))
	}

	// TCP timeouts (using deltas)
	if counter, err := t.getOrCreateInt64Counter("system.network.tcp.timeouts", "TCP retransmission timeouts", "1"); err == nil {
		counter.Add(ctx, int64(stats.Delta.TCPTimeouts), metric.WithAttributes(attrs...))
	}

	return nil
}

// transformSystemStats transforms system-wide statistics
func (t *Transformer) transformSystemStats(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	stats, ok := data.(*performance.SystemStats)
	if !ok {
		return fmt.Errorf("invalid system stats data type")
	}

	// Interrupts and context switches as counters
	if counter, err := t.getOrCreateInt64Counter("system.interrupts", "System interrupts", "1"); err == nil {
		counter.Add(ctx, int64(stats.Interrupts), metric.WithAttributes(attrs...))
	}

	if counter, err := t.getOrCreateInt64Counter("system.context_switches", "System context switches", "1"); err == nil {
		counter.Add(ctx, int64(stats.ContextSwitches), metric.WithAttributes(attrs...))
	}

	return nil
}

// transformKernelMessages transforms kernel log messages
func (t *Transformer) transformKernelMessages(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	messages, ok := data.([]performance.KernelMessage)
	if !ok {
		return fmt.Errorf("invalid kernel messages data type: expected []performance.KernelMessage, got %T", data)
	}

	// Count messages by severity
	severityCounts := make(map[string]int64)
	for _, msg := range messages {
		severity := fmt.Sprintf("severity_%d", msg.Severity)
		severityCounts[severity]++
	}

	if counter, err := t.getOrCreateInt64Counter("system.kernel.messages", "Kernel messages by severity", "1"); err == nil {
		for severity, count := range severityCounts {
			severityAttrs := append(attrs, attribute.String("severity", severity))
			counter.Add(ctx, count, metric.WithAttributes(severityAttrs...))
		}
	}

	return nil
}

func (t *Transformer) transformNUMAStats(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	stats, ok := data.(*performance.NUMAStatistics)
	if !ok {
		return fmt.Errorf("invalid NUMA stats data type")
	}

	if !stats.Enabled {
		return nil
	}

	// NUMA node memory usage
	if gauge, err := t.getOrCreateInt64Gauge("system.memory.usage", "NUMA memory usage", "By"); err == nil {
		for _, node := range stats.Nodes {
			nodeAttrs := append(attrs,
				attribute.String("numa_node", strconv.Itoa(node.ID)),
				attribute.String("state", "free"))
			gauge.Record(ctx, int64(node.MemFree), metric.WithAttributes(nodeAttrs...))
		}
	}

	return nil
}
