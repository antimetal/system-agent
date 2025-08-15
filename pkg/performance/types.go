// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"fmt"
	"path/filepath"
	"time"
)

// MetricType represents the type of performance metric
type MetricType string

const (
	// Runtime System Statistics
	MetricTypeLoad      MetricType = "load"
	MetricTypeMemory    MetricType = "memory"
	MetricTypeCPU       MetricType = "cpu"
	MetricTypeProcess   MetricType = "process"
	MetricTypeDisk      MetricType = "disk"
	MetricTypeNetwork   MetricType = "network"
	MetricTypeTCP       MetricType = "tcp"
	MetricTypeKernel    MetricType = "kernel"
	MetricTypeSystem    MetricType = "system"
	MetricTypeNUMAStats MetricType = "numa_stats"
	// Runtime Container Statistics
	MetricTypeCgroupCPU     MetricType = "cgroup_cpu"
	MetricTypeCgroupMemory  MetricType = "cgroup_memory"
	MetricTypeCgroupIO      MetricType = "cgroup_io"      // Future
	MetricTypeCgroupNetwork MetricType = "cgroup_network" // Future
	// Hardware configuration collectors
	MetricTypeCPUInfo     MetricType = "cpu_info"
	MetricTypeMemoryInfo  MetricType = "memory_info"
	MetricTypeDiskInfo    MetricType = "disk_info"
	MetricTypeNetworkInfo MetricType = "network_info"
	// Alerts
	MetricTypeProcessMemoryGrowth MetricType = "process_memory_growth"
)

// CollectorStatus represents the operational status of a collector
type CollectorStatus string

const (
	CollectorStatusActive   CollectorStatus = "active"
	CollectorStatusDegraded CollectorStatus = "degraded"
	CollectorStatusFailed   CollectorStatus = "failed"
	CollectorStatusDisabled CollectorStatus = "disabled"
)

// Snapshot represents a complete performance snapshot at a point in time
type Snapshot struct {
	Timestamp    time.Time
	NodeName     string
	ClusterName  string
	CollectorRun CollectorRunInfo
	Metrics      Metrics
}

// CollectorRunInfo contains metadata about a collector run
type CollectorRunInfo struct {
	Duration       time.Duration
	CollectorStats map[MetricType]CollectorStat
}

// CollectorStat tracks individual collector performance
type CollectorStat struct {
	Status   CollectorStatus
	Duration time.Duration
	Error    error
	Data     any // The actual collected data
}

// Metrics contains all collected performance metrics
type Metrics struct {
	Load      *LoadStats
	Memory    *MemoryStats
	CPU       []CPUStats
	Processes []ProcessStats
	Disks     []DiskStats
	Network   []NetworkStats
	TCP       *TCPStats
	System    *SystemStats
	Kernel    []KernelMessage
	// Hardware configuration
	CPUInfo     *CPUInfo
	MemoryInfo  *MemoryInfo
	DiskInfo    []DiskInfo
	NetworkInfo []NetworkInfo
	NUMAStats   *NUMAStatistics
}

// LoadStats represents system load information
type LoadStats struct {
	// Load averages from /proc/loadavg (1st, 2nd, 3rd fields)
	Load1Min  float64
	Load5Min  float64
	Load15Min float64
	// Running/total processes from /proc/loadavg (4th field, e.g., "2/1234")
	RunningProcs int32
	TotalProcs   int32
	// Blocked processes from /proc/stat (procs_blocked field)
	BlockedProcs int32
	// Last PID from /proc/loadavg (5th field)
	LastPID int32
	// System uptime from /proc/uptime (1st field in seconds)
	Uptime time.Duration
}

