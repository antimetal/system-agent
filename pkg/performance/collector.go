// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/performance/capabilities"
	"github.com/go-logr/logr"
)

// Collector is the base interface for all collectors
type Collector interface {
	Type() MetricType
	Name() string
	Capabilities() CollectorCapabilities
}

// PointCollector performs one-shot data collection
type PointCollector interface {
	Collector

	// Collect performs a single collection and returns the metrics
	Collect(ctx context.Context) (any, error)
}

// ContinuousCollector performs ongoing data collection with streaming output
type ContinuousCollector interface {
	Collector

	// Start begins continuous collection and returns a channel for streaming results
	Start(ctx context.Context) (<-chan any, error)

	// Stop halts continuous collection and cleans up resources
	Stop() error

	Status() CollectorStatus
	LastError() error
}

// NewContinuousCollector creates a new continuous collector instance with the provided config
type NewContinuousCollector func(logr.Logger, CollectionConfig) (ContinuousCollector, error)

// NewPointCollector creates a new point collector instance with the provided config
type NewPointCollector func(logr.Logger, CollectionConfig) (PointCollector, error)

type CollectorCapabilities struct {
	SupportsOneShot      bool
	SupportsContinuous   bool
	RequiredCapabilities []capabilities.Capability
	MinKernelVersion     string
}

// CanRun checks if the collector can run with current process capabilities
// Returns nil if all capabilities are present, otherwise returns an error
// with all missing capabilities joined together
func (c CollectorCapabilities) CanRun() error {
	if len(c.RequiredCapabilities) == 0 {
		return nil
	}

	hasAll, missing, err := capabilities.HasAllCapabilities(c.RequiredCapabilities)
	if err != nil {
		return err
	}

	if !hasAll && len(missing) > 0 {
		errs := make([]error, len(missing))
		for i, cap := range missing {
			errs[i] = capabilities.MissingCapabilityError{Capability: cap}
		}
		// Always use errors.Join even for single error to simplify unwrapping
		return errors.Join(errs...)
	}

	return nil
}

// BaseCollector provides common functionality for all collectors
type BaseCollector struct {
	metricType   MetricType
	name         string
	logger       logr.Logger
	config       CollectionConfig
	capabilities CollectorCapabilities
}

func NewBaseCollector(metricType MetricType, name string, logger logr.Logger, config CollectionConfig, capabilities CollectorCapabilities) BaseCollector {
	return BaseCollector{
		metricType:   metricType,
		name:         name,
		logger:       logger.WithName(string(metricType)),
		config:       config,
		capabilities: capabilities,
	}
}

func (b *BaseCollector) Type() MetricType {
	return b.metricType
}

func (b *BaseCollector) Name() string {
	return b.name
}

func (b *BaseCollector) Capabilities() CollectorCapabilities {
	return b.capabilities
}

func (b *BaseCollector) Logger() logr.Logger {
	return b.logger
}

type BaseContinuousCollector struct {
	BaseCollector
	status    CollectorStatus
	lastError error
}

func NewBaseContinuousCollector(metricType MetricType, name string, logger logr.Logger, config CollectionConfig, capabilities CollectorCapabilities) BaseContinuousCollector {
	return BaseContinuousCollector{
		BaseCollector: NewBaseCollector(metricType, name, logger, config, capabilities),
		status:        CollectorStatusDisabled,
	}
}

func (b *BaseContinuousCollector) Status() CollectorStatus {
	return b.status
}

func (b *BaseContinuousCollector) LastError() error {
	return b.lastError
}

func (b *BaseContinuousCollector) SetStatus(status CollectorStatus) {
	b.status = status
}

func (b *BaseContinuousCollector) SetError(err error) {
	b.lastError = err
	if err != nil {
		b.status = CollectorStatusFailed
		b.BaseCollector.logger.Error(err, "collector error")
	}
}

func (b *BaseContinuousCollector) ClearError() {
	b.lastError = nil
}

