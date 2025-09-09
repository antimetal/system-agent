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
)

// DeltaCalculationMode represents how delta/rate calculations are performed
type DeltaCalculationMode string

const (
	// DeltaModeDisabled disables all delta calculations (default for backward compatibility)
	DeltaModeDisabled DeltaCalculationMode = "disabled"
	// DeltaModeEnabled enables delta and rate calculations
	DeltaModeEnabled DeltaCalculationMode = "enabled"
)

// DeltaConfig represents configuration for delta/rate calculations
type DeltaConfig struct {
	// Mode controls what types of delta calculations are performed
	Mode DeltaCalculationMode
	// MinInterval is the minimum collection interval for meaningful rate calculations
	// Rates won't be calculated if the actual interval is less than this value
	MinInterval time.Duration
	// MaxInterval is the maximum interval before deltas are considered stale
	// If more time passes, the collector will reset and skip delta calculation
	MaxInterval time.Duration
}

// DefaultDeltaConfig returns a sensible default delta configuration
func DefaultDeltaConfig() DeltaConfig {
	return DeltaConfig{
		Mode:        DeltaModeDisabled,      // Backward compatible default
		MinInterval: 100 * time.Millisecond, // Avoid division by very small intervals
		MaxInterval: 5 * time.Minute,        // Reset state if gap is too large
	}
}

// IsEnabled returns whether delta calculation is enabled for a specific collector type
func (d DeltaConfig) IsEnabled(metricType MetricType) bool {
	if d.Mode == DeltaModeDisabled {
		return false
	}

	return d.isSupported(metricType)
}

// isSupported returns whether a metric type supports delta calculations
func (d DeltaConfig) isSupported(metricType MetricType) bool {
	switch metricType {
	case MetricTypeTCP, MetricTypeNetwork, MetricTypeCPU, MetricTypeSystem, MetricTypeDisk, MetricTypeMemory, MetricTypeNUMAStats:
		return true
	default:
		return false
	}
}

// DeltaMetadata contains metadata about delta calculations
type DeltaMetadata struct {
	// CollectionInterval is the actual time elapsed since the last collection
	CollectionInterval time.Duration
	// LastCollectionTime is when the previous collection occurred
	LastCollectionTime time.Time
	// IsFirstCollection indicates if this is the first collection (no deltas available)
	IsFirstCollection bool
	// CounterResetDetected indicates if a counter reset/rollover was detected
	CounterResetDetected bool
}

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
	CPU       []*CPUStats
	Processes []*ProcessStats
	Disks     []*DiskStats
	Network   []*NetworkStats
	TCP       *TCPStats
	System    *SystemStats
	Kernel    []KernelMessage
	// Hardware configuration
	CPUInfo     *CPUInfo
	MemoryInfo  *MemoryInfo
	DiskInfo    []*DiskInfo
	NetworkInfo []*NetworkInfo
	NUMAStats   *NUMAStatistics
}

// LoadStats represents system load information
type LoadStats struct {
	// Load averages from /proc/loadavg (1st, 2nd, 3rd fields)
	Load1Min  float64 `json:"load_1min"`
	Load5Min  float64 `json:"load_5min"`
	Load15Min float64 `json:"load_15min"`
	// Running/total processes from /proc/loadavg (4th field, e.g., "2/1234")
	RunningProcs int32 `json:"running_procs"`
	TotalProcs   int32 `json:"total_procs"`
	// Blocked processes from /proc/stat (procs_blocked field)
	BlockedProcs int32 `json:"blocked_procs"`
	// Last PID from /proc/loadavg (5th field)
	LastPID int32 `json:"last_pid"`
	// System uptime from /proc/uptime (1st field in seconds)
	Uptime time.Duration `json:"uptime"`
}

