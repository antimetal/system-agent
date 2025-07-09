package performance

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/antimetal/agent/pkg/resource"
	"github.com/go-logr/logr"
)

// Manager coordinates all performance monitoring activities
type Manager struct {
	config        CollectionConfig
	logger        logr.Logger
	registry      *CollectorRegistry
	store         *MetricsStore
	resourceStore resource.Store
	nodeName      string
	clusterName   string

	// Runtime state
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.RWMutex
	running     bool
	lastCollect time.Time

	// Channels for coordination
	collectTicker *time.Ticker
	stopCh        chan struct{}
}

type ManagerOptions struct {
	Config        CollectionConfig
	Logger        logr.Logger
	ResourceStore resource.Store
	NodeName      string
	ClusterName   string
}

func NewManager(opts ManagerOptions) (*Manager, error) {
	if opts.Logger.GetSink() == nil {
		return nil, fmt.Errorf("logger is required")
	}

	if opts.ResourceStore == nil {
		return nil, fmt.Errorf("resource store is required")
	}

	// Get node name from environment if not provided
	nodeName := opts.NodeName
	if nodeName == "" {
		nodeName = os.Getenv("NODE_NAME")
		if nodeName == "" {
			hostname, err := os.Hostname()
			if err != nil {
				return nil, fmt.Errorf("failed to get hostname: %w", err)
			}
			nodeName = hostname
		}
	}

	// Apply default config if needed
	config := opts.Config
	if config.Interval == 0 {
		config = DefaultCollectionConfig()
	}

	// Adjust paths for containerized environments
	if os.Getenv("HOST_PROC") != "" {
		config.HostProcPath = os.Getenv("HOST_PROC")
	}
	if os.Getenv("HOST_SYS") != "" {
		config.HostSysPath = os.Getenv("HOST_SYS")
	}
	if os.Getenv("HOST_DEV") != "" {
		config.HostDevPath = os.Getenv("HOST_DEV")
	}

	m := &Manager{
		config:        config,
		logger:        opts.Logger.WithName("performance-manager"),
		registry:      NewCollectorRegistry(opts.Logger),
		store:         NewMetricsStore(),
		resourceStore: opts.ResourceStore,
		nodeName:      nodeName,
		clusterName:   opts.ClusterName,
		stopCh:        make(chan struct{}),
	}

	return m, nil
}

func (m *Manager) RegisterCollector(collector Collector) error {
	if m.running {
		return fmt.Errorf("cannot register collector while manager is running")
	}

	return m.registry.Register(collector)
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("manager is already running")
	}

	m.logger.Info("starting performance monitoring",
		"interval", m.config.Interval,
		"node", m.nodeName,
		"enabledCollectors", m.config.EnabledCollectors,
	)

	// Create cancellable context
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Start continuous collectors (eBPF programs)
	if err := m.registry.StartAll(m.ctx, m.config); err != nil {
		m.logger.Error(err, "failed to start some collectors")
		// Continue anyway - some collectors may have started successfully
	}

	// Start collection ticker
	m.collectTicker = time.NewTicker(m.config.Interval)

	// Start collection loop
	m.wg.Add(1)
	go m.collectionLoop()

	m.running = true
	m.logger.Info("performance monitoring started")

	return nil
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	m.logger.Info("stopping performance monitoring")

	// Signal stop
	close(m.stopCh)

	// Stop ticker
	if m.collectTicker != nil {
		m.collectTicker.Stop()
	}

	// Cancel context
	if m.cancel != nil {
		m.cancel()
	}

	// Wait for collection loop to finish
	m.wg.Wait()

	// Stop all collectors
	m.registry.StopAll()

	m.running = false
	m.logger.Info("performance monitoring stopped")

	return nil
}

func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

func (m *Manager) GetSnapshot() *Snapshot {
	return m.store.GetSnapshot()
}

