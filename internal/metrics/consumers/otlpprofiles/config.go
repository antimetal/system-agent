// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otlpprofiles

import (
	"errors"
	"time"
)

// Config holds configuration for the OTLP profiling consumer
type Config struct {
	// AuthToken is the Bearer token for authentication (optional)
	// Populated from --intake-api-key flag (shared with intake worker)
	// Sent via gRPC metadata key "authorization"
	// Empty token works when intake service has auth disabled (dev mode)
	AuthToken string

	// ExportTimeout is the gRPC request timeout
	ExportTimeout time.Duration

	// ExportInterval is how often to export buffered profiles
	ExportInterval time.Duration

	// MaxQueueSize is the maximum number of profiles to buffer
	MaxQueueSize int

	// ExportBatchSize is the number of profiles to export in one batch
	ExportBatchSize int

	// ServiceName for OTLP resource attributes
	ServiceName string

	// ServiceVersion for OTLP resource attributes
	ServiceVersion string
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() Config {
	return Config{
		AuthToken:       "", // Empty = no auth (dev mode)
		ExportTimeout:   30 * time.Second,
		ExportInterval:  10 * time.Second,
		MaxQueueSize:    1000,
		ExportBatchSize: 100,
		ServiceName:     "antimetal-agent",
		ServiceVersion:  "dev",
	}
}

// Validate checks if the configuration is valid
func (c Config) Validate() error {
	if c.ExportTimeout <= 0 {
		return errors.New("export timeout must be positive")
	}
	if c.ExportInterval <= 0 {
		return errors.New("export interval must be positive")
	}
	if c.MaxQueueSize <= 0 {
		return errors.New("max queue size must be positive")
	}
	if c.ExportBatchSize <= 0 {
		return errors.New("export batch size must be positive")
	}
	if c.ExportBatchSize > c.MaxQueueSize {
		return errors.New("export batch size cannot exceed max queue size")
	}
	return nil
}