// MemoryStats represents runtime memory usage statistics from /proc/meminfo
// Used by MemoryCollector for operational monitoring and performance analysis
type MemoryStats struct {
	// Basic memory stats (all values in kB from /proc/meminfo)
	MemTotal     uint64 `json:"mem_total"`     // MemTotal: Total usable RAM
	MemFree      uint64 `json:"mem_free"`      // MemFree: Free memory
	MemAvailable uint64 `json:"mem_available"` // MemAvailable: Available memory for starting new applications
	Buffers      uint64 `json:"buffers"`       // Buffers: Memory in buffer cache
	Cached       uint64 `json:"cached"`        // Cached: Memory in page cache (excluding SwapCached)
	SwapCached   uint64 `json:"swap_cached"`   // SwapCached: Memory that was swapped out and is now back in RAM
	// Active/Inactive memory
	Active   uint64 `json:"active"`   // Active: Memory that has been used recently
	Inactive uint64 `json:"inactive"` // Inactive: Memory that hasn't been used recently
	// Swap stats
	SwapTotal uint64 `json:"swap_total"` // SwapTotal: Total swap space
	SwapFree  uint64 `json:"swap_free"`  // SwapFree: Unused swap space
	// Swap activity (cumulative counters from /proc/vmstat)
	SwapIn  uint64 `json:"swap_in"`  // pswpin: Pages swapped in since boot
	SwapOut uint64 `json:"swap_out"` // pswpout: Pages swapped out since boot

	Delta *MemoryDeltaData `json:"delta,omitempty"` // Delta data structure containing all calculated deltas and rates

	// Dirty pages
	Dirty     uint64 `json:"dirty"`     // Dirty: Memory waiting to be written back to disk
	Writeback uint64 `json:"writeback"` // Writeback: Memory actively being written back to disk
	// Anonymous memory
	AnonPages uint64 `json:"anon_pages"` // AnonPages: Non-file backed pages mapped into userspace
	Mapped    uint64 `json:"mapped"`     // Mapped: Files which have been mapped into memory
	Shmem     uint64 `json:"shmem"`      // Shmem: Total shared memory
	// Slab allocator
	Slab         uint64 `json:"slab"`          // Slab: Total slab allocator memory
	SReclaimable uint64 `json:"s_reclaimable"` // SReclaimable: Reclaimable slab memory
	SUnreclaim   uint64 `json:"s_unreclaim"`   // SUnreclaim: Unreclaimable slab memory
	// Kernel memory
	KernelStack uint64 `json:"kernel_stack"` // KernelStack: Memory used by kernel stacks
	PageTables  uint64 `json:"page_tables"`  // PageTables: Memory used by page tables
	// Memory commit
	CommitLimit uint64 `json:"commit_limit"` // CommitLimit: Total amount of memory that can be allocated
	CommittedAS uint64 `json:"committed_as"` // Committed_AS: Total committed memory
	// Virtual memory
	VmallocTotal uint64 `json:"vmalloc_total"` // VmallocTotal: Total size of vmalloc virtual address space
	VmallocUsed  uint64 `json:"vmalloc_used"`  // VmallocUsed: Used vmalloc area
	// HugePages
	HugePages_Total uint64 `json:"hugepages_total"` // HugePages_Total: Total number of hugepages
	HugePages_Free  uint64 `json:"hugepages_free"`  // HugePages_Free: Number of free hugepages
	HugePages_Rsvd  uint64 `json:"hugepages_rsvd"`  // HugePages_Rsvd: Number of reserved hugepages
	HugePages_Surp  uint64 `json:"hugepages_surp"`  // HugePages_Surp: Number of surplus hugepages
	HugePagesize    uint64 `json:"hugepage_size"`   // Hugepagesize: Default hugepage size (in kB)
	Hugetlb         uint64 `json:"hugetlb"`         // Hugetlb: Total memory consumed by huge pages of all sizes
}

// MemoryDeltaData contains all delta calculations for memory statistics
// This replaces individual pointer fields with a single nested structure
type MemoryDeltaData struct {
	DeltaMetadata

	// Delta values for swap activity (change since last collection)
	SwapIn  uint64 `json:"swap_in"`
	SwapOut uint64 `json:"swap_out"`

	// Rate values for swap activity (deltas per second)
	SwapInPerSec  uint64 `json:"swap_in_per_sec"`
	SwapOutPerSec uint64 `json:"swap_out_per_sec"`
}