// MemoryStats represents runtime memory usage statistics from /proc/meminfo
// Used by MemoryCollector for operational monitoring and performance analysis
type MemoryStats struct {
	// Basic memory stats (all values in kB from /proc/meminfo)
	MemTotal     uint64 // MemTotal: Total usable RAM
	MemFree      uint64 // MemFree: Free memory
	MemAvailable uint64 // MemAvailable: Available memory for starting new applications
	Buffers      uint64 // Buffers: Memory in buffer cache
	Cached       uint64 // Cached: Memory in page cache (excluding SwapCached)
	SwapCached   uint64 // SwapCached: Memory that was swapped out and is now back in RAM
	// Active/Inactive memory
	Active   uint64 // Active: Memory that has been used recently
	Inactive uint64 // Inactive: Memory that hasn't been used recently
	// Swap stats
	SwapTotal uint64 // SwapTotal: Total swap space
	SwapFree  uint64 // SwapFree: Unused swap space
	// Swap activity (cumulative counters from /proc/vmstat)
	SwapIn  uint64 // pswpin: Pages swapped in since boot
	SwapOut uint64 // pswpout: Pages swapped out since boot
	// Dirty pages
	Dirty     uint64 // Dirty: Memory waiting to be written back to disk
	Writeback uint64 // Writeback: Memory actively being written back to disk
	// Anonymous memory
	AnonPages uint64 // AnonPages: Non-file backed pages mapped into userspace
	Mapped    uint64 // Mapped: Files which have been mapped into memory
	Shmem     uint64 // Shmem: Total shared memory
	// Slab allocator
	Slab         uint64 // Slab: Total slab allocator memory
	SReclaimable uint64 // SReclaimable: Reclaimable slab memory
	SUnreclaim   uint64 // SUnreclaim: Unreclaimable slab memory
	// Kernel memory
	KernelStack uint64 // KernelStack: Memory used by kernel stacks
	PageTables  uint64 // PageTables: Memory used by page tables
	// Memory commit
	CommitLimit uint64 // CommitLimit: Total amount of memory that can be allocated
	CommittedAS uint64 // Committed_AS: Total committed memory
	// Virtual memory
	VmallocTotal uint64 // VmallocTotal: Total size of vmalloc virtual address space
	VmallocUsed  uint64 // VmallocUsed: Used vmalloc area
	// HugePages
	HugePages_Total uint64 // HugePages_Total: Total number of hugepages
	HugePages_Free  uint64 // HugePages_Free: Number of free hugepages
	HugePages_Rsvd  uint64 // HugePages_Rsvd: Number of reserved hugepages
	HugePages_Surp  uint64 // HugePages_Surp: Number of surplus hugepages
	HugePagesize    uint64 // Hugepagesize: Default hugepage size (in kB)
	Hugetlb         uint64 // Hugetlb: Total memory consumed by huge pages of all sizes
}

// CPUStats represents per-CPU statistics from /proc/stat
type CPUStats struct {
	// CPU index (-1 for aggregate "cpu" line, 0+ for "cpu0", "cpu1", etc.)
	CPUIndex int32
	// Time spent in different CPU states (in USER_HZ units from /proc/stat)
	User      uint64 // Time in user mode
	Nice      uint64 // Time in user mode with low priority (nice)
	System    uint64 // Time in system mode
	Idle      uint64 // Time spent idle
	IOWait    uint64 // Time waiting for I/O completion
	IRQ       uint64 // Time servicing interrupts
	SoftIRQ   uint64 // Time servicing softirqs
	Steal     uint64 // Time stolen by other operating systems in virtualized environment
	Guest     uint64 // Time spent running a virtual CPU for guest OS
	GuestNice uint64 // Time spent running a niced guest
}

