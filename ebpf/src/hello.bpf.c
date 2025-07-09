// SPDX-License-Identifier: GPL-2.0-only
/*
 * eBPF Hello World Program
 * 
 * This program demonstrates basic eBPF functionality by tracing the
 * sys_enter_openat syscall and logging filename information.
 *
 * Copyright (C) 2025 Antimetal, Inc.
 */

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_FILENAME_LEN 256

char LICENSE[] SEC("license") = "GPL";

// Custom event structure for userspace communication
struct event {
    u32 pid;
    u32 uid;
    char filename[MAX_FILENAME_LEN];
};

// Ring buffer for sending events to userspace
// More efficient than perf events, with better memory usage
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256 KB buffer
} events SEC(".maps");

SEC("tracepoint/syscalls/sys_enter_openat")
int trace_openat(struct trace_event_raw_sys_enter *ctx)
{
    // Reserve space in ring buffer first
    struct event *e;
    e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;
    
    // Get current PID and UID
    u64 pid_tgid = bpf_get_current_pid_tgid();
    u64 uid_gid = bpf_get_current_uid_gid();
    
    e->pid = pid_tgid >> 32;
    e->uid = uid_gid & 0xFFFFFFFF;
    
    // Read filename from syscall arguments
    // sys_enter_openat args: dfd, filename, flags, mode
    const char *filename = (const char *)ctx->args[1];
    bpf_probe_read_user_str(&e->filename, sizeof(e->filename), filename);
    
    // Submit the event
    bpf_ringbuf_submit(e, 0);
    
    return 0;
}