func (m *Manager) collectionLoop() {
	defer m.wg.Done()

	// Perform initial collection immediately
	m.performCollection()

	for {
		select {
		case <-m.collectTicker.C:
			m.performCollection()

		case <-m.stopCh:
			return

		case <-m.ctx.Done():
			return
		}
	}
}

func (m *Manager) performCollection() {
	start := time.Now()

	m.logger.V(1).Info("starting collection cycle")

	// Create collection context with timeout
	ctx, cancel := context.WithTimeout(m.ctx, m.config.Interval/2)
	defer cancel()

	// Collect from all enabled collectors
	stats := m.registry.CollectAll(ctx, m.config)

	// Update collection metadata
	m.mu.Lock()
	m.lastCollect = time.Now()
	m.mu.Unlock()

	// Create snapshot
	snapshot := &Snapshot{
		Timestamp:   start,
		NodeName:    m.nodeName,
		ClusterName: m.clusterName,
		CollectorRun: CollectorRunInfo{
			Duration:       time.Since(start),
			CollectorStats: stats,
		},
		Metrics: *m.store.GetSnapshot().Metrics.DeepCopy(),
	}

	m.store.UpdateSnapshot(snapshot)

	// Send to resource store if available
	if m.resourceStore != nil {
		if err := m.sendToResourceStore(snapshot); err != nil {
			m.logger.Error(err, "failed to send metrics to resource store")
		}
	}

	m.logger.V(1).Info("collection cycle completed",
		"duration", time.Since(start),
		"collectors", len(stats),
	)
}

func (m *Manager) sendToResourceStore(snapshot *Snapshot) error {
	// This will be implemented when we integrate with the resource store
	// For now, just log that we would send it
	m.logger.V(2).Info("would send metrics to resource store",
		"timestamp", snapshot.Timestamp,
		"node", snapshot.NodeName,
	)
	return nil
}

func (m *Manager) GetCollectorStatus() map[MetricType]CollectorStatus {
	status := make(map[MetricType]CollectorStatus)

	for _, collector := range m.registry.GetAll() {
		status[collector.Type()] = collector.Status()
	}

	return status
}

func (m *Manager) GetLastCollectTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastCollect
}

func (m *Metrics) DeepCopy() *Metrics {
	if m == nil {
		return nil
	}

	result := &Metrics{}

	// Deep copy each field
	if m.Load != nil {
		loadCopy := *m.Load
		result.Load = &loadCopy
	}

	if m.Memory != nil {
		memoryCopy := *m.Memory
		result.Memory = &memoryCopy
	}

	if m.CPU != nil {
		result.CPU = make([]CPUStats, len(m.CPU))
		copy(result.CPU, m.CPU)
	}

	if m.Processes != nil {
		result.Processes = make([]ProcessStats, len(m.Processes))
		copy(result.Processes, m.Processes)
	}

	if m.Disks != nil {
		result.Disks = make([]DiskStats, len(m.Disks))
		copy(result.Disks, m.Disks)
	}

	if m.Network != nil {
		result.Network = make([]NetworkStats, len(m.Network))
		copy(result.Network, m.Network)
	}

	if m.TCP != nil {
		tcpCopy := *m.TCP
		if m.TCP.ConnectionsByState != nil {
			tcpCopy.ConnectionsByState = make(map[string]uint64)
			for k, v := range m.TCP.ConnectionsByState {
				tcpCopy.ConnectionsByState[k] = v
			}
		}
		result.TCP = &tcpCopy
	}

	if m.Kernel != nil {
		result.Kernel = make([]KernelMessage, len(m.Kernel))
		copy(result.Kernel, m.Kernel)
	}

	return result
}

// ManagerRunnable implements the controller-runtime Runnable interface
// This allows the manager to be added to the controller-runtime manager
type ManagerRunnable struct {
	*Manager
}

func (m *ManagerRunnable) Start(ctx context.Context) error {
	if err := m.Manager.Start(ctx); err != nil {
		return err
	}

	// Wait for context cancellation
	<-ctx.Done()

	return m.Manager.Stop()
}