// ProcessStats represents per-process statistics
type ProcessStats struct {
	// Basic process info from /proc/[pid]/stat
	PID     int32  // Process ID (field 1 in stat)
	PPID    int32  // Parent process ID (field 4 in stat)
	PGID    int32  // Process group ID (field 5 in stat)
	SID     int32  // Session ID (field 6 in stat)
	Command string // Command name from /proc/[pid]/comm or stat field 2
	State   string // Process state (field 3 in stat: R, S, D, Z, T, etc.)
	// CPU stats from /proc/[pid]/stat
	CPUTime    uint64  // Total CPU time: utime + stime (fields 14+15)
	CPUPercent float64 // Calculated CPU usage percentage
	// Memory stats
	MemoryVSZ uint64 // Virtual memory size from /proc/[pid]/stat (field 23)
	MemoryRSS uint64 // Resident set size from /proc/[pid]/stat (field 24) * page_size
	MemoryPSS uint64 // Proportional set size from /proc/[pid]/smaps_rollup
	MemoryUSS uint64 // Unique set size from /proc/[pid]/smaps_rollup
	// Thread count from /proc/[pid]/stat
	Threads int32 // Number of threads (field 20)
	// Page faults from /proc/[pid]/stat
	MinorFaults uint64 // Minor faults (field 10)
	MajorFaults uint64 // Major faults (field 12)
	// Process timing
	StartTime time.Time // Process start time calculated from stat field 22 + boot time
	// Scheduling info from /proc/[pid]/stat
	Nice     int32 // Nice value (field 19)
	Priority int32 // Priority (field 18)
	// File descriptors from /proc/[pid]/fd/
	NumFds     int32 // Number of open file descriptors
	NumThreads int32 // Thread count from /proc/[pid]/status
	// Context switches from /proc/[pid]/status
	VoluntaryCtxt   uint64 // voluntary_ctxt_switches
	InvoluntaryCtxt uint64 // nonvoluntary_ctxt_switches
}

// DiskStats represents disk I/O statistics from /proc/diskstats
type DiskStats struct {
	// Device identification
	Device string // Device name (field 3 in /proc/diskstats)
	Major  uint32 // Major device number (field 1)
	Minor  uint32 // Minor device number (field 2)
	// Read statistics (fields 4-7 in /proc/diskstats)
	ReadsCompleted uint64 // Successfully completed reads
	ReadsMerged    uint64 // Reads merged before queuing
	SectorsRead    uint64 // Sectors read (multiply by 512 for bytes)
	ReadTime       uint64 // Time spent reading (milliseconds)
	// Write statistics (fields 8-11 in /proc/diskstats)
	WritesCompleted uint64 // Successfully completed writes
	WritesMerged    uint64 // Writes merged before queuing
	SectorsWritten  uint64 // Sectors written (multiply by 512 for bytes)
	WriteTime       uint64 // Time spent writing (milliseconds)
	// I/O queue statistics (fields 12-14 in /proc/diskstats)
	IOsInProgress  uint64 // I/Os currently in progress
	IOTime         uint64 // Time spent doing I/Os (milliseconds)
	WeightedIOTime uint64 // Weighted time spent doing I/Os (milliseconds)
	// Calculated fields
	IOPS             float64
	ReadBytesPerSec  float64
	WriteBytesPerSec float64
	Utilization      float64 // Percentage 0-100
	AvgQueueSize     float64
	AvgReadLatency   float64 // milliseconds
	AvgWriteLatency  float64 // milliseconds
}

// NetworkStats represents network interface statistics
type NetworkStats struct {
	// Interface name from /proc/net/dev
	Interface string
	// Receive statistics from /proc/net/dev (columns 2-9)
	RxBytes      uint64 // Bytes received
	RxPackets    uint64 // Packets received
	RxErrors     uint64 // Receive errors
	RxDropped    uint64 // Packets dropped on receive
	RxFIFO       uint64 // FIFO buffer errors
	RxFrame      uint64 // Frame alignment errors
	RxCompressed uint64 // Compressed packets received
	RxMulticast  uint64 // Multicast packets received
	// Transmit statistics from /proc/net/dev (columns 10-17)
	TxBytes      uint64 // Bytes transmitted
	TxPackets    uint64 // Packets transmitted
	TxErrors     uint64 // Transmit errors
	TxDropped    uint64 // Packets dropped on transmit
	TxFIFO       uint64 // FIFO buffer errors
	TxCollisions uint64 // Collisions detected
	TxCarrier    uint64 // Carrier losses
	TxCompressed uint64 // Compressed packets transmitted
	// Interface metadata from /sys/class/net/[interface]/
	Speed        uint64 // Link speed in Mbps from /sys/class/net/[interface]/speed
	Duplex       string // Duplex mode from /sys/class/net/[interface]/duplex
	OperState    string // Operational state from /sys/class/net/[interface]/operstate
	LinkDetected bool   // Link detection from /sys/class/net/[interface]/carrier
}

