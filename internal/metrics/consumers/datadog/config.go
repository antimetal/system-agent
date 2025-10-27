// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package datadog

import (
	"errors"
	"time"
)

// Config holds configuration for the Datadog profiling consumer
type Config struct {
	// AgentURL is the Datadog Agent profiling intake endpoint
	// Default: http://localhost:8126/profiling/v1/input
	AgentURL string

	// Service name for tagging profiles
	Service string

	// Environment name (e.g., "production", "staging")
	Env string

	// Version of the service
	Version string

	// UploadInterval is how often to upload profiles
	UploadInterval time.Duration

	// MaxQueueSize is the maximum number of profiles to buffer
	MaxQueueSize int

	// Hostname to tag profiles with (defaults to system hostname)
	Hostname string

	// APIKey for direct intake (optional, not needed when using agent)
	APIKey string

	// Tags are additional tags to attach to profiles
	Tags map[string]string
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() Config {
	return Config{
		AgentURL:       "http://localhost:8126/profiling/v1/input",
		Service:        "antimetal-agent",
		Env:            "production",
		Version:        "dev",
		UploadInterval: 60 * time.Second, // Datadog default
		MaxQueueSize:   100,
		Tags:           make(map[string]string),
	}
}

// Validate checks if the configuration is valid
func (c Config) Validate() error {
	if c.AgentURL == "" {
		return errors.New("agent URL cannot be empty")
	}
	if c.Service == "" {
		return errors.New("service name cannot be empty")
	}
	if c.Env == "" {
		return errors.New("environment cannot be empty")
	}
	if c.UploadInterval <= 0 {
		return errors.New("upload interval must be positive")
	}
	if c.MaxQueueSize <= 0 {
		return errors.New("max queue size must be positive")
	}
	return nil
}
