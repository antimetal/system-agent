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

// TransformAndEmit converts kernel messages to OpenTelemetry log records and emits them
func (t *LogTransformer) TransformAndEmit(ctx context.Context, event metrics.MetricEvent) error {
	// Only handle kernel messages
	if event.MetricType != metrics.MetricTypeKernel {
		return nil
	}

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
