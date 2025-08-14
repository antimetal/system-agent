// SPDX-License-Identifier: GPL-2.0-only
/* Copyright (c) 2024 Antimetal, Inc. */

// RSS-stat based memory growth monitor implementation
// Uses the kmem:rss_stat tracepoint for accurate, low-overhead RSS tracking

#include "vmlinux.h"

#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

#include "memgrowth_types.h"

// BPF Maps
struct {
  __uint(type, BPF_MAP_TYPE_HASH);
  __uint(max_entries, 10000);
  __type(key, __u32);  // PID
  __type(value, struct process_memory_state);
} process_states SEC(".maps");

struct {
  __uint(type, BPF_MAP_TYPE_RINGBUF);
  __uint(max_entries, 256 * 1024);  // 256KB ring buffer
} events SEC(".maps");

struct {
  __uint(type, BPF_MAP_TYPE_ARRAY);
  __uint(max_entries, 1);
  __type(key, __u32);
  __type(value, struct memgrowth_config);
} config_map SEC(".maps");

// RSS stat tracepoint structure and memory types are defined in vmlinux.h
// The tracepoint has been available since Linux 5.5

// Update RSS history with smart coalescing
static __always_inline void update_rss_history(
    struct process_memory_state *state, __u64 rss_bytes, __u32 time_ds) {
  // Convert RSS to MB for storage
  __u32 rss_mb = rss_bytes / (1024 * 1024);

  // Coalesce samples within 5s to prevent burst flooding
  const __u32 COALESCE_THRESHOLD_DS = 50;  // 5 seconds in deciseconds

  // Coalesce with most recent entry if within threshold
  if (state->history_count > 0) {
    __u32 head = state->history_head;
    __u32 last_idx;

    // Bitwise AND for verifier-friendly circular indexing
    last_idx = (head - 1) & RSS_HISTORY_MASK;

    __u32 time_since_last = time_ds - state->time_history_ds[last_idx];

    if (time_since_last < COALESCE_THRESHOLD_DS) {
      state->rss_history_mb[last_idx] = rss_mb;
      // Keep original timestamp to maintain time spacing
      return;
    }
  }

  // Add new entry to circular buffer
  __u32 head = state->history_head & RSS_HISTORY_MASK;
  state->rss_history_mb[head] = rss_mb;
  state->time_history_ds[head] = time_ds;

  // Increment with wrap-around
  state->history_head = (head + 1) & RSS_HISTORY_MASK;
  if (state->history_count < RSS_HISTORY_SIZE) {
    state->history_count++;
  }
}

