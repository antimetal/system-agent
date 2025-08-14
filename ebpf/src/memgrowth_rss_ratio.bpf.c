// SPDX-License-Identifier: GPL-2.0-only
/* Copyright (c) 2024 Antimetal, Inc. */

// Enhanced RSS ratio-based memory growth monitor
// Tracks anonymous vs file-backed memory ratios for accurate leak detection

#include "vmlinux.h"

#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

#include "memgrowth_types.h"

// Memory type constants are defined in vmlinux.h
// MM_FILEPAGES (0), MM_ANONPAGES (1), MM_SWAPENTS (2), MM_SHMEMPAGES (3)

// BPF Maps
struct {
  __uint(type, BPF_MAP_TYPE_HASH);
  __uint(max_entries, 10000);
  __type(key, __u32);  // PID
  __type(value, struct process_memory_state);
} process_states SEC(".maps");

struct {
  __uint(type, BPF_MAP_TYPE_RINGBUF);
  __uint(max_entries, 512 * 1024);  // 512KB ring buffer
} events SEC(".maps");

struct {
  __uint(type, BPF_MAP_TYPE_ARRAY);
  __uint(max_entries, 1);
  __type(key, __u32);
  __type(value, struct memgrowth_config);
} config_map SEC(".maps");

// Calculate memory ratios
static __always_inline void calculate_ratios(struct process_memory_state *state) {
  __u64 total = state->rss_anon + state->rss_file + state->rss_shmem;
  
  if (total == 0) {
    state->anon_ratio = 0;
    state->swap_ratio = 0;
    return;
  }
  
  // Calculate anonymous ratio (% * 10 for precision)
  state->anon_ratio = (state->rss_anon * 1000) / total;
  
  // Calculate swap pressure ratio
  if (state->rss_swap > 0) {
    // Swap ratio relative to total RSS
    state->swap_ratio = (state->rss_swap * 1000) / (total + state->rss_swap);
  } else {
    state->swap_ratio = 0;
  }
}

// Calculate growth rates by memory type
static __always_inline void calculate_type_growth_rates(
    struct process_memory_state *state,
    __u64 old_anon, __u64 old_file,
    __u64 time_delta_ns) {
  
  if (time_delta_ns == 0) {
    return;
  }
  
  // Anonymous memory growth rate
  if (state->rss_anon != old_anon) {
    __s64 anon_delta = (__s64)(state->rss_anon - old_anon);
    // Calculate rate with overflow protection
    if (anon_delta > 0) {
      __u64 abs_delta = anon_delta;
      if (abs_delta > (0x7FFFFFFFFFFFFFFF / 1000000000)) {
        state->anon_growth_rate = 0x7FFFFFFFFFFFFFFF;
      } else {
        state->anon_growth_rate = (abs_delta * 1000000000) / time_delta_ns;
      }
    } else {
      __u64 abs_delta = -anon_delta;
      if (abs_delta > (0x7FFFFFFFFFFFFFFF / 1000000000)) {
        state->anon_growth_rate = -0x7FFFFFFFFFFFFFFF;
      } else {
        state->anon_growth_rate = -((abs_delta * 1000000000) / time_delta_ns);
      }
    }
  } else {
    state->anon_growth_rate = 0;
  }
  
  // File memory growth rate
  if (state->rss_file != old_file) {
    __s64 file_delta = (__s64)(state->rss_file - old_file);
    if (file_delta > 0) {
      __u64 abs_delta = file_delta;
      if (abs_delta > (0x7FFFFFFFFFFFFFFF / 1000000000)) {
        state->file_growth_rate = 0x7FFFFFFFFFFFFFFF;
      } else {
        state->file_growth_rate = (abs_delta * 1000000000) / time_delta_ns;
      }
    } else {
      __u64 abs_delta = -file_delta;
      if (abs_delta > (0x7FFFFFFFFFFFFFFF / 1000000000)) {
        state->file_growth_rate = -0x7FFFFFFFFFFFFFFF;
      } else {
        state->file_growth_rate = -((abs_delta * 1000000000) / time_delta_ns);
      }
    }
  } else {
    state->file_growth_rate = 0;
  }
}