// TCPStats represents TCP connection statistics
type TCPStats struct {
	// Connection counts from /proc/net/snmp (Tcp: line)
	ActiveOpens  uint64 // Active connection openings (count since boot)
	PassiveOpens uint64 // Passive connection openings (count since boot)
	AttemptFails uint64 // Failed connection attempts (count since boot)
	EstabResets  uint64 // Resets from established state (count since boot)
	CurrEstab    uint64 // Current established connections (instantaneous count)
	InSegs       uint64 // Segments received (count since boot)
	OutSegs      uint64 // Segments sent (count since boot)
	RetransSegs  uint64 // Segments retransmitted (count since boot)
	InErrs       uint64 // Segments received with errors (count since boot)
	OutRsts      uint64 // RST segments sent (count since boot)
	InCsumErrors uint64 // Segments with checksum errors (count since boot)
	// Extended TCP stats from /proc/net/netstat (TcpExt: line)
	SyncookiesSent      uint64 // SYN cookies sent (count since boot)
	SyncookiesRecv      uint64 // SYN cookies received (count since boot)
	SyncookiesFailed    uint64 // SYN cookies failed (count since boot)
	ListenOverflows     uint64 // Listen queue overflows (count since boot)
	ListenDrops         uint64 // Listen queue drops (count since boot)
	TCPLostRetransmit   uint64 // Lost retransmissions (count since boot)
	TCPFastRetrans      uint64 // Fast retransmissions (count since boot)
	TCPSlowStartRetrans uint64 // Slow start retransmissions (count since boot)
	TCPTimeouts         uint64 // TCP timeouts (count since boot)
	// Connection states from /proc/net/tcp and /proc/net/tcp6
	// States: ESTABLISHED, SYN_SENT, SYN_RECV, FIN_WAIT1, FIN_WAIT2,
	// TIME_WAIT, CLOSE, CLOSE_WAIT, LAST_ACK, LISTEN, CLOSING
	ConnectionsByState map[string]uint64 // Current count per state (instantaneous)
}

// SystemStats represents system-wide activity statistics from /proc/stat
// Used by SystemStatsCollector for monitoring interrupt and context switch activity
type SystemStats struct {
	// Total interrupts serviced since boot from /proc/stat (intr line, first value)
	Interrupts uint64 // Total interrupt count (cumulative counter)
	// Context switches since boot from /proc/stat (ctxt line)
	ContextSwitches uint64 // Total context switches (cumulative counter)
}

// KernelMessage represents a kernel log message from /dev/kmsg
type KernelMessage struct {
	// Message header fields from /dev/kmsg format:
	// <priority>,<sequence>,<timestamp>,<flags>;<message>
	Timestamp   time.Time // Microseconds since boot, converted to time.Time
	Facility    uint8     // Syslog facility (priority >> 3)
	Severity    uint8     // Syslog severity (priority & 7)
	SequenceNum uint64    // Kernel sequence number
	Message     string    // Raw message text after the semicolon
	// Parsed fields from message content
	Subsystem string // Kernel subsystem if identifiable
	Device    string // Device name if present in message
}

// KernelSeverity represents kernel message severity levels
type KernelSeverity uint8

const (
	KernelSeverityEmergency KernelSeverity = 0
	KernelSeverityAlert     KernelSeverity = 1
	KernelSeverityCritical  KernelSeverity = 2
	KernelSeverityError     KernelSeverity = 3
	KernelSeverityWarning   KernelSeverity = 4
	KernelSeverityNotice    KernelSeverity = 5
	KernelSeverityInfo      KernelSeverity = 6
	KernelSeverityDebug     KernelSeverity = 7
)

// CollectionConfig represents configuration for performance collection
type CollectionConfig struct {
	Interval          time.Duration
	EnabledCollectors map[MetricType]bool
	HostProcPath      string // Path to /proc (useful for containers)
	HostSysPath       string // Path to /sys (useful for containers)
	HostDevPath       string // Path to /dev (useful for containers)
	HostCgroupPath    string // Path to /sys/fs/cgroup (useful for containers)
	TopProcessCount   int    // Number of top processes to collect (by CPU usage)
}