// CPUStats represents per-CPU statistics from /proc/stat
type CPUStats struct {
	// CPU index (-1 for aggregate "cpu" line, 0+ for "cpu0", "cpu1", etc.)
	CPUIndex int32 `json:"cpu_index"`
	// Time spent in different CPU states (in USER_HZ units from /proc/stat)
	User      uint64 `json:"user"`       // Time in user mode
	Nice      uint64 `json:"nice"`       // Time in user mode with low priority (nice)
	System    uint64 `json:"system"`     // Time in system mode
	Idle      uint64 `json:"idle"`       // Time spent idle
	IOWait    uint64 `json:"io_wait"`    // Time waiting for I/O completion
	IRQ       uint64 `json:"irq"`        // Time servicing interrupts
	SoftIRQ   uint64 `json:"soft_irq"`   // Time servicing softirqs
	Steal     uint64 `json:"steal"`      // Time stolen by other operating systems in virtualized environment
	Guest     uint64 `json:"guest"`      // Time spent running a virtual CPU for guest OS
	GuestNice uint64 `json:"guest_nice"` // Time spent running a niced guest

	// Delta data
	Delta *CPUDeltaData `json:"delta,omitempty"`
}

// CPUDeltaData contains delta calculations and rates for CPU time monitoring
type CPUDeltaData struct {
	DeltaMetadata

	// Delta values (change since last collection)
	User      uint64 `json:"user"`
	Nice      uint64 `json:"nice"`
	System    uint64 `json:"system"`
	Idle      uint64 `json:"idle"`
	IOWait    uint64 `json:"io_wait"`
	IRQ       uint64 `json:"irq"`
	SoftIRQ   uint64 `json:"soft_irq"`
	Steal     uint64 `json:"steal"`
	Guest     uint64 `json:"guest"`
	GuestNice uint64 `json:"guest_nice"`

	// Calculated utilization percentages
	UserPercent      float64 `json:"user_percent"`
	NicePercent      float64 `json:"nice_percent"`
	SystemPercent    float64 `json:"system_percent"`
	IdlePercent      float64 `json:"idle_percent"`
	IOWaitPercent    float64 `json:"io_wait_percent"`
	IRQPercent       float64 `json:"irq_percent"`
	SoftIRQPercent   float64 `json:"soft_irq_percent"`
	StealPercent     float64 `json:"steal_percent"`
	GuestPercent     float64 `json:"guest_percent"`
	GuestNicePercent float64 `json:"guest_nice_percent"`
}

// ProcessStats represents per-process statistics
type ProcessStats struct {
	// Basic process info from /proc/[pid]/stat
	PID     int32  `json:"pid"`     // Process ID (field 1 in stat)
	PPID    int32  `json:"ppid"`    // Parent process ID (field 4 in stat)
	PGID    int32  `json:"pgid"`    // Process group ID (field 5 in stat)
	SID     int32  `json:"sid"`     // Session ID (field 6 in stat)
	Command string `json:"command"` // Command name from /proc/[pid]/comm or stat field 2
	State   string `json:"state"`   // Process state (field 3 in stat: R, S, D, Z, T, etc.)
	// CPU stats from /proc/[pid]/stat
	CPUTime    uint64  `json:"cpu_time"`    // Total CPU time: utime + stime (fields 14+15)
	CPUPercent float64 `json:"cpu_percent"` // Calculated CPU usage percentage
	// Memory stats
	MemoryVSZ uint64 `json:"memory_vsz"` // Virtual memory size from /proc/[pid]/stat (field 23)
	MemoryRSS uint64 `json:"memory_rss"` // Resident set size from /proc/[pid]/stat (field 24) * page_size
	MemoryPSS uint64 `json:"memory_pss"` // Proportional set size from /proc/[pid]/smaps_rollup
	MemoryUSS uint64 `json:"memory_uss"` // Unique set size from /proc/[pid]/smaps_rollup
	// Thread count from /proc/[pid]/stat
	Threads int32 `json:"threads"` // Number of threads (field 20)
	// Page faults from /proc/[pid]/stat
	MinorFaults uint64 `json:"minor_faults"` // Minor faults (field 10)
	MajorFaults uint64 `json:"major_faults"` // Major faults (field 12)
	// Process timing
	StartTime time.Time `json:"start_time"` // Process start time calculated from stat field 22 + boot time
	// Scheduling info from /proc/[pid]/stat
	Nice     int32 `json:"nice"`     // Nice value (field 19)
	Priority int32 `json:"priority"` // Priority (field 18)
	// File descriptors from /proc/[pid]/fd/
	NumFds     int32 `json:"num_fds"`     // Number of open file descriptors
	NumThreads int32 `json:"num_threads"` // Thread count from /proc/[pid]/status
	// Context switches from /proc/[pid]/status
	VoluntaryCtxt   uint64 `json:"voluntary_ctxt"`   // voluntary_ctxt_switches
	InvoluntaryCtxt uint64 `json:"involuntary_ctxt"` // nonvoluntary_ctxt_switches
}