// ContinuousPointCollector wraps a PointCollector into a ContinuousCollector
// that calls Collect() on an interval
//
// Note: This is NOT goroutine-safe
type ContinuousPointCollector struct {
	BaseContinuousCollector
	pointCollector PointCollector
	ch             chan any
	stopped        chan struct{}
}

// NewContinuousPointCollector creates a new ContinuousPointCollector
func NewContinuousPointCollector(
	pointCollector PointCollector, config CollectionConfig, logger logr.Logger,
) *ContinuousPointCollector {
	pointCaps := pointCollector.Capabilities()
	caps := CollectorCapabilities{
		SupportsOneShot:      false,
		SupportsContinuous:   true,
		RequiredCapabilities: pointCaps.RequiredCapabilities,
		MinKernelVersion:     pointCaps.MinKernelVersion,
	}
	return &ContinuousPointCollector{
		BaseContinuousCollector: NewBaseContinuousCollector(
			pointCollector.Type(),
			pointCollector.Name(),
			logger,
			config,
			caps,
		),
		pointCollector: pointCollector,
		stopped:        make(chan struct{}),
	}
}

// PartialNewContinuousPointCollector wraps collector into a NewContinuousCollector.
// This is a convenience function for creating registry-compatible collectors
// from existing PointCollector instances.
func PartialNewContinuousPointCollector(collector NewPointCollector) NewContinuousCollector {
	return func(logger logr.Logger, config CollectionConfig) (ContinuousCollector, error) {
		c, err := collector(logger, config)
		if err != nil {
			return nil, err
		}
		return NewContinuousPointCollector(c, config, logger), nil
	}
}

// Start begins the continuous point collection
func (c *ContinuousPointCollector) Start(ctx context.Context) (<-chan any, error) {
	if c.Status() != CollectorStatusDisabled {
		return nil, fmt.Errorf("collector already running, possibly in another goroutine")
	}

	c.ch = make(chan any, 10000)
	go c.start(ctx)
	c.SetStatus(CollectorStatusActive)
	return c.ch, nil
}

func (c *ContinuousPointCollector) start(ctx context.Context) {
	ticker := time.NewTicker(c.config.Interval)
	for {
		select {
		case <-ticker.C:
			data, err := c.pointCollector.Collect(ctx)
			c.SetError(err)
			if err != nil {
				c.SetStatus(CollectorStatusDegraded)
				continue
			}
			c.ch <- data
		case <-ctx.Done():
			if err := c.Stop(); err != nil {
				c.SetError(err)
			}
		case <-c.stopped:
			ticker.Stop()
			return
		}
	}
}

// Stop halts the continuous point collection
func (c *ContinuousPointCollector) Stop() error {
	if c.Status() == CollectorStatusDisabled {
		return nil
	}

	if c.stopped != nil {
		close(c.stopped)
		c.stopped = nil
	}
	if c.ch != nil {
		close(c.ch)
		c.ch = nil
	}
	c.SetStatus(CollectorStatusDisabled)
	return nil
}

// OnceContinuousCollector is a ContinuousCollector that wraps a PointCollector and
// performs a one-shot collection.
//
// Start() will call Collect() once and return the result in a channel with a buffer size of 1
// and then close it. This is useful for collectors that only need to run once because the
// information collected doesn't change (e.g. hardware info).
//
// Note: This is NOT goroutine-safe
type OnceContinuousCollector struct {
	BaseContinuousCollector
	pointCollector PointCollector
	result         any
	once           sync.Once
}

// NewOnceContinuousCollector creates a new OnceContinuousCollector
func NewOnceContinuousCollector(
	pointCollector PointCollector, config CollectionConfig, logger logr.Logger,
) *OnceContinuousCollector {
	pointCaps := pointCollector.Capabilities()
	return &OnceContinuousCollector{
		pointCollector: pointCollector,
		BaseContinuousCollector: NewBaseContinuousCollector(
			pointCollector.Type(),
			pointCollector.Name(),
			logger,
			config,
			CollectorCapabilities{
				SupportsOneShot:      true,
				SupportsContinuous:   false,
				RequiredCapabilities: pointCaps.RequiredCapabilities,
				MinKernelVersion:     pointCaps.MinKernelVersion,
			},
		),
	}
}

