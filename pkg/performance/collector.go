package performance

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
)

type Collector interface {
	Type() MetricType

	Name() string

	// Collect performs a single collection and returns the metrics
	Collect(ctx context.Context) error

	// Start begins continuous collection (for collectors that need it, like eBPF)
	Start(ctx context.Context) error

	// Stop halts continuous collection and cleans up resources
	Stop() error

	Status() CollectorStatus

	LastError() error

	Capabilities() CollectorCapabilities
}

type CollectorCapabilities struct {
	SupportsOneShot    bool
	SupportsContinuous bool
	RequiresRoot       bool
	RequiresEBPF       bool
	MinKernelVersion   string
}

type BaseCollector struct {
	metricType   MetricType
	name         string
	status       CollectorStatus
	lastError    error
	logger       logr.Logger
	config       CollectionConfig
	capabilities CollectorCapabilities
	lastCollect  time.Time
}

func NewBaseCollector(metricType MetricType, name string, logger logr.Logger, config CollectionConfig, capabilities CollectorCapabilities) BaseCollector {
	return BaseCollector{
		metricType:   metricType,
		name:         name,
		status:       CollectorStatusDisabled,
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

func (b *BaseCollector) Status() CollectorStatus {
	return b.status
}

func (b *BaseCollector) LastError() error {
	return b.lastError
}

func (b *BaseCollector) Capabilities() CollectorCapabilities {
	return b.capabilities
}

func (b *BaseCollector) SetStatus(status CollectorStatus) {
	b.status = status
}

func (b *BaseCollector) SetError(err error) {
	b.lastError = err
	if err != nil {
		b.status = CollectorStatusFailed
		b.logger.Error(err, "collector error")
	}
}

func (b *BaseCollector) ClearError() {
	b.lastError = nil
}

func (b *BaseCollector) RecordCollectTime() {
	b.lastCollect = time.Now()
}

type CollectorRegistry struct {
	collectors map[MetricType]Collector
	logger     logr.Logger
}

func NewCollectorRegistry(logger logr.Logger) *CollectorRegistry {
	return &CollectorRegistry{
		collectors: make(map[MetricType]Collector),
		logger:     logger.WithName("registry"),
	}
}

func (r *CollectorRegistry) Register(collector Collector) error {
	if collector == nil {
		return fmt.Errorf("cannot register nil collector")
	}

	metricType := collector.Type()
	if _, exists := r.collectors[metricType]; exists {
		return fmt.Errorf("collector for metric type %s already registered", metricType)
	}

	r.collectors[metricType] = collector
	r.logger.Info("registered collector", "type", metricType, "name", collector.Name())
	return nil
}

func (r *CollectorRegistry) Get(metricType MetricType) (Collector, bool) {
	collector, ok := r.collectors[metricType]
	return collector, ok
}

func (r *CollectorRegistry) GetAll() []Collector {
	collectors := make([]Collector, 0, len(r.collectors))
	for _, collector := range r.collectors {
		collectors = append(collectors, collector)
	}
	return collectors
}

func (r *CollectorRegistry) GetEnabled(config CollectionConfig) []Collector {
	var enabled []Collector
	for metricType, collector := range r.collectors {
		if config.EnabledCollectors[metricType] {
			enabled = append(enabled, collector)
		}
	}
	return enabled
}

// StartAll starts all continuous collectors
func (r *CollectorRegistry) StartAll(ctx context.Context, config CollectionConfig) error {
	for metricType, collector := range r.collectors {
		if !config.EnabledCollectors[metricType] {
			continue
		}

		if !collector.Capabilities().SupportsContinuous {
			continue
		}

		if err := collector.Start(ctx); err != nil {
			r.logger.Error(err, "failed to start collector", "type", metricType)
			// Continue starting other collectors even if one fails
		}
	}
	return nil
}

func (r *CollectorRegistry) StopAll() {
	for metricType, collector := range r.collectors {
		if err := collector.Stop(); err != nil {
			r.logger.Error(err, "failed to stop collector", "type", metricType)
		}
	}
}

// CollectAll performs a one-shot collection from all enabled collectors
func (r *CollectorRegistry) CollectAll(ctx context.Context, config CollectionConfig) map[MetricType]CollectorStat {
	stats := make(map[MetricType]CollectorStat)

	for metricType, collector := range r.collectors {
		if !config.EnabledCollectors[metricType] {
			continue
		}

		start := time.Now()
		err := collector.Collect(ctx)
		duration := time.Since(start)

		stats[metricType] = CollectorStat{
			Status:   collector.Status(),
			Duration: duration,
			Error:    err,
		}
	}

	return stats
}

// MetricsStore provides thread-safe storage for collected metrics
type MetricsStore struct {
	snapshot *Snapshot
	// We'll add mutex later when needed for concurrent access
}

// NewMetricsStore creates a new metrics store
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

// GetSnapshot returns the current metrics snapshot
func (m *MetricsStore) GetSnapshot() *Snapshot {
	// In the future, we'll deep copy here for thread safety
	return m.snapshot
}

// UpdateSnapshot updates the entire snapshot
func (m *MetricsStore) UpdateSnapshot(snapshot *Snapshot) {
	m.snapshot = snapshot
}
