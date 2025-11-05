// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package perfdata

import (
	"errors"
	"time"
)

// Config holds configuration for the perf.data file writer consumer
type Config struct {
	// OutputPath is the base directory for perf.data files
	OutputPath string
	// FileMode controls whether to write seekable files or streaming pipes
	// "file" = seekable file with full header
	// "pipe" = streaming pipe format
	FileMode string
	// RotationInterval is how often to rotate to a new file
	RotationInterval time.Duration
	// MaxFileSize is the maximum size before rotating (0 = no size limit)
	MaxFileSize int64
	// MaxFiles is the maximum number of perf.data files to keep (0 = unlimited)
	MaxFiles int
	// BufferSize is the size of the write buffer
	BufferSize int
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() Config {
	return Config{
		OutputPath:       "/var/lib/antimetal/profiles",
		FileMode:         "file",
		RotationInterval: 1 * time.Hour,
		MaxFileSize:      100 * 1024 * 1024, // 100 MB
		MaxFiles:         24,                // Keep 24 hours worth
		BufferSize:       64 * 1024,         // 64 KB buffer
	}
}

// Validate checks if the configuration is valid
func (c Config) Validate() error {
	if c.OutputPath == "" {
		return errors.New("output path cannot be empty")
	}
	if c.FileMode != "file" && c.FileMode != "pipe" {
		return errors.New("file mode must be 'file' or 'pipe'")
	}
	if c.RotationInterval <= 0 {
		return errors.New("rotation interval must be positive")
	}
	if c.MaxFileSize < 0 {
		return errors.New("max file size cannot be negative")
	}
	if c.MaxFiles < 0 {
		return errors.New("max files cannot be negative")
	}
	if c.BufferSize <= 0 {
		return errors.New("buffer size must be positive")
	}
	return nil
}