// PartialNewOnceContinuousCollector wraps collector into a NewContinuousCollector.
// This is a convenience function for creating registry-compatible collectors from point collectors.
func PartialNewOnceContinuousCollector(collector NewPointCollector) NewContinuousCollector {
	return func(logger logr.Logger, config CollectionConfig) (ContinuousCollector, error) {
		c, err := collector(logger, config)
		if err != nil {
			return nil, err
		}
		return NewOnceContinuousCollector(c, config, logger), nil
	}
}

// Start performs an exactly once collection and returns the result in a channel
// The channel will be closed after the data is sent. This means the returned channel
// will always be closed with at most 1 item in the channel.
//
// Calling Start() subsequently after the first call will return a closed channel
// containing the previous result and the last recorded error status
//
// WARNING: Call Start() multiple times will return separate channel instances.
func (c *OnceContinuousCollector) Start(ctx context.Context) (<-chan any, error) {
	if c.Status() != CollectorStatusDisabled {
		return nil, fmt.Errorf("collector already running, possibly in another goroutine")
	}
	c.SetStatus(CollectorStatusActive)

	var data any
	var err error
	c.once.Do(func() {
		data, err = c.pointCollector.Collect(ctx)
		c.result = data
		c.SetError(err)
		if err != nil {
			c.SetStatus(CollectorStatusFailed)
			return
		}
	})
	ch := make(chan any, 1)
	if c.result != nil {
		ch <- c.result
	}
	close(ch)
	return ch, c.LastError()
}

// Stop sets the colllector status back to disabled but DOES NOT clear the result
func (c *OnceContinuousCollector) Stop() error {
	c.SetStatus(CollectorStatusDisabled)
	return nil
}

// MetricsStore provides thread-safe storage for collected metrics
type MetricsStore struct {
	snapshot *Snapshot
	// We'll add mutex later when needed for concurrent access
}

func NewMetricsStore() *MetricsStore {
	return &MetricsStore{
		snapshot: &Snapshot{
			Metrics: Metrics{},
		},
	}
}

func (m *MetricsStore) UpdateLoad(stats *LoadStats) {
	m.snapshot.Metrics.Load = stats
}

func (m *MetricsStore) UpdateMemory(stats *MemoryStats) {
	m.snapshot.Metrics.Memory = stats
}

func (m *MetricsStore) UpdateCPU(stats []CPUStats) {
	m.snapshot.Metrics.CPU = stats
}

func (m *MetricsStore) UpdateProcesses(stats []ProcessStats) {
	m.snapshot.Metrics.Processes = stats
}

func (m *MetricsStore) UpdateDisks(stats []DiskStats) {
	m.snapshot.Metrics.Disks = stats
}

func (m *MetricsStore) UpdateNetwork(stats []NetworkStats) {
	m.snapshot.Metrics.Network = stats
}

func (m *MetricsStore) UpdateTCP(stats *TCPStats) {
	m.snapshot.Metrics.TCP = stats
}

func (m *MetricsStore) UpdateKernel(messages []KernelMessage) {
	m.snapshot.Metrics.Kernel = messages
}

func (m *MetricsStore) UpdateCPUInfo(info *CPUInfo) {
	m.snapshot.Metrics.CPUInfo = info
}

func (m *MetricsStore) UpdateMemoryInfo(info *MemoryInfo) {
	m.snapshot.Metrics.MemoryInfo = info
}

func (m *MetricsStore) UpdateDiskInfo(info []DiskInfo) {
	m.snapshot.Metrics.DiskInfo = info
}

func (m *MetricsStore) UpdateNetworkInfo(info []NetworkInfo) {
	m.snapshot.Metrics.NetworkInfo = info
}

func (m *MetricsStore) GetSnapshot() *Snapshot {
	// In the future, we'll deep copy here for thread safety
	return m.snapshot
}

func (m *MetricsStore) UpdateSnapshot(snapshot *Snapshot) {
	m.snapshot = snapshot
}
