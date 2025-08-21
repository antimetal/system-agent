// SPDX-License-Identifier: GPL-2.0-only
/* Ring buffer-based streaming eBPF CPU profiler */

#include "vmlinux.h"

#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

#include "../include/profiler_types.h"

char LICENSE[] SEC("license") = "GPL";

// Ring buffer for streaming events (8MB default)
struct {
  __uint(type, BPF_MAP_TYPE_RINGBUF);
  __uint(max_entries, 8 * 1024 * 1024);  // 8MB
} events SEC(".maps");

// Map to store stack traces
struct {
  __uint(type, BPF_MAP_TYPE_STACK_TRACE);
  __uint(key_size, sizeof(u32));
  __uint(value_size, MAX_STACK_DEPTH * sizeof(u64));
  __uint(max_entries, 2048);  // Reduced from 10000 for ring buffer mode
} stack_traces SEC(".maps");

// Global counter for dropped events (due to ring buffer full)
struct {
  __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
  __uint(key_size, sizeof(u32));
  __uint(value_size, sizeof(u64));
  __uint(max_entries, 1);
} dropped_events SEC(".maps");

SEC("perf_event")
int profile_cpu_cycles(struct bpf_perf_event_data *ctx) {
  struct task_struct *task;
  struct profile_event *event;
  u64 stack_flags = 0;
  u32 zero = 0;
  u64 *dropped;

  task = (struct task_struct *)bpf_get_current_task();

  // Skip kernel threads (PID 0)
  s32 pid = BPF_CORE_READ(task, tgid);
  if (pid == 0) {
    return 0;
  }

  // Reserve space in ring buffer
  event = bpf_ringbuf_reserve(&events, sizeof(struct profile_event), 0);
  if (!event) {
    // Ring buffer full - increment drop counter
    dropped = bpf_map_lookup_elem(&dropped_events, &zero);
    if (dropped) {
      __sync_fetch_and_add(dropped, 1);
    }
    return 0;
  }

  // Fill event structure
  event->timestamp = bpf_ktime_get_ns();
  event->pid = pid;
  event->tid = BPF_CORE_READ(task, pid);
  event->cpu = bpf_get_smp_processor_id();
  event->flags = 0;

  // Get user stack ID
  stack_flags = BPF_F_USER_STACK;
  event->user_stack_id = bpf_get_stackid(ctx, &stack_traces, stack_flags);
  if (event->user_stack_id < 0) {
    event->flags |= PROF_FLAG_USER_STACK_TRUNCATED;
  }

  // Get kernel stack ID
  stack_flags = 0;  // Kernel stack
  event->kernel_stack_id = bpf_get_stackid(ctx, &stack_traces, stack_flags);
  if (event->kernel_stack_id < 0) {
    event->flags |= PROF_FLAG_KERNEL_STACK_TRUNCATED;
  }

  // Submit event to ring buffer
  bpf_ringbuf_submit(event, 0);

  return 0;
}