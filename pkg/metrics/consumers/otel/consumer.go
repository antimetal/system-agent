// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otel

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	metricSDK "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/antimetal/agent/pkg/metrics"
)

const (
	consumerName = "opentelemetry"
)

type Consumer struct {
	config Config
	logger logr.Logger

	// OpenTelemetry components
	exporter    metricSDK.Exporter
	provider    *metricSDK.MeterProvider
	meter       metric.Meter
	transformer *Transformer

	// Runtime state
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	healthy   atomic.Bool
	lastError atomic.Pointer[error]

	// Metrics
	eventsProcessed atomic.Uint64
	errorsCount     atomic.Uint64
	startTime       time.Time
}

// NewConsumer creates a new OpenTelemetry metrics consumer
func NewConsumer(config Config, logger logr.Logger) (*Consumer, error) {
	if !config.Enabled {
		return nil, nil // Return nil if disabled
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	consumer := &Consumer{
		config:    config,
		logger:    logger.WithName("otel-consumer"),
		ctx:       ctx,
		cancel:    cancel,
		startTime: time.Now(),
	}

	// Initialize OpenTelemetry components
	if err := consumer.initOpenTelemetry(); err != nil {
		cancel()
		return nil, err
	}

	consumer.healthy.Store(true)
	return consumer, nil
}

// initOpenTelemetry initializes the OpenTelemetry components
func (c *Consumer) initOpenTelemetry() error {
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
	exporter, err := otlpmetricgrpc.New(c.ctx, opts...)
	if err != nil {
		return err
	}
	c.exporter = exporter

	// Create resource with service information
	res := resource.NewWithAttributes(
		"",
		semconv.ServiceName(c.config.ServiceName),
		semconv.ServiceVersion(c.config.ServiceVersion),
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

	// Create transformer
	c.transformer = NewTransformer(c.meter, c.logger)

	return nil
}

// Name returns the consumer name identifier.
func (c *Consumer) Name() string {
	return consumerName
}

// Start begins processing metrics events from the provided channel.
// It launches a background goroutine to handle events and returns immediately.
func (c *Consumer) Start(events <-chan metrics.MetricEvent) error {
	c.logger.Info("Starting OpenTelemetry consumer",
		"endpoint", c.config.Endpoint,
		"service_name", c.config.ServiceName,
		"compression", c.config.Compression)

	c.wg.Add(1)
	go c.processEvents(events)

	return nil
}

// Stop gracefully shuts down the OpenTelemetry consumer.
// It cancels the context, waits for event processing to complete, and shuts down the meter provider.
func (c *Consumer) Stop() error {
	c.logger.Info("Stopping OpenTelemetry consumer...")
	c.cancel()
	c.wg.Wait()

	// Shutdown the meter provider
	if c.provider != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := c.provider.Shutdown(shutdownCtx); err != nil {
			c.logger.Error(err, "Error shutting down meter provider")
			return err
		}
	}

	c.logger.Info("OpenTelemetry consumer stopped",
		"events_processed", c.eventsProcessed.Load(),
		"errors", c.errorsCount.Load(),
		"uptime", time.Since(c.startTime))

	return nil
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
		ErrorsCount: c.errorsCount.Load(),
	}
}

// processEvents is the main event processing loop
func (c *Consumer) processEvents(events <-chan metrics.MetricEvent) {
	defer c.wg.Done()

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

	for {
		select {
		case event, ok := <-events:
			if !ok {
				c.logger.Info("Events channel closed, stopping consumer")
				return
			}

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

		case <-c.ctx.Done():
			c.logger.Info("Context cancelled, stopping consumer")
			return
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

	if err := c.transformer.TransformAndRecord(c.ctx, event); err != nil {
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

// NewConsumerFromConfig creates a consumer from configuration.
// Returns nil if OpenTelemetry is disabled in config, allowing for conditional consumer creation.
// This is the recommended way to create consumers in production environments.
func NewConsumerFromConfig(config Config, logger logr.Logger) (*Consumer, error) {
	if !config.Enabled {
		logger.Info("OpenTelemetry consumer is disabled")
		return nil, nil
	}

	consumer, err := NewConsumer(config, logger)
	if err != nil {
		return nil, err
	}

	return consumer, nil
}

// Compile-time check that Consumer implements ConsumerInterface
var _ metrics.ConsumerInterface = (*Consumer)(nil)
