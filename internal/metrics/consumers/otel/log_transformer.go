// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otel

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/log"

	"github.com/antimetal/agent/internal/metrics"
	"github.com/antimetal/agent/pkg/performance"
)

// LogTransformer converts kernel messages to OpenTelemetry log records
type LogTransformer struct {
	logger         logr.Logger
	loggerProvider log.LoggerProvider
	otelLogger     log.Logger
	serviceVersion string
}

// NewLogTransformer creates a new OpenTelemetry log transformer
func NewLogTransformer(provider log.LoggerProvider, logger logr.Logger, serviceVersion string) *LogTransformer {
	otelLogger := provider.Logger(
		"github.com/antimetal/agent/kernel",
		log.WithInstrumentationVersion("1.0.0"),
	)

	return &LogTransformer{
		logger:         logger.WithName("otel-log-transformer"),
		loggerProvider: provider,
		otelLogger:     otelLogger,
		serviceVersion: serviceVersion,
	}
}

// TransformAndEmit converts kernel messages and process snapshots to OpenTelemetry log records and emits them
func (t *LogTransformer) TransformAndEmit(ctx context.Context, event metrics.MetricEvent) error {
	switch event.MetricType {
	case metrics.MetricTypeKernel:
		return t.handleKernelMessages(ctx, event)
	case metrics.MetricTypeProcess:
		return t.handleProcessSnapshot(ctx, event)
	default:
		return nil
	}
}

// handleKernelMessages handles kernel message log events
func (t *LogTransformer) handleKernelMessages(ctx context.Context, event metrics.MetricEvent) error {
	switch data := event.Data.(type) {
	case []*performance.KernelMessage:
		// Handle array of messages (from point collection)
		for _, msg := range data {
			if err := t.emitKernelMessage(ctx, msg, event); err != nil {
				t.logger.V(2).Info("Failed to emit kernel message", "error", err)
				// Continue processing other messages even if one fails
			}
		}
	case *performance.KernelMessage:
		// Handle single message (from continuous collection)
		if err := t.emitKernelMessage(ctx, data, event); err != nil {
			return fmt.Errorf("failed to emit kernel message: %w", err)
		}
	default:
		return fmt.Errorf("unexpected kernel data type: %T", data)
	}

	return nil
}

// handleProcessSnapshot handles process snapshot log events
func (t *LogTransformer) handleProcessSnapshot(ctx context.Context, event metrics.MetricEvent) error {
	processes, ok := event.Data.([]*performance.ProcessStats)
	if !ok {
		return fmt.Errorf("unexpected process data type: %T, expected []*performance.ProcessStats", event.Data)
	}

	// Emit top-N process snapshots as structured logs
	return t.emitProcessSnapshots(ctx, processes, event)
}

// emitKernelMessage emits a single kernel message as an OpenTelemetry log record
func (t *LogTransformer) emitKernelMessage(ctx context.Context, msg *performance.KernelMessage, event metrics.MetricEvent) error {
	// Map kernel severity to OpenTelemetry severity
	severity := mapKernelSeverityToOTEL(msg.Severity)

	// Create a log record with all kernel message fields as attributes
	record := log.Record{}
	record.SetTimestamp(msg.Timestamp)
	record.SetSeverity(severity)
	record.SetSeverityText(getKernelSeverityText(msg.Severity))
	record.SetBody(log.StringValue(msg.Message))

	// Add attributes from the kernel message
	var attrs []log.KeyValue

	// Standard attributes from MetricEvent
	if event.NodeName != "" {
		attrs = append(attrs, log.String("host.name", event.NodeName))
	}
	if event.ClusterName != "" {
		attrs = append(attrs, log.String("k8s.cluster.name", event.ClusterName))
	}
	if event.Source != "" {
		attrs = append(attrs, log.String("service.instance.id", event.Source))
	}
	if t.serviceVersion != "" {
		attrs = append(attrs, log.String("service.version", t.serviceVersion))
	}

	// Kernel-specific attributes
	attrs = append(attrs,
		log.Int("kernel.facility", int(msg.Facility)),
		log.Int("kernel.severity", int(msg.Severity)),
		log.Int64("kernel.sequence", int64(msg.SequenceNum)),
	)

	if msg.Subsystem != "" {
		attrs = append(attrs, log.String("kernel.subsystem", msg.Subsystem))
	}
	if msg.Device != "" {
		attrs = append(attrs, log.String("kernel.device", msg.Device))
	}

	record.AddAttributes(attrs...)

	// Emit the log record
	t.otelLogger.Emit(ctx, record)

	return nil
}

// mapKernelSeverityToOTEL maps kernel severity levels (0-7) to OpenTelemetry severity levels
func mapKernelSeverityToOTEL(severity uint8) log.Severity {
	// Kernel severity levels (from include/linux/kern_levels.h):
	// 0 = KERN_EMERG   - system is unusable
	// 1 = KERN_ALERT   - action must be taken immediately
	// 2 = KERN_CRIT    - critical conditions
	// 3 = KERN_ERR     - error conditions
	// 4 = KERN_WARNING - warning conditions
	// 5 = KERN_NOTICE  - normal but significant condition
	// 6 = KERN_INFO    - informational
	// 7 = KERN_DEBUG   - debug-level messages

	switch severity {
	case 0: // KERN_EMERG
		return log.SeverityFatal4
	case 1: // KERN_ALERT
		return log.SeverityFatal3
	case 2: // KERN_CRIT
		return log.SeverityFatal2
	case 3: // KERN_ERR
		return log.SeverityError
	case 4: // KERN_WARNING
		return log.SeverityWarn
	case 5: // KERN_NOTICE
		return log.SeverityInfo2
	case 6: // KERN_INFO
		return log.SeverityInfo
	case 7: // KERN_DEBUG
		return log.SeverityDebug
	default:
		return log.SeverityInfo
	}
}

