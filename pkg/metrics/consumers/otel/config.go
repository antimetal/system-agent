// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otel

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// CompressionType represents the compression type for OTLP exports
type CompressionType string

const (
	CompressionGZip CompressionType = "gzip" // GZIP compression
	CompressionNone CompressionType = "none" // No compression

	// Safety limits to prevent OOM
	MaxSafeBatchSize = 10000  // Maximum safe batch size
	MaxSafeQueueSize = 100000 // Maximum safe queue size
)

// String returns the string representation of the compression type
func (c CompressionType) String() string {
	return string(c)
}

// IsValid checks if the compression type is valid
func (c CompressionType) IsValid() bool {
	return c == CompressionGZip || c == CompressionNone
}

type Config struct {
	Enabled bool

	// OTLP gRPC configuration
	Endpoint string // OTLP gRPC endpoint (default: localhost:4317)
	Insecure bool   // Disable TLS (default: false)

	// Headers for gRPC metadata
	Headers map[string]string

	// Compression type for OTLP exports
	Compression CompressionType

	// Timeout for export operations
	Timeout time.Duration

	// Retry configuration
	RetryConfig RetryConfig

	// Resource attributes
	ServiceName    string // Service name (default: antimetal-agent)
	ServiceVersion string // Service version

	// Global tags to add to all metrics
	GlobalTags []string

	// Advanced options
	BatchTimeout time.Duration // Max time between batches
	MaxBatchSize int           // Maximum metrics per batch
	MaxQueueSize int           // Maximum queued metrics
}

// RetryConfig configures retry behavior for failed exports
type RetryConfig struct {
	Enabled        bool          // Enable retry logic
	MaxRetries     int           // Maximum number of retries
	InitialBackoff time.Duration // Initial backoff duration
	MaxBackoff     time.Duration // Maximum backoff duration
	BackoffFactor  float64       // Backoff multiplier
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() Config {
	return Config{
		Enabled:     false, // Disabled by default
		Endpoint:    "localhost:4317",
		Insecure:    false,
		Headers:     make(map[string]string),
		Compression: CompressionGZip,
		Timeout:     30 * time.Second,
		RetryConfig: RetryConfig{
			Enabled:        true,
			MaxRetries:     3,
			InitialBackoff: 1 * time.Second,
			MaxBackoff:     30 * time.Second,
			BackoffFactor:  2.0,
		},
		ServiceName:    "antimetal-agent",
		ServiceVersion: "",
		GlobalTags: []string{
			"service:antimetal-agent",
		},
		BatchTimeout: 10 * time.Second,
		MaxBatchSize: 500,
		MaxQueueSize: 10000,
	}
}

// ApplyEnvironmentVariables applies standard OTLP environment variables to the configuration.
// It follows the OpenTelemetry specification for environment variable names and precedence.
func (c *Config) ApplyEnvironmentVariables() {
	// OTEL_EXPORTER_OTLP_METRICS_ENDPOINT or OTEL_EXPORTER_OTLP_ENDPOINT
	if endpoint := getEnvVar("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		c.Endpoint = endpoint
	}

	// OTEL_EXPORTER_OTLP_METRICS_INSECURE or OTEL_EXPORTER_OTLP_INSECURE
	if insecure := getEnvVar("OTEL_EXPORTER_OTLP_METRICS_INSECURE", "OTEL_EXPORTER_OTLP_INSECURE"); insecure != "" {
		if parsed, err := strconv.ParseBool(insecure); err == nil {
			c.Insecure = parsed
		}
	}

	// OTEL_EXPORTER_OTLP_METRICS_HEADERS or OTEL_EXPORTER_OTLP_HEADERS
	if headers := getEnvVar("OTEL_EXPORTER_OTLP_METRICS_HEADERS", "OTEL_EXPORTER_OTLP_HEADERS"); headers != "" {
		c.Headers = parseHeaders(headers)
	}

	// OTEL_EXPORTER_OTLP_METRICS_COMPRESSION or OTEL_EXPORTER_OTLP_COMPRESSION
	if compression := getEnvVar("OTEL_EXPORTER_OTLP_METRICS_COMPRESSION", "OTEL_EXPORTER_OTLP_COMPRESSION"); compression != "" {
		compressionType := CompressionType(compression)
		if compressionType.IsValid() {
			c.Compression = compressionType
		}
	}

	// OTEL_SERVICE_NAME
	if serviceName := os.Getenv("OTEL_SERVICE_NAME"); serviceName != "" {
		c.ServiceName = serviceName
	}

	// OTEL_SERVICE_VERSION
	if serviceVersion := os.Getenv("OTEL_SERVICE_VERSION"); serviceVersion != "" {
		c.ServiceVersion = serviceVersion
	}
}

// getEnvVar returns the first non-empty environment variable from the list
func getEnvVar(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

// parseHeaders parses comma-separated key=value pairs into a map
func parseHeaders(headers string) map[string]string {
	result := make(map[string]string)
	pairs := strings.Split(headers, ",")

	for _, pair := range pairs {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key != "" {
				result[key] = value
			}
		}
	}

	return result
}

// Validate ensures the configuration is valid and sets reasonable defaults.
// It validates required fields, enforces safety limits, and normalizes values.
func (c *Config) Validate() error {
	if c.Enabled && c.Endpoint == "" {
		return ErrEndpointRequired
	}

	// Validate compression type
	if c.Compression != "" && !c.Compression.IsValid() {
		return ErrInvalidCompressionType
	}

	// Set default compression if empty
	if c.Compression == "" {
		c.Compression = CompressionGZip
	}

	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}

	if c.BatchTimeout <= 0 {
		c.BatchTimeout = 10 * time.Second
	}

	if c.MaxBatchSize <= 0 {
		c.MaxBatchSize = 500
	} else if c.MaxBatchSize > MaxSafeBatchSize {
		return ErrBatchSizeTooLarge
	}

	if c.MaxQueueSize <= 0 {
		c.MaxQueueSize = 10000
	} else if c.MaxQueueSize > MaxSafeQueueSize {
		return ErrQueueSizeTooLarge
	}

	if c.ServiceName == "" {
		c.ServiceName = "antimetal-agent"
	}

	if c.RetryConfig.MaxRetries < 0 {
		c.RetryConfig.MaxRetries = 0
	}

	if c.RetryConfig.InitialBackoff <= 0 {
		c.RetryConfig.InitialBackoff = 1 * time.Second
	}

	if c.RetryConfig.MaxBackoff <= 0 {
		c.RetryConfig.MaxBackoff = 30 * time.Second
	}

	if c.RetryConfig.BackoffFactor <= 1.0 {
		c.RetryConfig.BackoffFactor = 2.0
	}

	return nil
}

// Common errors
var (
	ErrEndpointRequired       = fmt.Errorf("OTLP endpoint is required when OpenTelemetry is enabled")
	ErrInvalidCompressionType = fmt.Errorf("compression type must be '%s' or '%s'", CompressionGZip, CompressionNone)
	ErrBatchSizeTooLarge      = fmt.Errorf("batch size cannot exceed %d to prevent OOM", MaxSafeBatchSize)
	ErrQueueSizeTooLarge      = fmt.Errorf("queue size cannot exceed %d to prevent OOM", MaxSafeQueueSize)
)
