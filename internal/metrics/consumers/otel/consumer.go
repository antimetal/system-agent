// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otel

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	metricSDK "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/antimetal/agent/internal/metrics"
)

// Compile-time check
var _ metrics.Consumer = (*Consumer)(nil)

const (
	consumerName = "opentelemetry"
)

var (
	// ErrBufferFull is returned when the internal buffer is full
	ErrBufferFull = errors.New("internal buffer is full")
)

type Consumer struct {
	config Config
	logger logr.Logger

	// OpenTelemetry components
	exporter    metricSDK.Exporter
	provider    *metricSDK.MeterProvider
	meter       metric.Meter
	transformer *Transformer

	// Internal buffering
	buffer *MetricsBuffer

	// Runtime state
	wg        sync.WaitGroup
	healthy   atomic.Bool
	lastError atomic.Pointer[error]

	// Metrics
	eventsProcessed atomic.Uint64
	eventsDropped   atomic.Uint64
	errorsCount     atomic.Uint64
	startTime       time.Time
}

// NewConsumer creates a new OpenTelemetry metrics consumer
func NewConsumer(config Config, logger logr.Logger) (*Consumer, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Create ring buffer with notification threshold set to export batch size
	// This ensures we get notified when there's enough data for export
	buffer, err := NewMetricsBuffer(config.MaxQueueSize, config.ExportBatchSize)
	if err != nil {
		return nil, err
	}

	consumer := &Consumer{
		config:    config,
		logger:    logger.WithName("otel-consumer"),
		startTime: time.Now(),
		buffer:    buffer,
	}

	// Note: OpenTelemetry components will be initialized in Start() when we have a context
	consumer.healthy.Store(true)
	return consumer, nil
}

// initOpenTelemetry initializes the OpenTelemetry components
func (c *Consumer) initOpenTelemetry(ctx context.Context) error {
	// Create OTLP gRPC exporter
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(c.config.Endpoint),
		otlpmetricgrpc.WithTimeout(c.config.Timeout),
	}

	// Configure TLS
	if c.config.Insecure {
		opts = append(opts, otlpmetricgrpc.WithTLSCredentials(insecure.NewCredentials()))
	}

	// Add headers
	if len(c.config.Headers) > 0 {
		opts = append(opts, otlpmetricgrpc.WithHeaders(c.config.Headers))
	}

	// Configure compression
	if c.config.Compression != "" {
		switch c.config.Compression {
		case CompressionGZip:
			opts = append(opts, otlpmetricgrpc.WithCompressor(c.config.Compression.String()))
		case CompressionNone:
			// No compression
		default:
			c.logger.V(1).Info("Unknown compression type, using default", "compression", c.config.Compression)
		}
	}

	// Add retry configuration
	if c.config.RetryConfig.Enabled {
		// Calculate MaxElapsedTime safely to prevent overflow
		maxElapsed := c.config.RetryConfig.MaxBackoff
		if c.config.RetryConfig.MaxRetries > 0 {
			// Use safe multiplication with bounds checking
			if c.config.RetryConfig.MaxRetries <= 100 { // reasonable upper limit
				maxElapsed = time.Duration(c.config.RetryConfig.MaxRetries) * c.config.RetryConfig.MaxBackoff
			} else {
				// For very large retry counts, use a sensible maximum (30 minutes)
				maxElapsed = 30 * time.Minute
			}
		}

		retryConfig := otlpmetricgrpc.RetryConfig{
			Enabled:         true,
			InitialInterval: c.config.RetryConfig.InitialBackoff,
			MaxInterval:     c.config.RetryConfig.MaxBackoff,
			MaxElapsedTime:  maxElapsed,
		}
		opts = append(opts, otlpmetricgrpc.WithRetry(retryConfig))
	}

	// Create the exporter
	exporter, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return err
	}
	c.exporter = exporter

	// Create resource with service information
	res := resource.NewWithAttributes(
		"",
		semconv.ServiceName(c.config.ServiceName),
		semconv.ServiceVersion(c.config.ServiceVersion),
		attribute.String("antimetal", "true"),
	)

	// Create meter provider
	c.provider = metricSDK.NewMeterProvider(
		metricSDK.WithReader(metricSDK.NewPeriodicReader(
			exporter,
			metricSDK.WithInterval(c.config.BatchTimeout),
		)),
		metricSDK.WithResource(res),
	)

	// Set global meter provider
	otel.SetMeterProvider(c.provider)

	// Create meter
	c.meter = c.provider.Meter(
		"github.com/antimetal/agent",
		metric.WithInstrumentationVersion("1.0.0"),
	)

	// Create transformer with service version for attributes
	c.transformer = NewTransformer(c.meter, c.logger, c.config.ServiceVersion)

	return nil
}

// Name returns the consumer name identifier.
func (c *Consumer) Name() string {
	return consumerName
}

