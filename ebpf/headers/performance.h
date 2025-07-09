// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright (c) 2024 Antimetal

#ifndef __PERFORMANCE_H__
#define __PERFORMANCE_H__

#include <linux/types.h>

// Maximum lengths for various fields
#define TASK_COMM_LEN 16
#define MAX_CPU_NR 256
#define MAX_FILENAME_LEN 256

// Event types for performance monitoring
enum perf_event_type {
    PERF_EVENT_SCHED_SWITCH = 1,
    PERF_EVENT_PROCESS_FORK = 2,
    PERF_EVENT_PROCESS_EXIT = 3,
    PERF_EVENT_BLOCK_IO_START = 4,
    PERF_EVENT_BLOCK_IO_DONE = 5,
    PERF_EVENT_NET_PACKET_RX = 6,
    PERF_EVENT_NET_PACKET_TX = 7,
};

// CPU scheduling event structure
struct sched_event {
    __u64 timestamp;
    __u32 cpu;
    __u32 prev_pid;
    __u32 prev_tgid;
    __u32 next_pid;
    __u32 next_tgid;
    __u64 prev_runtime_ns;
    char prev_comm[TASK_COMM_LEN];
    char next_comm[TASK_COMM_LEN];
    __u8 prev_state;
    __u8 pad[3];
};

// Process lifecycle event structure
struct process_event {
    __u64 timestamp;
    __u32 cpu;
    __u32 pid;
    __u32 tgid;
    __u32 ppid;
    __u32 uid;
    __u32 gid;
    char comm[TASK_COMM_LEN];
    __u8 event_type; // FORK or EXIT
    __u8 pad[3];
    __u64 exit_code; // Only valid for EXIT events
};

// Block I/O event structure
struct io_event {
    __u64 timestamp;
    __u32 cpu;
    __u32 pid;
    __u32 tgid;
    __u32 major;
    __u32 minor;
    __u64 sector;
    __u32 nr_sector;
    __u32 rwbs; // Read/Write/Barrier/Sync flags
    __u64 latency_ns; // Only valid for completion events
    char comm[TASK_COMM_LEN];
    __u8 event_type; // START or DONE
    __u8 pad[3];
};

// Network packet event structure
struct net_event {
    __u64 timestamp;
    __u32 cpu;
    __u32 pid;
    __u32 tgid;
    __u32 netns;
    __u32 ifindex;
    __u32 len;
    __u16 protocol;
    __u8 direction; // 0=RX, 1=TX
    __u8 pad;
    __u64 skb_addr;
    char comm[TASK_COMM_LEN];
};

// Per-CPU statistics tracking
struct cpu_stat {
    __u64 user;
    __u64 nice;
    __u64 system;
    __u64 idle;
    __u64 iowait;
    __u64 irq;
    __u64 softirq;
    __u64 steal;
    __u64 guest;
    __u64 guest_nice;
};

// Per-process statistics tracking
struct process_stat {
    __u32 pid;
    __u32 tgid;
    __u64 cpu_time_ns;
    __u64 last_seen_ns;
    __u64 utime;
    __u64 stime;
    __u64 start_time;
    __u32 nr_threads;
    __u32 state;
    char comm[TASK_COMM_LEN];
};

// Block device statistics
struct block_stat {
    __u32 major;
    __u32 minor;
    __u64 read_ios;
    __u64 read_sectors;
    __u64 read_time_ns;
    __u64 write_ios;
    __u64 write_sectors;
    __u64 write_time_ns;
    __u64 in_flight;
    __u64 io_time_ns;
};

// Network interface statistics
struct net_stat {
    __u32 ifindex;
    __u64 rx_packets;
    __u64 rx_bytes;
    __u64 rx_errors;
    __u64 rx_dropped;
    __u64 tx_packets;
    __u64 tx_bytes;
    __u64 tx_errors;
    __u64 tx_dropped;
};

// Helper macros for BPF programs
#define PERF_MAX_STACK_DEPTH 127

// Common map definitions that can be reused
#define DEFINE_PERF_RINGBUF(name, size) \
    struct { \
        __uint(type, BPF_MAP_TYPE_RINGBUF); \
        __uint(max_entries, size); \
    } name SEC(".maps")

#define DEFINE_PERF_HASH(name, key_type, val_type, max_entries) \
    struct { \
        __uint(type, BPF_MAP_TYPE_HASH); \
        __type(key, key_type); \
        __type(value, val_type); \
        __uint(max_entries, max_entries); \
    } name SEC(".maps")

#define DEFINE_PERF_PERCPU_ARRAY(name, val_type, max_entries) \
    struct { \
        __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY); \
        __type(key, __u32); \
        __type(value, val_type); \
        __uint(max_entries, max_entries); \
    } name SEC(".maps")

// Timestamp helper
static __always_inline __u64 get_timestamp_ns(void) {
    return bpf_ktime_get_ns();
}

// CPU helper
static __always_inline __u32 get_cpu(void) {
    return bpf_get_smp_processor_id();
}

#endif /* __PERFORMANCE_H__ */