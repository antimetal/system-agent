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
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/antimetal/agent/pkg/metrics"
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

// instrumentCacheEntry tracks an instrument and its last access time for LRU eviction
type instrumentCacheEntry struct {
	instrument   interface{}
	lastAccessed time.Time
}

// Transformer converts generic metrics events to OpenTelemetry metrics
type Transformer struct {
	meter  metric.Meter
	logger logr.Logger

	// Cached instruments for performance with LRU tracking
	instruments map[string]*instrumentCacheEntry
	// instrumentsMutex protects the instruments map
	instrumentsMutex sync.RWMutex
	// Track total cache size for eviction
	cacheSize int
}

// NewTransformer creates a new OpenTelemetry metrics transformer
func NewTransformer(meter metric.Meter, logger logr.Logger) *Transformer {
	return &Transformer{
		meter:       meter,
		logger:      logger.WithName("otel-transformer"),
		instruments: make(map[string]*instrumentCacheEntry),
		cacheSize:   0,
	}
}

// evictOldestInstrument removes the oldest accessed instrument from cache
// Must be called with write lock held
func (t *Transformer) evictOldestInstrument() {
	if len(t.instruments) == 0 {
		return
	}

	var oldestKey string
	var oldestTime time.Time
	first := true

	for key, entry := range t.instruments {
		if first || entry.lastAccessed.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.lastAccessed
			first = false
		}
	}

	if oldestKey != "" {
		delete(t.instruments, oldestKey)
		t.cacheSize--
		t.logger.V(2).Info("Evicted oldest instrument from cache", "key", oldestKey)
	}
}

// getCachedInstrument retrieves an instrument from cache and updates access time
func (t *Transformer) getCachedInstrument(key string) (interface{}, bool) {
	t.instrumentsMutex.RLock()
	entry, exists := t.instruments[key]
	t.instrumentsMutex.RUnlock()

	if exists {
		// Update access time with write lock
		t.instrumentsMutex.Lock()
		entry.lastAccessed = time.Now()
		t.instrumentsMutex.Unlock()
		return entry.instrument, true
	}
	return nil, false
}

// setCachedInstrument stores an instrument in cache with eviction if needed
func (t *Transformer) setCachedInstrument(key string, instrument interface{}) {
	t.instrumentsMutex.Lock()
	defer t.instrumentsMutex.Unlock()

	// Check if we need to evict
	if t.cacheSize >= MaxInstrumentCacheSize {
		t.evictOldestInstrument()
	}

	t.instruments[key] = &instrumentCacheEntry{
		instrument:   instrument,
		lastAccessed: time.Now(),
	}
	t.cacheSize++
}

// TransformAndRecord converts a metrics event to OpenTelemetry format and records it.
// It supports 14 different metric types including load, memory, CPU, process, disk, network, TCP,
// system, kernel, and NUMA statistics. Returns an error if the transformation fails.
func (t *Transformer) TransformAndRecord(ctx context.Context, event metrics.MetricEvent) error {
	switch event.MetricType {
	case "load":
		return t.transformLoadStats(ctx, event.Data, t.buildAttributes(event))
	case "memory":
		return t.transformMemoryStats(ctx, event.Data, t.buildAttributes(event))
	case "cpu":
		return t.transformCPUStats(ctx, event.Data, t.buildAttributes(event))
	case "process":
		return t.transformProcessStats(ctx, event.Data, t.buildAttributes(event))
	case "disk":
		return t.transformDiskStats(ctx, event.Data, t.buildAttributes(event))
	case "network":
		return t.transformNetworkStats(ctx, event.Data, t.buildAttributes(event))
	case "tcp":
		return t.transformTCPStats(ctx, event.Data, t.buildAttributes(event))
	case "system":
		return t.transformSystemStats(ctx, event.Data, t.buildAttributes(event))
	case "kernel":
		return t.transformKernelMessages(ctx, event.Data, t.buildAttributes(event))
	case "cpu_info", "memory_info", "disk_info", "network_info":
		// Info metrics are typically handled as resource attributes, not time-series metrics
		return nil
	case "numa_stats":
		return t.transformNUMAStats(ctx, event.Data, t.buildAttributes(event))
	default:
		t.logger.V(1).Info("Unknown metric type", "type", event.MetricType)
		return nil
	}
}

