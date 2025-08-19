// SPDX-License-Identifier: GPL-2.0-only
/* Copyright (c) 2024 Antimetal, Inc. */

#ifndef __MEMGROWTH_TYPES_H__
#define __MEMGROWTH_TYPES_H__

// Event types for memory growth monitoring
enum memgrowth_event_type {
  EVENT_NEW_PROCESS = 1,
  EVENT_GROWTH_DETECTED = 2,
  EVENT_HIGH_RISK = 3,
  EVENT_PROCESS_EXIT = 4,
  EVENT_OOM_WARNING = 5,
};

// Size of circular buffer for historical RSS tracking
// MUST be a power of 2 for efficient circular buffer indexing with bitwise AND
// The BPF verifier understands (index & MASK) patterns for bounds checking
#define RSS_HISTORY_SIZE 16  // Power of 2: allows bitwise masking
#define RSS_HISTORY_MASK (RSS_HISTORY_SIZE - 1)  // 0xF for size 16

// Process memory state tracked in kernel
struct process_memory_state {
  __u32 pid;
  __u32 tgid;
  __u64 start_time_ns;   // Start time in nanoseconds (for reference)
  __u32 last_update_ds;  // Deciseconds (tenths of second) since start
  __u64 current_rss;
  __u64 peak_rss;
  __u64 initial_rss;
  __u64 total_growth;
  __u64 growth_rate;  // bytes per second (instantaneous)
  __u32 sample_count;
  __u8 leak_confidence;  // 0-100
  __u8 pattern_type;     // 0=stable, 1=steady, 2=accelerating, 3=oscillating
  char comm[16];

  // RSS component tracking for ratio analysis
  __u64 rss_anon;      // Anonymous pages (heap, stack)
  __u64 rss_file;      // File-backed pages (code, libs, mmap files)
  __u64 rss_swap;      // Swap entries
  __u64 rss_shmem;     // Shared memory pages
  
  // Initial values for growth tracking
  __u64 initial_anon;
  __u64 initial_file;
  
  // Growth rates by type
  __s64 anon_growth_rate;  // bytes/sec (can be negative)
  __s64 file_growth_rate;  // bytes/sec (can be negative)
  
  // Memory ratios (stored as percentage * 10 for precision)
  __u16 anon_ratio;    // Anonymous memory % * 10 (0-1000)
  __u16 swap_ratio;    // Swap usage % * 10 (0-1000)
  
  // Monotonic growth tracking (for 5-minute threshold)
  __u32 monotonic_start_ds;  // When monotonic growth started (deciseconds)
  __u32 last_decrease_ds;    // Last time RSS decreased
  __u8 is_monotonic;         // Currently in monotonic growth phase

  // VSZ tracking for divergence detection
  __u64 vsz_bytes;           // Virtual memory size
  __u64 initial_vsz;         // Initial VSZ for comparison
  __s64 vsz_growth_rate;     // VSZ growth rate bytes/sec
  __u16 vsz_rss_ratio;       // VSZ/RSS ratio * 10 (for precision)
  
  // Additional memory stats for reporting
  __u64 total_vm;    // Total virtual memory in bytes (same as vsz_bytes)
  __u64 heap_size;   // Heap size (TODO: implement proper collection)
  __u64 stack_size;  // Stack size (TODO: implement proper collection)

  // Circular buffer for RSS history (for trend analysis)
  __u32 rss_history_mb[RSS_HISTORY_SIZE];  // RSS in MB (saves space)
  // Time stored in deciseconds (tenths of second) - design tradeoff:
  // - Nanoseconds (u64): 8 bytes, perfect precision, but excessive for our
  // needs
  // - Seconds (u32): 4 bytes, but too coarse for 500ms coalescing threshold
  // - Deciseconds (u32): 4 bytes, 100ms resolution, perfect for our use case
  //   Allows detecting samples within 500ms while keeping compact storage
  __u32 time_history_ds[RSS_HISTORY_SIZE];  // Deciseconds since start
  __u8 history_head;   // Next write position in circular buffer (0-15)
  __u8 history_count;  // Number of valid entries (max 16)

  // Trend analysis from linear regression
  __s64 trend_slope;  // Bytes per second trend (from MB-based regression)
  __u32 trend_r2;     // R-squared * 1000 (0-1000, quality of linear fit)
};

// Event sent to userspace
struct memory_growth_event {
  __u64 timestamp;
  __u32 pid;
  __u32 tgid;
  __u8 event_type;
  __u8 leak_confidence;
  __u8 pattern_type;  // 0=stable, 1=steady, 2=accelerating
  __u8 reserved;
  __u64 current_rss;
  __u64 total_growth;
  __u64 growth_rate;
  
  // RSS components for detailed analysis
  __u64 rss_anon;
  __u64 rss_file;
  __u64 rss_swap;
  __u16 anon_ratio;  // % * 10
  __u16 swap_ratio;  // % * 10
  
  __u64 heap_size;   // From mm_struct
  __u64 stack_size;  // From mm_struct
  __u64 total_vm;    // Total virtual memory
  char comm[16];
};

// Configuration from userspace
struct memgrowth_config {
  __u64 min_rss_threshold;      // Minimum RSS to track (bytes)
  __u64 growth_rate_threshold;  // Alert threshold (bytes/sec)
  __u32 min_process_age;        // Minimum age before tracking (ms)
  __u32 sample_interval;        // Sampling interval (ms)
  __u8 confidence_threshold;    // Alert confidence threshold (0-100)
  __u8 enabled;                 // Global enable/disable
};

#endif  // __MEMGROWTH_TYPES_H__