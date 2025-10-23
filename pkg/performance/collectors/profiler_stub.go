// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !linux

package collectors

import (
	"errors"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// ProfilerCollector is not available on non-Linux platforms
type ProfilerCollector struct {
	performance.BaseContinuousCollector
}

// NewProfiler creates a stub profiler on non-Linux platforms
func NewProfiler(logger logr.Logger, config performance.CollectionConfig) (*ProfilerCollector, error) {
	return nil, errors.New("profiler collector is only supported on Linux")
}

// Setup is not available on non-Linux platforms
func (c *ProfilerCollector) Setup(config ProfilerConfig) error {
	return errors.New("profiler collector is only supported on Linux")
}