// DiskStats represents disk I/O statistics from /proc/diskstats
type DiskStats struct {
	// Device identification
	Device string `json:"device"` // Device name (field 3 in /proc/diskstats)
	Major  uint32 `json:"major"`  // Major device number (field 1)
	Minor  uint32 `json:"minor"`  // Minor device number (field 2)
	// Read statistics (fields 4-7 in /proc/diskstats)
	ReadsCompleted uint64 `json:"reads_completed"` // Successfully completed reads
	ReadsMerged    uint64 `json:"reads_merged"`    // Reads merged before queuing
	SectorsRead    uint64 `json:"sectors_read"`    // Sectors read (multiply by 512 for bytes)
	ReadTime       uint64 `json:"read_time"`       // Time spent reading (milliseconds)
	// Write statistics (fields 8-11 in /proc/diskstats)
	WritesCompleted uint64 `json:"writes_completed"` // Successfully completed writes
	WritesMerged    uint64 `json:"writes_merged"`    // Writes merged before queuing
	SectorsWritten  uint64 `json:"sectors_written"`  // Sectors written (multiply by 512 for bytes)
	WriteTime       uint64 `json:"write_time"`       // Time spent writing (milliseconds)
	// I/O queue statistics (fields 12-14 in /proc/diskstats)
	IOsInProgress  uint64 `json:"i_os_in_progress"` // I/Os currently in progress
	IOTime         uint64 `json:"io_time"`          // Time spent doing I/Os (milliseconds)
	WeightedIOTime uint64 `json:"weighted_io_time"` // Weighted time spent doing I/Os (milliseconds)

	Delta *DiskDeltaData `json:"delta,omitempty"` // Delta data structure containing all calculated deltas and rates
}

// DiskDeltaData contains all delta calculations for disk statistics
type DiskDeltaData struct {
	DeltaMetadata

	// Delta values (change since last collection)
	ReadsCompleted  uint64 `json:"reads_completed"`
	WritesCompleted uint64 `json:"writes_completed"`
	ReadsMerged     uint64 `json:"reads_merged"`
	WritesMerged    uint64 `json:"writes_merged"`
	SectorsRead     uint64 `json:"sectors_read"`
	SectorsWritten  uint64 `json:"sectors_written"`
	ReadTime        uint64 `json:"read_time"`
	WriteTime       uint64 `json:"write_time"`
	IOTime          uint64 `json:"io_time"`
	WeightedIOTime  uint64 `json:"weighted_io_time"`

	// Rate values (deltas per second)
	ReadsPerSec          uint64 `json:"reads_per_sec"`
	WritesPerSec         uint64 `json:"writes_per_sec"`
	SectorsReadPerSec    uint64 `json:"sectors_read_per_sec"`
	SectorsWrittenPerSec uint64 `json:"sectors_written_per_sec"`

	// Calculated performance metrics (emulating iostat output)
	IOPS             uint64  `json:"iops"`                // iostat: r/s + w/s - Total I/O operations per second
	ReadBytesPerSec  uint64  `json:"read_bytes_per_sec"`  // iostat: rKB/s * 1024 - Read throughput in bytes/sec
	WriteBytesPerSec uint64  `json:"write_bytes_per_sec"` // iostat: wKB/s * 1024 - Write throughput in bytes/sec
	Utilization      float64 `json:"utilization"`         // iostat: %util - Disk utilization percentage (0-100)
	AvgQueueSize     float64 `json:"avg_queue_size"`      // iostat: avgqu-sz - Average queue depth
	AvgReadLatency   uint64  `json:"avg_read_latency"`    // iostat: r_await - Average read latency in milliseconds
	AvgWriteLatency  uint64  `json:"avg_write_latency"`   // iostat: w_await - Average write latency in milliseconds
}