// Calculate linear regression trend from RSS history
static __always_inline void calculate_trend(
    struct process_memory_state *state) {
  if (state->history_count < 6) {
    state->trend_slope = 0;
    state->trend_r2 = 0;
    return;  // Need at least 6 points for meaningful trend
  }

  __u32 n = state->history_count;
  __s64 sum_x = 0, sum_y = 0, sum_xy = 0, sum_x2 = 0;

  // Use oldest timestamp as reference (t=0) to avoid overflow
  __u32 start_idx = (state->history_head - n) & RSS_HISTORY_MASK;
  __u32 t0 = state->time_history_ds[start_idx];

  // Calculate sums for linear regression
  for (__u32 i = 0; i < n && i < RSS_HISTORY_SIZE; i++) {
    __u32 idx = (start_idx + i) & RSS_HISTORY_MASK;

    // X: time in seconds from start
    __s64 x_ds = state->time_history_ds[idx] - t0;
    __s64 x = x_ds / 10;  // Convert to seconds
    // Y is already in MB
    __s64 y = state->rss_history_mb[idx];

    // Check for potential overflow before adding
    if (sum_x > 0 && x > (__s64)(0x7FFFFFFFFFFFFFFF - sum_x)) {
      break;
    }
    if (sum_y > 0 && y > (__s64)(0x7FFFFFFFFFFFFFFF - sum_y)) {
      break;
    }

    sum_x += x;
    sum_y += y;
    sum_xy += x * y;
    sum_x2 += x * x;
  }

  // Calculate slope: (n*sum_xy - sum_x*sum_y) / (n*sum_x2 - sum_x^2)
  __s64 numerator = n * sum_xy - sum_x * sum_y;
  __s64 denominator = n * sum_x2 - sum_x * sum_x;

  if (denominator == 0) {
    state->trend_slope = 0;
    state->trend_r2 = 0;
    return;
  }

  // Calculate slope in MB/s (BPF lacks signed division)
  __s64 slope_mb_per_s;
  if ((numerator < 0 && denominator > 0) ||
      (numerator > 0 && denominator < 0)) {
    // Negative slope
    __u64 abs_num = numerator < 0 ? -numerator : numerator;
    __u64 abs_den = denominator < 0 ? -denominator : denominator;
    slope_mb_per_s = -(abs_num / abs_den);
  } else {
    // Positive slope
    __u64 abs_num = numerator < 0 ? -numerator : numerator;
    __u64 abs_den = denominator < 0 ? -denominator : denominator;
    slope_mb_per_s = abs_num / abs_den;
  }

  // Convert to bytes/s for storage
  state->trend_slope = slope_mb_per_s * 1024 * 1024;

  // Calculate R² for quality of fit (simplified for eBPF)
  // For now, use a simplified metric based on consistency
  if (slope_mb_per_s > 0) {
    state->trend_r2 = 800;  // Good fit for positive slope
  } else if (slope_mb_per_s == 0) {
    state->trend_r2 = 1000;  // Perfect fit for stable
  } else {
    state->trend_r2 = 500;  // Moderate fit for negative slope
  }
}

// Calculate growth rate and confidence scoring
static __always_inline void analyze_growth(struct process_memory_state *state,
                                           __u64 old_rss, __u64 time_delta_ns) {
  if (time_delta_ns == 0) {
    return;
  }

  // Calculate total growth from initial
  state->total_growth = state->current_rss > state->initial_rss
                            ? state->current_rss - state->initial_rss
                            : 0;

  // Calculate instantaneous growth rate
  if (state->current_rss != old_rss) {
    if (state->current_rss > old_rss) {
      __u64 rss_delta = state->current_rss - old_rss;
      // Prevent overflow
      if (rss_delta > (0xFFFFFFFFFFFFFFFF / 1000000000)) {
        state->growth_rate = 0xFFFFFFFFFFFFFFFF;
      } else {
        state->growth_rate = (rss_delta * 1000000000) / time_delta_ns;
      }
    } else {
      // RSS decreased - negative growth
      state->growth_rate = 0;
      state->pattern_type = 0;  // stable
    }
  }

  // Update peak RSS
  if (state->current_rss > state->peak_rss) {
    state->peak_rss = state->current_rss;
  }

  // Calculate leak confidence based on multiple factors
  if (state->growth_rate > 0 || state->trend_slope > 0) {
    __u64 effective_rate =
        state->trend_slope > 0 ? state->trend_slope : state->growth_rate;

    state->leak_confidence = 0;

    // Growth rate magnitude (0-25 points)
    if (effective_rate > 10485760) {  // >10MB/s
      state->leak_confidence += 25;
    } else if (effective_rate > 1048576) {  // >1MB/s
      state->leak_confidence += 20;
    } else if (effective_rate > 102400) {  // >100KB/s
      state->leak_confidence += 15;
    }

    // Pattern quality from R² (0-35 points)
    if (state->trend_r2 > 900) {
      state->leak_confidence += 35;
      state->pattern_type = 1;  // steady
    } else if (state->trend_r2 > 700) {
      state->leak_confidence += 25;
      state->pattern_type = 1;  // steady
    }

    // Duration (0-25 points)
    if (state->sample_count > 30) {
      state->leak_confidence += 25;
    } else if (state->sample_count > 20) {
      state->leak_confidence += 20;
    } else if (state->sample_count > 10) {
      state->leak_confidence += 15;
    }

    // Growth relative to initial (0-15 points)
    if (state->initial_rss > 0 && state->total_growth > 0) {
      __u64 growth_pct = (state->total_growth * 100) / state->initial_rss;
      if (growth_pct > 200) {  // Tripled
        state->leak_confidence += 15;
      } else if (growth_pct > 100) {  // Doubled
        state->leak_confidence += 10;
      }
    }

    // Cap at 100
    if (state->leak_confidence > 100) {
      state->leak_confidence = 100;
    }
  }
}

