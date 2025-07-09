// SPDX-License-Identifier: GPL-2.0-only
/*
 * eBPF Hello World Program
 * 
 * This program demonstrates basic eBPF functionality by tracing the
 * sys_enter_open syscall and logging filename information.
 *
 * Copyright (C) 2025 Antimetal, Inc.
 */

#include <linux/types.h>
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define MAX_FILENAME_LEN 256

char LICENSE[] SEC("license") = "GPL";

struct event {
    __u32 pid;
    __u32 uid;
    char filename[MAX_FILENAME_LEN];
};

struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(int));
    __uint(value_size, sizeof(int));
} events SEC(".maps");

// Simple struct for tracepoint context
struct trace_event_raw_sys_enter {
    __u64 args[6];
};

SEC("tracepoint/syscalls/sys_enter_openat")
int trace_openat(struct trace_event_raw_sys_enter *ctx)
{
    struct event e = {};
    
    // Get current PID and UID
    __u64 pid_tgid = bpf_get_current_pid_tgid();
    __u64 uid_gid = bpf_get_current_uid_gid();
    
    e.pid = pid_tgid >> 32;
    e.uid = uid_gid & 0xFFFFFFFF;
    
    // Read filename from syscall arguments
    const char *filename = (const char *)ctx->args[1];
    bpf_probe_read_user_str(&e.filename, sizeof(e.filename), filename);
    
    // Submit event to userspace
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &e, sizeof(e));
    
    return 0;
}