// NetworkStats represents network interface statistics
type NetworkStats struct {
	// Interface name from /proc/net/dev
	Interface string `json:"interface"`
	// Receive statistics from /proc/net/dev (columns 2-9)
	RxBytes      uint64 `json:"rx_bytes"`      // Bytes received
	RxPackets    uint64 `json:"rx_packets"`    // Packets received
	RxErrors     uint64 `json:"rx_errors"`     // Receive errors
	RxDropped    uint64 `json:"rx_dropped"`    // Packets dropped on receive
	RxFIFO       uint64 `json:"rx_fifo"`       // FIFO buffer errors
	RxFrame      uint64 `json:"rx_frame"`      // Frame alignment errors
	RxCompressed uint64 `json:"rx_compressed"` // Compressed packets received
	RxMulticast  uint64 `json:"rx_multicast"`  // Multicast packets received
	// Transmit statistics from /proc/net/dev (columns 10-17)
	TxBytes      uint64 `json:"tx_bytes"`      // Bytes transmitted
	TxPackets    uint64 `json:"tx_packets"`    // Packets transmitted
	TxErrors     uint64 `json:"tx_errors"`     // Transmit errors
	TxDropped    uint64 `json:"tx_dropped"`    // Packets dropped on transmit
	TxFIFO       uint64 `json:"tx_fifo"`       // FIFO buffer errors
	TxCollisions uint64 `json:"tx_collisions"` // Collisions detected
	TxCarrier    uint64 `json:"tx_carrier"`    // Carrier losses
	TxCompressed uint64 `json:"tx_compressed"` // Compressed packets transmitted
	// Interface metadata from /sys/class/net/[interface]/
	Speed        uint64 `json:"speed"`         // Link speed in Mbps from /sys/class/net/[interface]/speed
	Duplex       string `json:"duplex"`        // Duplex mode from /sys/class/net/[interface]/duplex
	OperState    string `json:"oper_state"`    // Operational state from /sys/class/net/[interface]/operstate
	LinkDetected bool   `json:"link_detected"` // Link detection from /sys/class/net/[interface]/carrier

	Delta *NetworkDeltaData `json:"delta,omitempty"` // Delta data structure containing all calculated deltas and rates
}

// NetworkDeltaData contains all delta calculations for network statistics
type NetworkDeltaData struct {
	DeltaMetadata

	// Delta values (change since last collection)
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	TxPackets uint64 `json:"tx_packets"`
	RxErrors  uint64 `json:"rx_errors"`
	TxErrors  uint64 `json:"tx_errors"`
	RxDropped uint64 `json:"rx_dropped"`
	TxDropped uint64 `json:"tx_dropped"`

	// Rate values (deltas per second)
	RxBytesPerSec   uint64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec   uint64 `json:"tx_bytes_per_sec"`
	RxPacketsPerSec uint64 `json:"rx_packets_per_sec"`
	TxPacketsPerSec uint64 `json:"tx_packets_per_sec"`
	RxErrorsPerSec  uint64 `json:"rx_errors_per_sec"`
	TxErrorsPerSec  uint64 `json:"tx_errors_per_sec"`
	RxDroppedPerSec uint64 `json:"rx_dropped_per_sec"`
	TxDroppedPerSec uint64 `json:"tx_dropped_per_sec"`
}

