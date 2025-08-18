// SPDX-License-Identifier: GPL-2.0-only
/*
 * This header defines the data structures shared between BPF and userspace
 * for the ring buffer-based streaming profiler.
 */

#ifndef __PROFILER_TYPES_H
#define __PROFILER_TYPES_H

#define MAX_STACK_DEPTH 64
#define PROF_COMM_LEN 16

// Profile event structure for ring buffer (32 bytes)
struct profile_event {
  __u64 timestamp;        // 8 bytes - nanoseconds since boot
  __s32 pid;              // 4 bytes - process ID
  __s32 tid;              // 4 bytes - thread ID
  __s32 user_stack_id;    // 4 bytes - stack trace ID
  __s32 kernel_stack_id;  // 4 bytes - stack trace ID
  __u32 cpu;              // 4 bytes - CPU number
  __u32 flags;            // 4 bytes - event flags
} __attribute__((packed));

// Flags for profile events
#define PROF_FLAG_USER_STACK_TRUNCATED (1 << 0)
#define PROF_FLAG_KERNEL_STACK_TRUNCATED (1 << 1)
#define PROF_FLAG_STACK_COLLISION (1 << 2)

// Statistics structure for monitoring
struct profiler_stats {
  __u64 events_processed;
  __u64 events_dropped;
  __u64 ring_buffer_full;
  __u64 stack_trace_errors;
};

#endif /* __PROFILER_TYPES_H */