// DefaultCollectionConfig returns a default configuration
func DefaultCollectionConfig() CollectionConfig {
	return CollectionConfig{
		Interval: time.Second,
		EnabledCollectors: map[MetricType]bool{
			// Runtime system resource collectors
			MetricTypeLoad:    true,
			MetricTypeMemory:  true,
			MetricTypeCPU:     true,
			MetricTypeProcess: true,
			MetricTypeDisk:    true,
			MetricTypeNetwork: true,
			MetricTypeTCP:     true,
			MetricTypeSystem:  true,
			MetricTypeKernel:  true,
			// Runtime container resource collectors
			MetricTypeCgroupCPU:    true,
			MetricTypeCgroupMemory: true,
			// Hardware configuration collectors
			MetricTypeCPUInfo:     true,
			MetricTypeMemoryInfo:  true,
			MetricTypeDiskInfo:    true,
			MetricTypeNetworkInfo: true,
			MetricTypeNUMAStats:   true,
		},
		HostProcPath:   "/proc",
		HostSysPath:    "/sys",
		HostDevPath:    "/dev",
		HostCgroupPath: "/sys/fs/cgroup",
	}
}

// ApplyDefaults fills in zero values with defaults
func (c *CollectionConfig) ApplyDefaults() {
	defaults := DefaultCollectionConfig()

	if c.Interval == 0 {
		c.Interval = defaults.Interval
	}
	if c.EnabledCollectors == nil {
		c.EnabledCollectors = defaults.EnabledCollectors
	}
	if c.HostProcPath == "" {
		c.HostProcPath = defaults.HostProcPath
	}
	if c.HostSysPath == "" {
		c.HostSysPath = defaults.HostSysPath
	}
	if c.HostDevPath == "" {
		c.HostDevPath = defaults.HostDevPath
	}
	if c.HostCgroupPath == "" {
		c.HostCgroupPath = defaults.HostCgroupPath
	}
}

// ValidateOptions specifies validation requirements for CollectionConfig
type ValidateOptions struct {
	RequireHostProcPath   bool
	RequireHostSysPath    bool
	RequireHostDevPath    bool
	RequireHostCgroupPath bool
}

// Validate ensures that all configured paths are absolute paths and that required paths are non-empty.
// This centralizes path validation logic previously duplicated across all collectors.
func (c *CollectionConfig) Validate(opt ValidateOptions) error {

	// Check required paths are non-empty
	if opt.RequireHostProcPath && c.HostProcPath == "" {
		return fmt.Errorf("HostProcPath is required but not provided")
	}
	if opt.RequireHostSysPath && c.HostSysPath == "" {
		return fmt.Errorf("HostSysPath is required but not provided")
	}
	if opt.RequireHostDevPath && c.HostDevPath == "" {
		return fmt.Errorf("HostDevPath is required but not provided")
	}
	if opt.RequireHostCgroupPath && c.HostCgroupPath == "" {
		return fmt.Errorf("HostCgroupPath is required but not provided")
	}

	// Check all non-empty paths are absolute
	if c.HostProcPath != "" && !filepath.IsAbs(c.HostProcPath) {
		return fmt.Errorf("HostProcPath must be an absolute path, got: %q", c.HostProcPath)
	}
	if c.HostSysPath != "" && !filepath.IsAbs(c.HostSysPath) {
		return fmt.Errorf("HostSysPath must be an absolute path, got: %q", c.HostSysPath)
	}
	if c.HostDevPath != "" && !filepath.IsAbs(c.HostDevPath) {
		return fmt.Errorf("HostDevPath must be an absolute path, got: %q", c.HostDevPath)
	}
	if c.HostCgroupPath != "" && !filepath.IsAbs(c.HostCgroupPath) {
		return fmt.Errorf("HostCgroupPath must be an absolute path, got: %q", c.HostCgroupPath)
	}
	return nil
}

