//go:build ignore

// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright (c) 2024 Antimetal

#include <linux/bpf.h>
#include <linux/types.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include "../headers/performance.h"

// Ring buffer for sending scheduling events to userspace
DEFINE_PERF_RINGBUF(sched_events, 8192);

// Per-CPU stats tracking
DEFINE_PERF_PERCPU_ARRAY(cpu_stats, struct cpu_stat, 1);

// Per-process CPU time tracking
DEFINE_PERF_HASH(process_cpu_time, __u32, struct process_stat, 10240);

// Track scheduling switches
SEC("tp_btf/sched_switch")
int trace_sched_switch(u64 *ctx) {
    struct task_struct *prev = (struct task_struct *)ctx[1];
    struct task_struct *next = (struct task_struct *)ctx[2];
    struct sched_event *e;
    
    // Reserve space in ring buffer
    e = bpf_ringbuf_reserve(&sched_events, sizeof(*e), 0);
    if (!e) {
        return 0;
    }
    
    // Fill event data
    e->timestamp = get_timestamp_ns();
    e->cpu = get_cpu();
    
    // Previous task info
    e->prev_pid = BPF_CORE_READ(prev, pid);
    e->prev_tgid = BPF_CORE_READ(prev, tgid);
    bpf_probe_read_kernel_str(&e->prev_comm, sizeof(e->prev_comm), &prev->comm);
    e->prev_state = BPF_CORE_READ(prev, __state);
    
    // Next task info  
    e->next_pid = BPF_CORE_READ(next, pid);
    e->next_tgid = BPF_CORE_READ(next, tgid);
    bpf_probe_read_kernel_str(&e->next_comm, sizeof(e->next_comm), &next->comm);
    
    // Submit event
    bpf_ringbuf_submit(e, 0);
    
    // Update per-process CPU time tracking
    struct process_stat *pstat;
    __u32 pid = e->prev_pid;
    
    pstat = bpf_map_lookup_elem(&process_cpu_time, &pid);
    if (pstat) {
        __u64 now = e->timestamp;
        __u64 delta = now - pstat->last_seen_ns;
        pstat->cpu_time_ns += delta;
        pstat->last_seen_ns = now;
    } else {
        // New process, create entry
        struct process_stat new_pstat = {};
        new_pstat.pid = pid;
        new_pstat.tgid = e->prev_tgid;
        new_pstat.last_seen_ns = e->timestamp;
        new_pstat.cpu_time_ns = 0;
        bpf_probe_read_kernel_str(&new_pstat.comm, sizeof(new_pstat.comm), &prev->comm);
        
        bpf_map_update_elem(&process_cpu_time, &pid, &new_pstat, BPF_ANY);
    }
    
    // Update tracking for next process
    pid = e->next_pid;
    pstat = bpf_map_lookup_elem(&process_cpu_time, &pid);
    if (pstat) {
        pstat->last_seen_ns = e->timestamp;
    } else {
        struct process_stat new_pstat = {};
        new_pstat.pid = pid;
        new_pstat.tgid = e->next_tgid;
        new_pstat.last_seen_ns = e->timestamp;
        new_pstat.cpu_time_ns = 0;
        bpf_probe_read_kernel_str(&new_pstat.comm, sizeof(new_pstat.comm), &next->comm);
        
        bpf_map_update_elem(&process_cpu_time, &pid, &new_pstat, BPF_ANY);
    }
    
    return 0;
}

// Track process creation
SEC("tp_btf/task_newtask")
int trace_task_newtask(u64 *ctx) {
    struct task_struct *task = (struct task_struct *)ctx[0];
    struct process_event *e;
    
    e = bpf_ringbuf_reserve(&sched_events, sizeof(*e), 0);
    if (!e) {
        return 0;
    }
    
    e->timestamp = get_timestamp_ns();
    e->cpu = get_cpu();
    e->event_type = PERF_EVENT_PROCESS_FORK;
    
    e->pid = BPF_CORE_READ(task, pid);
    e->tgid = BPF_CORE_READ(task, tgid);
    e->ppid = BPF_CORE_READ(task, real_parent, pid);
    
    bpf_probe_read_kernel_str(&e->comm, sizeof(e->comm), &task->comm);
    
    bpf_ringbuf_submit(e, 0);
    
    return 0;
}

// Track process exit
SEC("tp_btf/sched_process_exit") 
int trace_sched_process_exit(u64 *ctx) {
    struct task_struct *task = (struct task_struct *)ctx[0];
    struct process_event *e;
    
    e = bpf_ringbuf_reserve(&sched_events, sizeof(*e), 0);
    if (!e) {
        return 0;
    }
    
    e->timestamp = get_timestamp_ns();
    e->cpu = get_cpu();
    e->event_type = PERF_EVENT_PROCESS_EXIT;
    
    e->pid = BPF_CORE_READ(task, pid);
    e->tgid = BPF_CORE_READ(task, tgid);
    e->ppid = BPF_CORE_READ(task, real_parent, pid);
    e->exit_code = BPF_CORE_READ(task, exit_code);
    
    bpf_probe_read_kernel_str(&e->comm, sizeof(e->comm), &task->comm);
    
    bpf_ringbuf_submit(e, 0);
    
    // Clean up process tracking
    __u32 pid = e->pid;
    bpf_map_delete_elem(&process_cpu_time, &pid);
    
    return 0;
}

char LICENSE[] SEC("license") = "Dual BSD/GPL";