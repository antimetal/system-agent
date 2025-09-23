// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otel

import (
	"flag"
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

// Command-line flag variables (populated by init())
var (
	flagEnabled *bool
)

func init() {
	// Define OpenTelemetry enable flag that will be parsed in main()
	// All other configuration comes from standard OTEL environment variables
	flagEnabled = flag.Bool("enable-otel", false, "Enable OpenTelemetry metrics consumer (configure via OTEL_* environment variables)")
}

// String returns the string representation of the compression type
func (c CompressionType) String() string {
	return string(c)
}

// IsValid checks if the compression type is valid
func (c CompressionType) IsValid() bool {
	return c == CompressionGZip || c == CompressionNone
}

type Config struct {
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
	BatchTimeout    time.Duration // Max time between batches
	ExportBatchSize int           // Number of metrics to accumulate before export
	MaxQueueSize    int           // Maximum queued metrics in ring buffer
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
		BatchTimeout:    10 * time.Second,
		ExportBatchSize: 500,
		MaxQueueSize:    1000,
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

	// OTEL_EXPORTER_OTLP_METRICS_TIMEOUT or OTEL_EXPORTER_OTLP_TIMEOUT
	if timeout := getEnvVar("OTEL_EXPORTER_OTLP_METRICS_TIMEOUT", "OTEL_EXPORTER_OTLP_TIMEOUT"); timeout != "" {
		if duration, err := time.ParseDuration(timeout); err == nil {
			c.Timeout = duration
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

	// OTEL_EXPORTER_METRICS_MAX_QUEUE_SIZE - custom env var for queue size
	if queueSize := os.Getenv("OTEL_EXPORTER_METRICS_MAX_QUEUE_SIZE"); queueSize != "" {
		if size, err := strconv.Atoi(queueSize); err == nil && size > 0 {
			c.MaxQueueSize = size
		}
	}

	// OTEL_EXPORTER_METRICS_EXPORT_BATCH_SIZE - custom env var for batch size
	if batchSize := os.Getenv("OTEL_EXPORTER_METRICS_EXPORT_BATCH_SIZE"); batchSize != "" {
		if size, err := strconv.Atoi(batchSize); err == nil && size > 0 {
			c.ExportBatchSize = size
		}
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
	if c.Endpoint == "" {
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

	if c.ExportBatchSize <= 0 {
		c.ExportBatchSize = 500
	} else if c.ExportBatchSize > MaxSafeBatchSize {
		return ErrBatchSizeTooLarge
	}

	if c.MaxQueueSize <= 0 {
		c.MaxQueueSize = 1000
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

// GetConfigFromEnvironment builds a Config from environment variables
func GetConfigFromEnvironment() Config {
	config := DefaultConfig()
	config.ApplyEnvironmentVariables()
	return config
}

// IsEnabled returns whether OpenTelemetry is enabled via flags
func IsEnabled() bool {
	return flagEnabled != nil && *flagEnabled
}

// Common errors
var (
	ErrEndpointRequired       = fmt.Errorf("OTLP endpoint is required when OpenTelemetry is enabled")
	ErrInvalidCompressionType = fmt.Errorf("compression type must be '%s' or '%s'", CompressionGZip, CompressionNone)
	ErrBatchSizeTooLarge      = fmt.Errorf("batch size cannot exceed %d to prevent OOM", MaxSafeBatchSize)
	ErrQueueSizeTooLarge      = fmt.Errorf("queue size cannot exceed %d to prevent OOM", MaxSafeQueueSize)
)