// Enhanced leak confidence calculation using ratios
static __always_inline void calculate_ratio_based_confidence(
    struct process_memory_state *state) {
  
  state->leak_confidence = 0;
  
  // Factor 1: High anonymous memory ratio (0-30 points)
  // Heap leaks show up as anonymous memory
  if (state->anon_ratio > 900) {  // >90% anonymous
    state->leak_confidence += 30;
  } else if (state->anon_ratio > 800) {  // >80% anonymous
    state->leak_confidence += 25;
  } else if (state->anon_ratio > 700) {  // >70% anonymous
    state->leak_confidence += 20;
  } else if (state->anon_ratio > 600) {  // >60% anonymous
    state->leak_confidence += 15;
  }
  
  // Factor 2: Anonymous growing faster than file (0-35 points)
  // This is the strongest indicator of a heap leak
  if (state->anon_growth_rate > 0 && state->file_growth_rate <= 0) {
    // Anon growing, file stable or shrinking
    state->leak_confidence += 35;
  } else if (state->anon_growth_rate > state->file_growth_rate * 2) {
    // Anon growing 2x faster than file
    state->leak_confidence += 30;
  } else if (state->anon_growth_rate > state->file_growth_rate) {
    // Anon growing faster than file
    state->leak_confidence += 20;
  }
  
  // Factor 3: Swap pressure (0-20 points)
  // Memory leaks often cause swap usage
  if (state->swap_ratio > 100) {  // >10% in swap
    state->leak_confidence += 20;
  } else if (state->swap_ratio > 50) {  // >5% in swap
    state->leak_confidence += 15;
  } else if (state->swap_ratio > 10) {  // >1% in swap
    state->leak_confidence += 10;
  }
  
  // Factor 4: Anonymous growth magnitude (0-15 points)
  if (state->initial_anon > 0 && state->rss_anon > state->initial_anon) {
    __u64 anon_growth = state->rss_anon - state->initial_anon;
    __u64 growth_pct = (anon_growth * 100) / state->initial_anon;
    
    if (growth_pct > 500) {  // 5x growth
      state->leak_confidence += 15;
    } else if (growth_pct > 200) {  // 2x growth
      state->leak_confidence += 10;
    } else if (growth_pct > 100) {  // Doubled
      state->leak_confidence += 5;
    }
  }
  
  // Cap at 100
  if (state->leak_confidence > 100) {
    state->leak_confidence = 100;
  }
  
  // Determine pattern type based on ratios
  if (state->anon_growth_rate > 0 && state->anon_ratio > 700) {
    state->pattern_type = 1;  // Steady leak pattern
  } else if (state->swap_ratio > 100) {
    state->pattern_type = 2;  // Memory pressure pattern
  } else {
    state->pattern_type = 0;  // Stable
  }
}