// buildAttributes constructs OpenTelemetry attributes from the event
// buildAttributes constructs OpenTelemetry attributes from the event.
// It extracts host.name, k8s.cluster.name, service.instance.id and custom tags,
// with protection against excessive attributes to prevent OOM conditions.
func (t *Transformer) buildAttributes(event metrics.MetricEvent) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 10)

	if event.NodeName != "" {
		attrs = append(attrs, attribute.String("host.name", event.NodeName))
	}
	if event.ClusterName != "" {
		attrs = append(attrs, attribute.String("k8s.cluster.name", event.ClusterName))
	}
	if event.Source != "" {
		attrs = append(attrs, attribute.String("service.instance.id", event.Source))
	}

	return attrs
}

// getOrCreateFloat64Gauge gets or creates a Float64Gauge instrument
func (t *Transformer) getOrCreateFloat64Gauge(name, description, unit string) (metric.Float64Gauge, error) {
	key := fmt.Sprintf("f64_gauge_%s", name)

	// Try to get from cache
	if inst, exists := t.getCachedInstrument(key); exists {
		return inst.(metric.Float64Gauge), nil
	}

	// Create new instrument
	gauge, err := t.meter.Float64Gauge(name,
		metric.WithDescription(description),
		metric.WithUnit(unit))
	if err != nil {
		return nil, err
	}

	// Cache it with automatic eviction if needed
	t.setCachedInstrument(key, gauge)
	return gauge, nil
}

// getOrCreateInt64Gauge gets or creates an Int64Gauge instrument
func (t *Transformer) getOrCreateInt64Gauge(name, description, unit string) (metric.Int64Gauge, error) {
	key := fmt.Sprintf("i64_gauge_%s", name)

	// Try to get from cache
	if inst, exists := t.getCachedInstrument(key); exists {
		return inst.(metric.Int64Gauge), nil
	}

	// Create new instrument
	gauge, err := t.meter.Int64Gauge(name,
		metric.WithDescription(description),
		metric.WithUnit(unit))
	if err != nil {
		return nil, err
	}

	// Cache it with automatic eviction if needed
	t.setCachedInstrument(key, gauge)
	return gauge, nil
}

// getOrCreateInt64Counter gets or creates an Int64Counter instrument
func (t *Transformer) getOrCreateInt64Counter(name, description, unit string) (metric.Int64Counter, error) {
	key := fmt.Sprintf("i64_counter_%s", name)

	// Try to get from cache
	if inst, exists := t.getCachedInstrument(key); exists {
		return inst.(metric.Int64Counter), nil
	}

	// Create new instrument
	counter, err := t.meter.Int64Counter(name,
		metric.WithDescription(description),
		metric.WithUnit(unit))
	if err != nil {
		return nil, err
	}

	// Cache it with automatic eviction if needed
	t.setCachedInstrument(key, counter)
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

	// Memory usage by state
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

	// Swap usage
	if stats.SwapTotal > 0 {
		if gauge, err := t.getOrCreateInt64Gauge("system.memory.swap_usage", "Swap usage", "By"); err == nil {
			freeAttrs := append(attrs, attribute.String("state", "free"))
			usedAttrs := append(attrs, attribute.String("state", "used"))
			gauge.Record(ctx, int64(stats.SwapFree*1024), metric.WithAttributes(freeAttrs...))
			gauge.Record(ctx, int64((stats.SwapTotal-stats.SwapFree)*1024), metric.WithAttributes(usedAttrs...))
		}
	}

	return nil
}