// TCPStats represents TCP connection statistics
type TCPStats struct {
	// Connection counts from /proc/net/snmp (Tcp: line)
	ActiveOpens  uint64 `json:"active_opens"`   // Active connection openings (count since boot)
	PassiveOpens uint64 `json:"passive_opens"`  // Passive connection openings (count since boot)
	AttemptFails uint64 `json:"attempt_fails"`  // Failed connection attempts (count since boot)
	EstabResets  uint64 `json:"estab_resets"`   // Resets from established state (count since boot)
	CurrEstab    uint64 `json:"curr_estab"`     // Current established connections (instantaneous count)
	InSegs       uint64 `json:"in_segs"`        // Segments received (count since boot)
	OutSegs      uint64 `json:"out_segs"`       // Segments sent (count since boot)
	RetransSegs  uint64 `json:"retrans_segs"`   // Segments retransmitted (count since boot)
	InErrs       uint64 `json:"in_errs"`        // Segments received with errors (count since boot)
	OutRsts      uint64 `json:"out_rsts"`       // RST segments sent (count since boot)
	InCsumErrors uint64 `json:"in_csum_errors"` // Segments with checksum errors (count since boot)
	// Extended TCP stats from /proc/net/netstat (TcpExt: line)
	SyncookiesSent      uint64 `json:"syncookies_sent"`        // SYN cookies sent (count since boot)
	SyncookiesRecv      uint64 `json:"syncookies_recv"`        // SYN cookies received (count since boot)
	SyncookiesFailed    uint64 `json:"syncookies_failed"`      // SYN cookies failed (count since boot)
	ListenOverflows     uint64 `json:"listen_overflows"`       // Listen queue overflows (count since boot)
	ListenDrops         uint64 `json:"listen_drops"`           // Listen queue drops (count since boot)
	TCPLostRetransmit   uint64 `json:"tcp_lost_retransmit"`    // Lost retransmissions (count since boot)
	TCPFastRetrans      uint64 `json:"tcp_fast_retrans"`       // Fast retransmissions (count since boot)
	TCPSlowStartRetrans uint64 `json:"tcp_slow_start_retrans"` // Slow start retransmissions (count since boot)
	TCPTimeouts         uint64 `json:"tcp_timeouts"`           // TCP timeouts (count since boot)
	// Connection states from /proc/net/tcp and /proc/net/tcp6
	// States: ESTABLISHED, SYN_SENT, SYN_RECV, FIN_WAIT1, FIN_WAIT2,
	// TIME_WAIT, CLOSE, CLOSE_WAIT, LAST_ACK, LISTEN, CLOSING
	ConnectionsByState map[string]uint64 `json:"connections_by_state,omitempty"` // Current count per state (instantaneous)

	Delta *TCPDeltaData `json:"delta,omitempty"` // Delta data structure containing all calculated deltas and rates
}

// TCPDeltaData contains all delta calculations for TCP statistics
type TCPDeltaData struct {
	DeltaMetadata

	// Delta values (change since last collection)
	ActiveOpens         uint64 `json:"active_opens"`
	PassiveOpens        uint64 `json:"passive_opens"`
	AttemptFails        uint64 `json:"attempt_fails"`
	EstabResets         uint64 `json:"estab_resets"`
	InSegs              uint64 `json:"in_segs"`
	OutSegs             uint64 `json:"out_segs"`
	RetransSegs         uint64 `json:"retrans_segs"`
	InErrs              uint64 `json:"in_errs"`
	OutRsts             uint64 `json:"out_rsts"`
	InCsumErrors        uint64 `json:"in_csum_errors"`
	SyncookiesSent      uint64 `json:"syncookies_sent"`
	SyncookiesRecv      uint64 `json:"syncookies_recv"`
	SyncookiesFailed    uint64 `json:"syncookies_failed"`
	ListenOverflows     uint64 `json:"listen_overflows"`
	ListenDrops         uint64 `json:"listen_drops"`
	TCPLostRetransmit   uint64 `json:"tcp_lost_retransmit"`
	TCPFastRetrans      uint64 `json:"tcp_fast_retrans"`
	TCPSlowStartRetrans uint64 `json:"tcp_slow_start_retrans"`
	TCPTimeouts         uint64 `json:"tcp_timeouts"`

	// Rate values (deltas per second)
	ActiveOpensPerSec         uint64 `json:"active_opens_per_sec"`
	PassiveOpensPerSec        uint64 `json:"passive_opens_per_sec"`
	AttemptFailsPerSec        uint64 `json:"attempt_fails_per_sec"`
	EstabResetsPerSec         uint64 `json:"estab_resets_per_sec"`
	InSegsPerSec              uint64 `json:"in_segs_per_sec"`
	OutSegsPerSec             uint64 `json:"out_segs_per_sec"`
	RetransSegsPerSec         uint64 `json:"retrans_segs_per_sec"`
	InErrsPerSec              uint64 `json:"in_errs_per_sec"`
	OutRstsPerSec             uint64 `json:"out_rsts_per_sec"`
	InCsumErrorsPerSec        uint64 `json:"in_csum_errors_per_sec"`
	SyncookiesSentPerSec      uint64 `json:"syncookies_sent_per_sec"`
	SyncookiesRecvPerSec      uint64 `json:"syncookies_recv_per_sec"`
	SyncookiesFailedPerSec    uint64 `json:"syncookies_failed_per_sec"`
	ListenOverflowsPerSec     uint64 `json:"listen_overflows_per_sec"`
	ListenDropsPerSec         uint64 `json:"listen_drops_per_sec"`
	TCPLostRetransmitPerSec   uint64 `json:"tcp_lost_retransmit_per_sec"`
	TCPFastRetransPerSec      uint64 `json:"tcp_fast_retrans_per_sec"`
	TCPSlowStartRetransPerSec uint64 `json:"tcp_slow_start_retrans_per_sec"`
	TCPTimeoutsPerSec         uint64 `json:"tcp_timeouts_per_sec"`
}