// CPUInfo represents CPU hardware configuration
type CPUInfo struct {
	// CPU counts
	// PhysicalCores represents the number of physical CPU cores. If physical topology
	// information is unavailable (e.g., in virtualized environments), this field falls
	// back to counting logical cores instead. This behavior ensures compatibility but
	// may not always reflect the actual physical core count.
	PhysicalCores int32
	LogicalCores  int32
	// CPU identification
	ModelName string
	VendorID  string
	CPUFamily int32 // CPU family number (e.g., 6, 15, 23)
	Model     int32 // CPU model number (e.g., 85, 94, 69)
	Stepping  int32 // CPU stepping number (e.g., 1, 2, 7)
	Microcode string
	// CPU frequencies
	CPUMHz    float64 // Current frequency from /proc/cpuinfo
	CPUMinMHz float64 // Minimum frequency from /sys/devices/system/cpu/cpu0/cpufreq/
	CPUMaxMHz float64 // Maximum frequency from /sys/devices/system/cpu/cpu0/cpufreq/
	// Cache sizes (from /proc/cpuinfo)
	CacheSize      string
	CacheAlignment int32
	// CPU features
	Flags []string // CPU flags/features
	// NUMA information
	NUMANodes int32
	// Additional info
	BogoMIPS float64
	// Per-core info if needed
	Cores []CPUCore
}

// CPUCore represents per-core CPU information
type CPUCore struct {
	Processor  int32   // Processor number
	CoreID     int32   // Physical core ID
	PhysicalID int32   // Physical package ID
	Siblings   int32   // Number of siblings
	CPUMHz     float64 // Current frequency
}

// MemoryInfo represents memory hardware configuration and NUMA topology
// Used by MemoryInfoCollector for hardware inventory and capacity planning
type MemoryInfo struct {
	// Total memory from /proc/meminfo
	TotalBytes uint64
	// Whether NUMA is enabled/available on this system
	NUMAEnabled bool
	// Whether automatic NUMA balancing is available (from /proc/sys/kernel/numa_balancing)
	NUMABalancingAvailable bool
	// NUMA configuration from /sys/devices/system/node/
	NUMANodes []NUMANode
}

// NUMANode represents a NUMA memory node
type NUMANode struct {
	NodeID     int32
	TotalBytes uint64
	CPUs       []int32 // CPU cores in this NUMA node
	// Distance to other nodes (from /sys/devices/system/node/node*/distance)
	// Index corresponds to target node ID, value is relative distance
	// Lower is better, typically 10 for local, 20+ for remote nodes
	Distances []int32
}

// DiskInfo represents disk hardware configuration
type DiskInfo struct {
	// Device identification
	Device string // e.g., sda, nvme0n1
	Model  string // From /sys/block/[device]/device/model
	Vendor string // From /sys/block/[device]/device/vendor
	// Disk properties
	SizeBytes uint64 // From /sys/block/[device]/size * block_size
	BlockSize uint32 // From /sys/block/[device]/queue/logical_block_size
	// Disk type
	Rotational bool // From /sys/block/[device]/queue/rotational (true=HDD, false=SSD)
	// Queue configuration
	QueueDepth uint32 // From /sys/block/[device]/queue/nr_requests
	Scheduler  string // From /sys/block/[device]/queue/scheduler
	// Physical properties
	PhysicalBlockSize uint32 // From /sys/block/[device]/queue/physical_block_size
	// Partitions
	Partitions []PartitionInfo
}

// PartitionInfo represents partition information
type PartitionInfo struct {
	Name        string
	SizeBytes   uint64
	StartSector uint64
}

// NetworkInfo represents network interface hardware configuration
type NetworkInfo struct {
	// Interface identification
	Interface string // Interface name
	Driver    string // From /sys/class/net/[interface]/device/driver
	// Hardware properties
	MACAddress string // From /sys/class/net/[interface]/address
	Speed      uint64 // Mbps from /sys/class/net/[interface]/speed
	Duplex     string // From /sys/class/net/[interface]/duplex
	// Configuration
	MTU uint32 // From /sys/class/net/[interface]/mtu
	// Interface type
	Type string // ethernet, wireless, loopback, etc.
	// State
	OperState string // From /sys/class/net/[interface]/operstate
	Carrier   bool   // From /sys/class/net/[interface]/carrier
}