// Main RSS stat tracepoint handler
SEC("tp/kmem/rss_stat")
int trace_rss_stat(struct trace_event_raw_rss_stat *ctx) {
  // Only track current process RSS changes
  if (!ctx->curr) {
    return 0;
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
  
  // Get memory type and size from tracepoint
  int member = ctx->member;
  __u64 size_pages = ctx->size > 0 ? ctx->size : 0;
  __u64 size_bytes = size_pages * 4096;
  
  // Look up or create process state
  struct process_memory_state *state = bpf_map_lookup_elem(&process_states, &pid);
  
  if (!state) {
    // New process - initialize
    struct process_memory_state new_state = {};
    new_state.pid = pid;
    new_state.tgid = pid;
    new_state.start_time_ns = bpf_ktime_get_ns();
    new_state.last_update_ds = 0;
    
    // Initialize based on memory type
    switch (member) {
      case MM_ANONPAGES:
        new_state.rss_anon = size_bytes;
        new_state.initial_anon = size_bytes;
        break;
      case MM_FILEPAGES:
        new_state.rss_file = size_bytes;
        new_state.initial_file = size_bytes;
        break;
      case MM_SWAPENTS:
        new_state.rss_swap = size_bytes;
        break;
      case MM_SHMEMPAGES:
        new_state.rss_shmem = size_bytes;
        break;
    }
    
    new_state.current_rss = size_bytes;
    new_state.initial_rss = size_bytes;
    new_state.peak_rss = size_bytes;
    new_state.sample_count = 1;
    
    // Get process name
    bpf_probe_read_kernel_str(new_state.comm, sizeof(new_state.comm), task->comm);
    
    // Calculate initial ratios
    calculate_ratios(&new_state);
    
    bpf_map_update_elem(&process_states, &pid, &new_state, BPF_ANY);
    
    // Send new process event if significant
    if (size_bytes > (10 * 1024 * 1024)) {  // >10MB
      struct memory_growth_event *event = 
          bpf_ringbuf_reserve(&events, sizeof(struct memory_growth_event), 0);
      if (event) {
        event->timestamp = new_state.start_time_ns;
        event->pid = pid;
        event->tgid = pid;
        event->event_type = EVENT_NEW_PROCESS;
        event->current_rss = size_bytes;
        event->rss_anon = new_state.rss_anon;
        event->rss_file = new_state.rss_file;
        event->rss_swap = new_state.rss_swap;
        event->anon_ratio = new_state.anon_ratio;
        event->swap_ratio = new_state.swap_ratio;
        __builtin_memcpy(event->comm, new_state.comm, sizeof(event->comm));
        bpf_ringbuf_submit(event, 0);
      }
    }
  } else {
    // Existing process - update the appropriate memory type
    __u64 old_anon = state->rss_anon;
    __u64 old_file = state->rss_file;
    __u64 old_total = state->current_rss;
    
    // Update the specific memory type
    switch (member) {
      case MM_ANONPAGES:
        state->rss_anon = size_bytes;
        if (state->initial_anon == 0) {
          state->initial_anon = size_bytes;
        }
        break;
      case MM_FILEPAGES:
        state->rss_file = size_bytes;
        if (state->initial_file == 0) {
          state->initial_file = size_bytes;
        }
        break;
      case MM_SWAPENTS:
        state->rss_swap = size_bytes;
        break;
      case MM_SHMEMPAGES:
        state->rss_shmem = size_bytes;
        break;
    }
    
    // Recalculate total RSS
    state->current_rss = state->rss_anon + state->rss_file + state->rss_shmem;
    
    // Update peak
    if (state->current_rss > state->peak_rss) {
      state->peak_rss = state->current_rss;
    }
    
    // Calculate time delta
    __u64 now = bpf_ktime_get_ns();
    __u32 now_ds = (now - state->start_time_ns) / 100000000;
    __u32 time_delta_ds = now_ds - state->last_update_ds;
    
    // Rate limit updates (minimum 1 second between updates)
    if (time_delta_ds < 10) {
      return 0;
    }
    
    // Update timing
    state->last_update_ds = now_ds;
    state->sample_count++;
    
    // Calculate growth metrics
    __u64 time_delta_ns = (__u64)time_delta_ds * 100000000;
    calculate_type_growth_rates(state, old_anon, old_file, time_delta_ns);
    
    // Update total growth
    if (state->current_rss > state->initial_rss) {
      state->total_growth = state->current_rss - state->initial_rss;
    } else {
      state->total_growth = 0;
    }
    
    // Calculate overall growth rate
    if (state->current_rss != old_total && time_delta_ns > 0) {
      if (state->current_rss > old_total) {
        __u64 delta = state->current_rss - old_total;
        state->growth_rate = (delta * 1000000000) / time_delta_ns;
      } else {
        state->growth_rate = 0;
      }
    }
    
    // Calculate ratios and confidence
    calculate_ratios(state);
    calculate_ratio_based_confidence(state);
    
    // Update state in map
    bpf_map_update_elem(&process_states, &pid, state, BPF_EXIST);
    
    // Get config for thresholds
    __u32 config_key = 0;
    struct memgrowth_config *config = bpf_map_lookup_elem(&config_map, &config_key);
    __u64 min_rss = config ? config->min_rss_threshold : (10 * 1024 * 1024);
    
    // Skip if below minimum threshold
    if (state->current_rss < min_rss) {
      return 0;
    }
    
    // Send event if high confidence or significant change
    bool send_event = false;
    __u8 event_type = EVENT_GROWTH_DETECTED;
    
    if (state->leak_confidence >= 60) {
      send_event = true;
      event_type = EVENT_HIGH_RISK;
    } else if (state->anon_growth_rate > (1024 * 1024)) {  // >1MB/s anon growth
      send_event = true;
    } else if (state->swap_ratio > 100) {  // Significant swap usage
      send_event = true;
      event_type = EVENT_OOM_WARNING;
    }
    
    if (send_event) {
      struct memory_growth_event *event = 
          bpf_ringbuf_reserve(&events, sizeof(struct memory_growth_event), 0);
      if (event) {
        event->timestamp = now;
        event->pid = pid;
        event->tgid = pid;
        event->event_type = event_type;
        event->current_rss = state->current_rss;
        event->total_growth = state->total_growth;
        event->growth_rate = state->growth_rate;
        event->leak_confidence = state->leak_confidence;
        event->pattern_type = state->pattern_type;
        
        // Include RSS breakdown
        event->rss_anon = state->rss_anon;
        event->rss_file = state->rss_file;
        event->rss_swap = state->rss_swap;
        event->anon_ratio = state->anon_ratio;
        event->swap_ratio = state->swap_ratio;
        
        __builtin_memcpy(event->comm, state->comm, sizeof(event->comm));
        bpf_ringbuf_submit(event, 0);
      }
    }
  }
  
  return 0;
}

// Process exit cleanup
SEC("tp/sched/sched_process_exit")
int trace_process_exit(void *ctx) {
  struct task_struct *task = (struct task_struct *)bpf_get_current_task();
  if (!task) {
    return 0;
  }
  
  __u32 pid = BPF_CORE_READ(task, tgid);
  struct process_memory_state *state = bpf_map_lookup_elem(&process_states, &pid);
  
  if (state) {
    // Send final stats if process had significant memory usage
    if (state->peak_rss > (50 * 1024 * 1024)) {  // >50MB peak
      struct memory_growth_event *event = 
          bpf_ringbuf_reserve(&events, sizeof(struct memory_growth_event), 0);
      if (event) {
        event->timestamp = bpf_ktime_get_ns();
        event->pid = pid;
        event->tgid = pid;
        event->event_type = EVENT_PROCESS_EXIT;
        event->current_rss = state->current_rss;
        event->total_growth = state->total_growth;
        event->leak_confidence = state->leak_confidence;
        event->pattern_type = state->pattern_type;
        
        // Final RSS breakdown
        event->rss_anon = state->rss_anon;
        event->rss_file = state->rss_file;
        event->rss_swap = state->rss_swap;
        event->anon_ratio = state->anon_ratio;
        event->swap_ratio = state->swap_ratio;
        
        __builtin_memcpy(event->comm, state->comm, sizeof(event->comm));
        bpf_ringbuf_submit(event, 0);
      }
    }
    
    bpf_map_delete_elem(&process_states, &pid);
  }
  
  return 0;
}

char _license[] SEC("license") = "GPL";