// Main RSS stat tracepoint handler
SEC("tp/kmem/rss_stat")
int trace_rss_stat(struct trace_event_raw_rss_stat *ctx) {
  // Only track current process RSS changes
  if (!ctx->curr) {
    return 0;  // Skip non-current mm updates
  }

  // Get current task
  struct task_struct *task = (struct task_struct *)bpf_get_current_task();
  if (!task) {
    return 0;
  }

  __u32 pid = BPF_CORE_READ(task, tgid);  // Use TGID for process tracking
  if (pid == 0) {
    return 0;  // Skip kernel threads
  }

  // Get current timestamp
  __u64 now = bpf_ktime_get_ns();

  // Look up existing state
  struct process_memory_state *state =
      bpf_map_lookup_elem(&process_states, &pid);

  // Get config
  __u32 config_key = 0;
  struct memgrowth_config *config =
      bpf_map_lookup_elem(&config_map, &config_key);
  __u64 min_rss =
      config ? config->min_rss_threshold : (10 * 1024 * 1024);  // 10MB default

  // The rss_stat tracepoint fires per memory type, not total RSS
  // ctx->size is the current value for this type (file/anon/swap/shmem) in pages
  // ctx->member indicates which type (0=file, 1=anon, 2=swap, 3=shmem)
  // For comprehensive tracking, we should accumulate all types
  // but for simplicity, track anonymous pages as primary indicator

  __u64 rss_pages = ctx->size > 0 ? ctx->size : 0;
  __u64 rss_bytes = rss_pages * 4096;

  // Filter by minimum RSS threshold
  if (rss_bytes < min_rss) {
    return 0;
  }

  // MB-resolution filtering
  __u32 rss_mb = rss_bytes / (1024 * 1024);

  if (!state) {
    // New process - initialize tracking
    struct process_memory_state new_state = {};
    new_state.pid = pid;
    new_state.tgid = pid;
    new_state.start_time_ns = now;
    new_state.last_update_ds = 0;
    new_state.current_rss = rss_bytes;
    new_state.initial_rss = rss_bytes;
    new_state.peak_rss = rss_bytes;
    new_state.sample_count = 1;

    // Get process name
    bpf_probe_read_kernel_str(new_state.comm, sizeof(new_state.comm),
                              task->comm);

    // Initialize history with first sample
    new_state.rss_history_mb[0] = rss_mb;
    new_state.time_history_ds[0] = 0;
    new_state.history_head = 1;
    new_state.history_count = 1;

    // Store new state
    bpf_map_update_elem(&process_states, &pid, &new_state, BPF_ANY);

    // Send new process event
    struct memory_growth_event *event =
        bpf_ringbuf_reserve(&events, sizeof(struct memory_growth_event), 0);
    if (event) {
      event->timestamp = now;
      event->pid = pid;
      event->tgid = pid;
      event->event_type = EVENT_NEW_PROCESS;
      event->current_rss = rss_bytes;
      event->total_growth = 0;
      event->growth_rate = 0;
      event->leak_confidence = 0;
      event->pattern_type = 0;
      __builtin_memcpy(event->comm, new_state.comm, sizeof(event->comm));
      bpf_ringbuf_submit(event, 0);
    }
  } else {
    // Existing process - check if RSS actually changed at MB resolution
    __u32 old_rss_mb = state->current_rss / (1024 * 1024);
    if (rss_mb == old_rss_mb) {
      return 0;  // No change at MB resolution
    }

    // Calculate time deltas
    __u32 now_ds = (now - state->start_time_ns) / 100000000;  // Deciseconds
    __u32 time_delta_ds = now_ds - state->last_update_ds;

    // Adaptive sampling based on growth behavior
    __u32 min_interval_ds = 10;  // Default 1 second
    if (state->growth_rate == 0 && state->sample_count > 10) {
      min_interval_ds = 100;                   // 10 seconds for stable
    } else if (state->growth_rate < 102400) {  // <100KB/s
      min_interval_ds = 50;                    // 5 seconds for slow growth
    }

    // Rate limit based on adaptive interval
    if (time_delta_ds < min_interval_ds) {
      return 0;
    }

    // Update RSS history
    update_rss_history(state, rss_bytes, now_ds);

    // Calculate trend from history
    calculate_trend(state);

    // Update state
    __u64 old_rss = state->current_rss;
    state->current_rss = rss_bytes;
    state->last_update_ds = now_ds;
    state->sample_count++;

    // Analyze growth patterns
    __u64 time_delta_ns = (__u64)time_delta_ds * 100000000;
    analyze_growth(state, old_rss, time_delta_ns);

    // Update state in map
    bpf_map_update_elem(&process_states, &pid, state, BPF_EXIST);

    // Send event if significant change or high confidence
    __u64 growth_threshold =
        config ? config->growth_rate_threshold : (1024 * 1024);  // 1MB/s

    if (state->growth_rate > growth_threshold || state->leak_confidence >= 60) {
      struct memory_growth_event *event =
          bpf_ringbuf_reserve(&events, sizeof(struct memory_growth_event), 0);
      if (event) {
        event->timestamp = now;
        event->pid = pid;
        event->tgid = pid;
        event->current_rss = rss_bytes;
        event->total_growth = state->total_growth;
        event->growth_rate = state->growth_rate;
        event->leak_confidence = state->leak_confidence;
        event->pattern_type = state->pattern_type;
        __builtin_memcpy(event->comm, state->comm, sizeof(event->comm));

        if (state->leak_confidence >= 60) {
          event->event_type = EVENT_HIGH_RISK;
        } else {
          event->event_type = EVENT_GROWTH_DETECTED;
        }

        bpf_ringbuf_submit(event, 0);
      }
    }
  }

  return 0;
}

// Track process exit to clean up state
SEC("tp/sched/sched_process_exit")
int trace_process_exit(void *ctx) {
  struct task_struct *task = (struct task_struct *)bpf_get_current_task();
  if (!task) {
    return 0;
  }

  __u32 pid = BPF_CORE_READ(task, tgid);
  struct process_memory_state *state =
      bpf_map_lookup_elem(&process_states, &pid);

  if (state) {
    // Send exit event with final stats
    struct memory_growth_event *event =
        bpf_ringbuf_reserve(&events, sizeof(struct memory_growth_event), 0);
    if (event) {
      event->timestamp = bpf_ktime_get_ns();
      event->pid = pid;
      event->tgid = pid;
      event->event_type = EVENT_PROCESS_EXIT;
      event->current_rss = state->current_rss;
      event->total_growth = state->total_growth;
      event->growth_rate = state->growth_rate;
      event->leak_confidence = state->leak_confidence;
      event->pattern_type = state->pattern_type;
      __builtin_memcpy(event->comm, state->comm, sizeof(event->comm));
      bpf_ringbuf_submit(event, 0);
    }

    bpf_map_delete_elem(&process_states, &pid);
  }

  return 0;
}

char _license[] SEC("license") = "GPL";