// NUMAStatistics represents runtime NUMA performance statistics
// This is collected continuously to monitor allocation patterns and performance
type NUMAStatistics struct {
	// Whether NUMA is enabled on this system (must match NUMAInfo.Enabled)
	Enabled bool
	// Number of NUMA nodes (must match NUMAInfo.NodeCount)
	NodeCount int
	// Runtime statistics per node
	Nodes []NUMANodeStatistics
	// Whether automatic NUMA balancing is currently enabled (runtime state)
	AutoBalanceEnabled bool
}

// NUMANodeStatistics represents runtime statistics for a single NUMA node
type NUMANodeStatistics struct {
	// Node ID (0-based, must match NUMANodeInfo.ID)
	ID int
	// Current memory usage in bytes (from /sys/devices/system/node/node*/meminfo)
	MemFree   uint64 // Currently free
	MemUsed   uint64 // Currently used
	FilePages uint64 // File-backed pages (page cache)
	AnonPages uint64 // Anonymous pages (process memory)
	// NUMA allocation statistics in pages (from /sys/devices/system/node/node*/numastat)
	// These are monotonically increasing counters since boot
	NumaHit       uint64 // Memory successfully allocated on intended node
	NumaMiss      uint64 // Memory allocated here despite preferring different node
	NumaForeign   uint64 // Memory intended for here but allocated elsewhere
	InterleaveHit uint64 // Interleaved memory successfully allocated here
	LocalNode     uint64 // Memory allocated here while process was running here
	OtherNode     uint64 // Memory allocated here while process was on other node
}

// CgroupCPUStats represents CPU resource usage and throttling for a container
type CgroupCPUStats struct {
	// Container identification
	ContainerID   string
	ContainerName string // If available from runtime
	CgroupPath    string

	// CPU usage
	UsageNanos   uint64  // Total CPU time consumed in nanoseconds
	UsagePercent float64 // Calculated CPU usage percentage

	// CPU throttling (from cpu.stat)
	NrPeriods     uint64 // Number of enforcement periods
	NrThrottled   uint64 // Number of times throttled
	ThrottledTime uint64 // Total time throttled in nanoseconds

	// CPU limits
	CpuShares   uint64 // Relative weight (cpu.shares)
	CpuQuotaUs  int64  // Quota in microseconds per period (-1 if unlimited)
	CpuPeriodUs uint64 // Period length in microseconds

	// Calculated metrics
	ThrottlePercent float64 // Percentage of periods throttled
}

// CgroupMemoryStats represents memory usage and pressure for a container
type CgroupMemoryStats struct {
	// Container identification
	ContainerID   string
	ContainerName string
	CgroupPath    string

	// Memory usage (from memory.stat)
	RSS        uint64 // Resident set size
	Cache      uint64 // Page cache memory
	MappedFile uint64 // Memory mapped files
	Swap       uint64 // Swap usage

	// Detailed breakdown
	ActiveAnon   uint64 // Active anonymous pages
	InactiveAnon uint64 // Inactive anonymous pages
	ActiveFile   uint64 // Active file cache
	InactiveFile uint64 // Inactive file cache

	// Memory limits
	LimitBytes    uint64 // Memory limit (memory.limit_in_bytes)
	UsageBytes    uint64 // Current usage (memory.usage_in_bytes)
	MaxUsageBytes uint64 // Peak usage (memory.max_usage_in_bytes)

	// Memory pressure
	FailCount    uint64 // Number of times limit was hit
	OOMKillCount uint64 // Number of OOM kills
	UnderOOM     bool   // Currently under OOM

	// Calculated metrics
	UsagePercent float64 // Usage as percentage of limit
	CachePercent float64 // Cache as percentage of total usage
}

// ContainerInfo provides container runtime metadata
type ContainerInfo struct {
	ID        string
	Name      string
	Runtime   string // docker, containerd, cri-o
	State     string // running, paused, stopped
	StartedAt time.Time
	Labels    map[string]string
}