// HandleEvent processes a metric event by adding it to the internal buffer.
// This method is non-blocking. The ring buffer automatically overwrites the
// oldest event when full, implementing a natural drop-oldest policy.
func (c *Consumer) HandleEvent(event metrics.MetricEvent) error {
	// Push to ring buffer (never blocks, overwrites oldest if full)
	c.buffer.Push(event)
	return nil
}

// Start begins processing metrics events.
// It launches a background goroutine to process buffered events and returns immediately.
func (c *Consumer) Start(ctx context.Context) error {
	c.logger.Info("Starting OpenTelemetry consumer",
		"endpoint", c.config.Endpoint,
		"service_name", c.config.ServiceName,
		"compression", c.config.Compression)

	// Initialize OpenTelemetry components now that we have a context
	if err := c.initOpenTelemetry(ctx); err != nil {
		return err
	}

	c.wg.Add(1)
	go c.processEvents(ctx)

	return nil
}

// shutdown gracefully shuts down the meter provider.
// This is called when the context is cancelled.
func (c *Consumer) shutdown(ctx context.Context) {
	// Shutdown the meter provider with a timeout
	if c.provider != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 30*time.Second)
		defer shutdownCancel()

		if err := c.provider.Shutdown(shutdownCtx); err != nil {
			c.logger.Error(err, "Error shutting down meter provider")
		}
	}

	c.logger.Info("OpenTelemetry consumer stopped",
		"events_processed", c.eventsProcessed.Load(),
		"errors", c.errorsCount.Load(),
		"uptime", time.Since(c.startTime))
}

// Health returns the current health status of the consumer.
// It provides information about health state, last error, and processing metrics.
func (c *Consumer) Health() metrics.ConsumerHealth {
	var lastErr error
	if errPtr := c.lastError.Load(); errPtr != nil {
		lastErr = *errPtr
	}

	return metrics.ConsumerHealth{
		Healthy:     c.healthy.Load(),
		LastError:   lastErr,
		EventsCount: c.eventsProcessed.Load(),
		ErrorsCount: c.errorsCount.Load() + c.eventsDropped.Load(),
	}
}

// processEvents is the main event processing loop
func (c *Consumer) processEvents(ctx context.Context) {
	defer c.wg.Done()
	defer c.shutdown(ctx)

	// Setup error recovery
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error(nil, "OpenTelemetry consumer panic recovered", "panic", r)
			c.healthy.Store(false)
			if err, ok := r.(error); ok {
				c.lastError.Store(&err)
			}
		}
	}()

	c.logger.Info("OpenTelemetry consumer event processing started")

	// Batch timer for periodic flushes
	ticker := time.NewTicker(c.config.BatchTimeout)
	defer ticker.Stop()

	notify := c.buffer.NotifyChannel()

	for {
		select {
		case <-notify:
			// Drain all events when threshold is reached
			events := c.buffer.Drain()
			if len(events) > 0 {
				// Process in batches if we have too many events
				for i := 0; i < len(events); i += c.config.ExportBatchSize {
					end := i + c.config.ExportBatchSize
					if end > len(events) {
						end = len(events)
					}
					c.processBatch(events[i:end])
				}
			}

		case <-ticker.C:
			// Periodic flush - drain all available events
			events := c.buffer.Drain()
			if len(events) > 0 {
				c.processBatch(events)
			}

		case <-ctx.Done():
			// Process any remaining events
			events := c.buffer.Drain()
			if len(events) > 0 {
				c.processBatch(events)
			}
			c.logger.Info("Context cancelled, stopping consumer")
			return
		}
	}
}

// processBatch processes a batch of metrics events
func (c *Consumer) processBatch(batch []metrics.MetricEvent) {
	for _, event := range batch {
		if err := c.processEvent(event); err != nil {
			c.logger.Error(err, "Failed to process metrics event",
				"metric_type", event.MetricType,
				"source", event.Source)
			c.errorsCount.Add(1)
			c.lastError.Store(&err)

			// Don't mark as unhealthy for individual event failures
			// Only mark unhealthy if we have too many consecutive errors
			if c.errorsCount.Load()%ErrorThresholdForHealthCheck == 0 {
				c.logger.Error(nil, "High error rate detected in OpenTelemetry consumer",
					"errors", c.errorsCount.Load(),
					"events", c.eventsProcessed.Load())
			}
		} else {
			c.eventsProcessed.Add(1)
		}
	}
}

// processEvent processes a single metrics event
func (c *Consumer) processEvent(event metrics.MetricEvent) error {
	// Log detailed event information at debug level
	c.logger.V(2).Info("Processing metrics event",
		"metric_type", event.MetricType,
		"event_type", event.EventType,
		"source", event.Source,
		"node", event.NodeName,
		"cluster", event.ClusterName,
		"timestamp", event.Timestamp)

	if err := c.transformer.TransformAndRecord(event); err != nil {
		return err
	}

	// Log heartbeat periodically
	if c.eventsProcessed.Load()%HeartbeatInterval == 0 {
		c.logger.V(1).Info("OpenTelemetry consumer heartbeat",
			"events_processed", c.eventsProcessed.Load(),
			"errors", c.errorsCount.Load())
	}

	return nil
}
