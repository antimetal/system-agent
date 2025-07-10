// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"time"
)

// MetricType represents the type of performance metric
type MetricType string

const (
	MetricTypeLoad    MetricType = "load"
	MetricTypeMemory  MetricType = "memory"
	MetricTypeCPU     MetricType = "cpu"
	MetricTypeProcess MetricType = "process"
	MetricTypeDisk    MetricType = "disk"
	MetricTypeNetwork MetricType = "network"
	MetricTypeTCP     MetricType = "tcp"
	MetricTypeKernel  MetricType = "kernel"
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
	Kernel    []KernelMessage
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
	// Last PID from /proc/loadavg (5th field)
	LastPID int32
	// System uptime from /proc/uptime (1st field in seconds)
	Uptime time.Duration
}

// MemoryStats represents memory usage information from /proc/meminfo
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
	HugePagesize    uint64 // Hugepagesize: Default hugepage size (in kB)
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
	// Calculated fields
	Utilization float64 // Percentage 0-100
	// Delta values for rate calculation
	DeltaTotal uint64
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
	// Calculated fields
	RxBytesPerSec   float64
	RxPacketsPerSec float64
	TxBytesPerSec   float64
	TxPacketsPerSec float64
	// Interface metadata from /sys/class/net/[interface]/
	Speed        uint64 // Link speed in Mbps from /sys/class/net/[interface]/speed
	Duplex       string // Duplex mode from /sys/class/net/[interface]/duplex
	OperState    string // Operational state from /sys/class/net/[interface]/operstate
	LinkDetected bool   // Link detection from /sys/class/net/[interface]/carrier
}

// TCPStats represents TCP connection statistics
type TCPStats struct {
	// Connection counts from /proc/net/snmp (Tcp: line)
	ActiveOpens  uint64 // Active connection openings
	PassiveOpens uint64 // Passive connection openings
	AttemptFails uint64 // Failed connection attempts
	EstabResets  uint64 // Resets from established state
	CurrEstab    uint64 // Current established connections
	InSegs       uint64 // Segments received
	OutSegs      uint64 // Segments sent
	RetransSegs  uint64 // Segments retransmitted
	InErrs       uint64 // Segments received with errors
	OutRsts      uint64 // RST segments sent
	InCsumErrors uint64 // Segments with checksum errors
	// Extended TCP stats from /proc/net/netstat (TcpExt: line)
	SyncookiesSent      uint64 // SYN cookies sent
	SyncookiesRecv      uint64 // SYN cookies received
	SyncookiesFailed    uint64 // SYN cookies failed
	ListenOverflows     uint64 // Listen queue overflows
	ListenDrops         uint64 // Listen queue drops
	TCPLostRetransmit   uint64 // Lost retransmissions
	TCPFastRetrans      uint64 // Fast retransmissions
	TCPSlowStartRetrans uint64 // Slow start retransmissions
	TCPTimeouts         uint64 // TCP timeouts
	// Connection states from /proc/net/tcp and /proc/net/tcp6
	// States: ESTABLISHED, SYN_SENT, SYN_RECV, FIN_WAIT1, FIN_WAIT2,
	// TIME_WAIT, CLOSE, CLOSE_WAIT, LAST_ACK, LISTEN, CLOSING
	ConnectionsByState map[string]uint64
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
}

// DefaultCollectionConfig returns a default configuration
func DefaultCollectionConfig() CollectionConfig {
	return CollectionConfig{
		Interval: time.Second,
		EnabledCollectors: map[MetricType]bool{
			MetricTypeLoad:    true,
			MetricTypeMemory:  true,
			MetricTypeCPU:     true,
			MetricTypeProcess: true,
			MetricTypeDisk:    true,
			MetricTypeNetwork: true,
			MetricTypeTCP:     true,
			MetricTypeKernel:  true,
		},
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
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
}