// SystemStats represents system-wide activity statistics from /proc/stat
// Used by SystemStatsCollector for monitoring interrupt and context switch activity
type SystemStats struct {
	// Total interrupts serviced since boot from /proc/stat (intr line, first value)
	Interrupts uint64 `json:"interrupts"` // Total interrupt count (cumulative counter)
	// Context switches since boot from /proc/stat (ctxt line)
	ContextSwitches uint64 `json:"context_switches"` // Total context switches (cumulative counter)

	Delta *SystemDeltaData `json:"delta,omitempty"` // Delta data structure containing all calculated deltas and rates
}

// SystemDeltaData contains all delta calculations for system statistics
// This replaces individual pointer fields with a single nested structure
type SystemDeltaData struct {
	DeltaMetadata

	// Delta values (change since last collection)
	Interrupts      uint64 `json:"interrupts"`
	ContextSwitches uint64 `json:"context_switches"`

	// Rate values (deltas per second)
	InterruptsPerSec      uint64 `json:"interrupts_per_sec"`
	ContextSwitchesPerSec uint64 `json:"context_switches_per_sec"`
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
	TopProcessCount   int    // Number of top processes to collect (by CPU usage)

	// Delta configuration for monotonic counter collectors
	Delta DeltaConfig
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
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
		Delta:        DefaultDeltaConfig(),
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

	// Apply delta defaults
	if c.Delta.Mode == "" {
		c.Delta.Mode = defaults.Delta.Mode
	}
	if c.Delta.MinInterval == 0 {
		c.Delta.MinInterval = defaults.Delta.MinInterval
	}
	if c.Delta.MaxInterval == 0 {
		c.Delta.MaxInterval = defaults.Delta.MaxInterval
	}
}

// ValidateOptions specifies validation requirements for CollectionConfig
type ValidateOptions struct {
	RequireHostProcPath bool
	RequireHostSysPath  bool
	RequireHostDevPath  bool
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

	Delta *NUMADeltaData // Delta data structure containing all calculated deltas and rates
}

// NUMADeltaData contains all delta calculations for NUMA node statistics
type NUMADeltaData struct {
	DeltaMetadata

	// Delta values for NUMA allocation counters (change since last collection)
	NumaHit       uint64
	NumaMiss      uint64
	NumaForeign   uint64
	InterleaveHit uint64
	LocalNode     uint64
	OtherNode     uint64

	// Rate values for NUMA allocations (deltas per second)
	NumaHitPerSec       uint64
	NumaMissPerSec      uint64
	NumaForeignPerSec   uint64
	InterleaveHitPerSec uint64
	LocalNodePerSec     uint64
	OtherNodePerSec     uint64
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