// getKernelSeverityText returns the text representation of kernel severity
func getKernelSeverityText(severity uint8) string {
	switch severity {
	case 0:
		return "EMERG"
	case 1:
		return "ALERT"
	case 2:
		return "CRIT"
	case 3:
		return "ERROR"
	case 4:
		return "WARNING"
	case 5:
		return "NOTICE"
	case 6:
		return "INFO"
	case 7:
		return "DEBUG"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", severity)
	}
}

// emitProcessSnapshots emits process snapshots as structured OTEL logs
func (t *LogTransformer) emitProcessSnapshots(ctx context.Context, processes []*performance.ProcessStats, event metrics.MetricEvent) error {
	if len(processes) == 0 {
		return nil
	}

	// Processes are already sorted by CPU% and limited by the collector's TopProcesses configuration
	// Just emit each process as a structured log entry
	for rank, proc := range processes {
		if err := t.emitProcessLog(ctx, proc, rank+1, len(processes), event); err != nil {
			t.logger.V(2).Info("Failed to emit process log", "error", err, "pid", proc.PID)
			// Continue processing other processes even if one fails
		}
	}

	return nil
}

// emitProcessLog emits a single process as an OpenTelemetry log record
func (t *LogTransformer) emitProcessLog(ctx context.Context, proc *performance.ProcessStats, rank int, totalProcesses int, event metrics.MetricEvent) error {
	// Create log record
	record := log.Record{}
	record.SetTimestamp(event.Timestamp)
	record.SetSeverity(log.SeverityInfo)
	record.SetBody(log.StringValue(fmt.Sprintf("Process snapshot: rank %d by CPU", rank)))

	// Build attributes following OTEL semantic conventions
	var attrs []log.KeyValue

	// Standard resource attributes
	if event.NodeName != "" {
		attrs = append(attrs, log.String("host.name", event.NodeName))
	}
	if event.ClusterName != "" {
		attrs = append(attrs, log.String("k8s.cluster.name", event.ClusterName))
	}
	if event.Source != "" {
		attrs = append(attrs, log.String("service.instance.id", event.Source))
	}
	if t.serviceVersion != "" {
		attrs = append(attrs, log.String("service.version", t.serviceVersion))
	}

	// Snapshot metadata
	attrs = append(attrs,
		log.String("log.type", "process.snapshot"),
		log.Int("snapshot.rank", rank),
		log.String("snapshot.sort_by", "cpu_percent"),
		log.Int("snapshot.total_processes", totalProcesses),
	)

	// Process identification (OTEL semantic conventions)
	attrs = append(attrs,
		log.Int64("process.pid", int64(proc.PID)),
		log.String("process.executable.name", proc.Command),
	)

	if proc.PPID > 0 {
		attrs = append(attrs, log.Int64("process.parent_pid", int64(proc.PPID)))
	}

	if proc.Cmdline != "" {
		attrs = append(attrs, log.String("process.command_line", proc.Cmdline))
	}

	if proc.State != "" {
		attrs = append(attrs, log.String("process.runtime.state", proc.State))
	}

	// CPU metrics
	attrs = append(attrs,
		log.Float64("process.cpu.utilization", proc.CPUPercent/100.0), // Convert to ratio (0-1)
		log.Int64("process.cpu.time", int64(proc.CPUTime)),            // Total CPU time in ticks
	)

	// Memory metrics (all in bytes)
	attrs = append(attrs,
		log.Int64("process.memory.virtual", int64(proc.MemoryVSZ)),
		log.Int64("process.memory.rss", int64(proc.MemoryRSS)),
	)

	if proc.MemoryPSS > 0 {
		attrs = append(attrs, log.Int64("process.memory.pss", int64(proc.MemoryPSS)))
	}
	if proc.MemoryUSS > 0 {
		attrs = append(attrs, log.Int64("process.memory.uss", int64(proc.MemoryUSS)))
	}

	// Thread and file descriptor counts
	if proc.Threads > 0 {
		attrs = append(attrs, log.Int64("process.thread.count", int64(proc.Threads)))
	}
	if proc.NumFds > 0 {
		attrs = append(attrs, log.Int64("process.open_file_descriptors", int64(proc.NumFds)))
	}

	// Context switches
	if proc.VoluntaryCtxt > 0 {
		attrs = append(attrs, log.Int64("process.context_switches.voluntary", int64(proc.VoluntaryCtxt)))
	}
	if proc.InvoluntaryCtxt > 0 {
		attrs = append(attrs, log.Int64("process.context_switches.involuntary", int64(proc.InvoluntaryCtxt)))
	}

	// Scheduling info
	if proc.Nice != 0 {
		attrs = append(attrs, log.Int64("process.nice", int64(proc.Nice)))
	}
	if proc.Priority != 0 {
		attrs = append(attrs, log.Int64("process.priority", int64(proc.Priority)))
	}

	// Add start time if available
	if !proc.StartTime.IsZero() {
		attrs = append(attrs, log.Int64("process.start_time", proc.StartTime.Unix()))
	}

	record.AddAttributes(attrs...)

	// Emit the log record
	t.otelLogger.Emit(ctx, record)

	return nil
}
