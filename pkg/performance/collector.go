package performance

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
)

// PointCollector performs one-shot data collection
type PointCollector interface {
	Type() MetricType
	Name() string

	// Collect performs a single collection and returns the metrics
	Collect(ctx context.Context) (interface{}, error)

	Capabilities() CollectorCapabilities
}

// ContinuousCollector performs ongoing data collection with streaming output
type ContinuousCollector interface {
	Type() MetricType
	Name() string

	// Start begins continuous collection and returns a channel for streaming results
	Start(ctx context.Context) (<-chan interface{}, error)

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

type BasePointCollector struct {
	metricType   MetricType
	name         string
	logger       logr.Logger
	config       CollectionConfig
	capabilities CollectorCapabilities
}

func NewBasePointCollector(metricType MetricType, name string, logger logr.Logger, config CollectionConfig, capabilities CollectorCapabilities) BasePointCollector {
	return BasePointCollector{
		metricType:   metricType,
		name:         name,
		logger:       logger.WithName(string(metricType)),
		config:       config,
		capabilities: capabilities,
	}
}

func (b *BasePointCollector) Type() MetricType {
	return b.metricType
}

func (b *BasePointCollector) Name() string {
	return b.name
}

func (b *BasePointCollector) Capabilities() CollectorCapabilities {
	return b.capabilities
}

type BaseContinuousCollector struct {
	metricType   MetricType
	name         string
	status       CollectorStatus
	lastError    error
	logger       logr.Logger
	config       CollectionConfig
	capabilities CollectorCapabilities
}

func NewBaseContinuousCollector(metricType MetricType, name string, logger logr.Logger, config CollectionConfig, capabilities CollectorCapabilities) BaseContinuousCollector {
	return BaseContinuousCollector{
		metricType:   metricType,
		name:         name,
		status:       CollectorStatusDisabled,
		logger:       logger.WithName(string(metricType)),
		config:       config,
		capabilities: capabilities,
	}
}

func (b *BaseContinuousCollector) Type() MetricType {
	return b.metricType
}

func (b *BaseContinuousCollector) Name() string {
	return b.name
}

func (b *BaseContinuousCollector) Status() CollectorStatus {
	return b.status
}

func (b *BaseContinuousCollector) LastError() error {
	return b.lastError
}

func (b *BaseContinuousCollector) Capabilities() CollectorCapabilities {
	return b.capabilities
}

func (b *BaseContinuousCollector) SetStatus(status CollectorStatus) {
	b.status = status
}

func (b *BaseContinuousCollector) SetError(err error) {
	b.lastError = err
	if err != nil {
		b.status = CollectorStatusFailed
		b.logger.Error(err, "collector error")
	}
}

func (b *BaseContinuousCollector) ClearError() {
	b.lastError = nil
}

type CollectorRegistry struct {
	pointCollectors      map[MetricType]PointCollector
	continuousCollectors map[MetricType]ContinuousCollector
	logger               logr.Logger
}

func NewCollectorRegistry(logger logr.Logger) *CollectorRegistry {
	return &CollectorRegistry{
		pointCollectors:      make(map[MetricType]PointCollector),
		continuousCollectors: make(map[MetricType]ContinuousCollector),
		logger:               logger.WithName("registry"),
	}
}

func (r *CollectorRegistry) RegisterPoint(collector PointCollector) error {
	if collector == nil {
		return fmt.Errorf("cannot register nil collector")
	}

	metricType := collector.Type()
	if _, exists := r.pointCollectors[metricType]; exists {
		return fmt.Errorf("point collector for metric type %s already registered", metricType)
	}
	if _, exists := r.continuousCollectors[metricType]; exists {
		return fmt.Errorf("continuous collector for metric type %s already registered", metricType)
	}

	r.pointCollectors[metricType] = collector
	r.logger.Info("registered point collector", "type", metricType, "name", collector.Name())
	return nil
}

func (r *CollectorRegistry) RegisterContinuous(collector ContinuousCollector) error {
	if collector == nil {
		return fmt.Errorf("cannot register nil collector")
	}

	metricType := collector.Type()
	if _, exists := r.continuousCollectors[metricType]; exists {
		return fmt.Errorf("continuous collector for metric type %s already registered", metricType)
	}
	if _, exists := r.pointCollectors[metricType]; exists {
		return fmt.Errorf("point collector for metric type %s already registered", metricType)
	}

	r.continuousCollectors[metricType] = collector
	r.logger.Info("registered continuous collector", "type", metricType, "name", collector.Name())
	return nil
}

func (r *CollectorRegistry) GetPoint(metricType MetricType) (PointCollector, bool) {
	collector, ok := r.pointCollectors[metricType]
	return collector, ok
}

func (r *CollectorRegistry) GetContinuous(metricType MetricType) (ContinuousCollector, bool) {
	collector, ok := r.continuousCollectors[metricType]
	return collector, ok
}

func (r *CollectorRegistry) GetAllPoint() []PointCollector {
	collectors := make([]PointCollector, 0, len(r.pointCollectors))
	for _, collector := range r.pointCollectors {
		collectors = append(collectors, collector)
	}
	return collectors
}

func (r *CollectorRegistry) GetAllContinuous() []ContinuousCollector {
	collectors := make([]ContinuousCollector, 0, len(r.continuousCollectors))
	for _, collector := range r.continuousCollectors {
		collectors = append(collectors, collector)
	}
	return collectors
}

func (r *CollectorRegistry) GetEnabledPoint(config CollectionConfig) []PointCollector {
	var enabled []PointCollector
	for metricType, collector := range r.pointCollectors {
		if config.EnabledCollectors[metricType] {
			enabled = append(enabled, collector)
		}
	}
	return enabled
}

func (r *CollectorRegistry) GetEnabledContinuous(config CollectionConfig) []ContinuousCollector {
	var enabled []ContinuousCollector
	for metricType, collector := range r.continuousCollectors {
		if config.EnabledCollectors[metricType] {
			enabled = append(enabled, collector)
		}
	}
	return enabled
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

func (m *MetricsStore) GetSnapshot() *Snapshot {
	// In the future, we'll deep copy here for thread safety
	return m.snapshot
}

func (m *MetricsStore) UpdateSnapshot(snapshot *Snapshot) {
	m.snapshot = snapshot
}
