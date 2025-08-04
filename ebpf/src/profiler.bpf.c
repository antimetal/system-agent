// SPDX-License-Identifier: GPL-2.0-only
#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include "../include/profiler_types.h"

char LICENSE[] SEC("license") = "GPL";

// Map to store stack traces
struct {
    __uint(type, BPF_MAP_TYPE_STACK_TRACE);
    __uint(key_size, sizeof(u32));
    __uint(value_size, MAX_STACK_DEPTH * sizeof(u64));
    __uint(max_entries, 10000);
} stack_traces SEC(".maps");

// Map to count stack occurrences
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(key_size, sizeof(struct stack_key));
    __uint(value_size, sizeof(struct stack_count));
    __uint(max_entries, 10000);
} stack_counts SEC(".maps");

// Perf event handler
SEC("perf_event")
int profile(struct bpf_perf_event_data *ctx)
{
    struct task_struct *task;
    struct stack_key key = {};
    struct stack_count *val, zero = {};
    u64 stack_flags = 0;
    
    // Get current task
    task = (struct task_struct *)bpf_get_current_task();
    
    // Get PID and TID
    key.pid = BPF_CORE_READ(task, tgid);
    key.tid = BPF_CORE_READ(task, pid);
    
    // Skip kernel threads (PID 0)
    if (key.pid == 0)
        return 0;
    
    // Get stack IDs
    stack_flags = BPF_F_USER_STACK;
    key.user_stack_id = bpf_get_stackid(ctx, &stack_traces, stack_flags);
    
    stack_flags = 0; // Kernel stack
    key.kernel_stack_id = bpf_get_stackid(ctx, &stack_traces, stack_flags);
    
    // Update count
    val = bpf_map_lookup_elem(&stack_counts, &key);
    if (val) {
        val->count++;
    } else {
        // New stack trace
        zero.count = 1;
        zero.cpu = bpf_get_smp_processor_id();
        zero.padding = 0;
        bpf_map_update_elem(&stack_counts, &key, &zero, BPF_NOEXIST);
    }
    
    return 0;
}