// transformCPUStats transforms CPU usage statistics
func (t *Transformer) transformCPUStats(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	cpuStats, ok := data.([]performance.CPUStats)
	if !ok {
		return fmt.Errorf("invalid CPU stats data type")
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
func (t *Transformer) recordCPUUtilization(ctx context.Context, gauge metric.Float64Gauge, cpu performance.CPUStats, baseAttrs []attribute.KeyValue) error {
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
func (t *Transformer) calculateCPUTotal(cpu performance.CPUStats) uint64 {
	return cpu.User + cpu.Nice + cpu.System + cpu.Idle + cpu.IOWait + cpu.IRQ + cpu.SoftIRQ + cpu.Steal + cpu.Guest + cpu.GuestNice
}

// calculateCPUStates calculates CPU utilization percentages by state
func (t *Transformer) calculateCPUStates(cpu performance.CPUStats, total uint64) map[string]float64 {
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
	processes, ok := data.([]performance.ProcessStats)
	if !ok {
		return fmt.Errorf("invalid process stats data type")
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
	disks, ok := data.([]performance.DiskStats)
	if !ok {
		return fmt.Errorf("invalid disk stats data type")
	}

	for _, disk := range disks {
		diskAttrs := append(attrs, attribute.String("device", disk.Device))

		// I/O operations
		if counter, err := t.getOrCreateInt64Counter("system.disk.operations", "Disk I/O operations", "1"); err == nil {
			readAttrs := append(diskAttrs, attribute.String("direction", "read"))
			writeAttrs := append(diskAttrs, attribute.String("direction", "write"))
			counter.Add(ctx, int64(disk.ReadsCompleted), metric.WithAttributes(readAttrs...))
			counter.Add(ctx, int64(disk.WritesCompleted), metric.WithAttributes(writeAttrs...))
		}

		// I/O bytes
		if counter, err := t.getOrCreateInt64Counter("system.disk.io", "Disk I/O bytes", "By"); err == nil {
			readAttrs := append(diskAttrs, attribute.String("direction", "read"))
			writeAttrs := append(diskAttrs, attribute.String("direction", "write"))
			counter.Add(ctx, int64(disk.SectorsRead*DefaultSectorSize), metric.WithAttributes(readAttrs...))
			counter.Add(ctx, int64(disk.SectorsWritten*DefaultSectorSize), metric.WithAttributes(writeAttrs...))
		}
	}

	return nil
}

// transformNetworkStats transforms network interface statistics
func (t *Transformer) transformNetworkStats(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	interfaces, ok := data.([]performance.NetworkStats)
	if !ok {
		return fmt.Errorf("invalid network stats data type")
	}

	for _, iface := range interfaces {
		ifaceAttrs := append(attrs, attribute.String("device", iface.Interface))

		// Network I/O bytes
		if counter, err := t.getOrCreateInt64Counter("system.network.io", "Network I/O bytes", "By"); err == nil {
			rxAttrs := append(ifaceAttrs, attribute.String("direction", "receive"))
			txAttrs := append(ifaceAttrs, attribute.String("direction", "transmit"))
			counter.Add(ctx, int64(iface.RxBytes), metric.WithAttributes(rxAttrs...))
			counter.Add(ctx, int64(iface.TxBytes), metric.WithAttributes(txAttrs...))
		}

		// Network packets
		if counter, err := t.getOrCreateInt64Counter("system.network.packets", "Network packets", "1"); err == nil {
			rxAttrs := append(ifaceAttrs, attribute.String("direction", "receive"))
			txAttrs := append(ifaceAttrs, attribute.String("direction", "transmit"))
			counter.Add(ctx, int64(iface.RxPackets), metric.WithAttributes(rxAttrs...))
			counter.Add(ctx, int64(iface.TxPackets), metric.WithAttributes(txAttrs...))
		}

		// Network errors
		if counter, err := t.getOrCreateInt64Counter("system.network.errors", "Network errors", "1"); err == nil {
			rxAttrs := append(ifaceAttrs, attribute.String("direction", "receive"))
			txAttrs := append(ifaceAttrs, attribute.String("direction", "transmit"))
			counter.Add(ctx, int64(iface.RxErrors), metric.WithAttributes(rxAttrs...))
			counter.Add(ctx, int64(iface.TxErrors), metric.WithAttributes(txAttrs...))
		}
	}

	return nil
}

// transformTCPStats transforms TCP connection statistics
func (t *Transformer) transformTCPStats(ctx context.Context, data any, attrs []attribute.KeyValue) error {
	stats, ok := data.(*performance.TCPStats)
	if !ok {
		return fmt.Errorf("invalid TCP stats data type")
	}

	// TCP connection counts by state
	if gauge, err := t.getOrCreateInt64Gauge("system.network.connections", "Network connections by protocol and state", "1"); err == nil {
		tcpAttrs := append(attrs, attribute.String("protocol", "tcp"))
		for state, count := range stats.ConnectionsByState {
			stateAttrs := append(tcpAttrs, attribute.String("state", state))
			gauge.Record(ctx, int64(count), metric.WithAttributes(stateAttrs...))
		}
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
		return fmt.Errorf("invalid kernel messages data type")
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
