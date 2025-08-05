// SPDX-License-Identifier: GPL-2.0
/* Test program for CO-RE (Compile Once - Run Everywhere) relocations
 * This program tests various CO-RE features to ensure they work across
 * different kernel versions.
 */

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

char LICENSE[] SEC("license") = "GPL";

/* Test basic field access with CO-RE */
SEC("tracepoint/syscalls/sys_enter_open")
int test_core_field_access(void *ctx)
{
    struct task_struct *task = (void *)bpf_get_current_task();
    
    /* Test 1: Basic field access */
    pid_t pid = BPF_CORE_READ(task, pid);
    
    /* Test 2: Nested field access */
    pid_t tgid = BPF_CORE_READ(task, tgid);
    
    /* Test 3: Field existence check */
    if (bpf_core_field_exists(task->nsproxy)) {
        /* This field exists in newer kernels */
        void *nsproxy = BPF_CORE_READ(task, nsproxy);
        /* Use nsproxy to avoid unused variable warning */
        if (nsproxy) {
            /* Field exists and is not NULL */
        }
    }
    
    /* Test 4: Simple field read that should work on all kernels */
    int prio = BPF_CORE_READ(task, prio);
    
    /* Use the variables to avoid compiler warnings */
    if (pid > 0 && tgid > 0 && prio >= 0) {
        /* All fields read successfully */
    }
    
    return 0;
}

/* Test CO-RE with different map types */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, u32);
    __type(value, u64);
} test_hash SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 10);
    __type(key, u32);
    __type(value, u64);
} test_array SEC(".maps");

/* Test program that uses maps */
SEC("kprobe/sys_open")
int test_core_maps(struct pt_regs *ctx)
{
    u32 key = 0;
    u64 val = 1;
    
    /* Test map operations */
    bpf_map_update_elem(&test_hash, &key, &val, BPF_ANY);
    
    u64 *array_val = bpf_map_lookup_elem(&test_array, &key);
    if (array_val) {
        *array_val = val;
    }
    
    return 0;
}

/* Test kernel version specific features */
SEC("raw_tracepoint/sys_enter")
int test_kernel_features(void *ctx)
{
    /* Test helpers that may not be available on all kernels */
    u64 time = bpf_ktime_get_ns();
    
    /* This will be relocated properly by CO-RE */
    struct task_struct *task = (void *)bpf_get_current_task();
    
    /* Test reading comm field which should exist in all versions */
    char comm[16];
    bpf_probe_read_kernel_str(comm, sizeof(comm), &task->comm);
    
    /* Use time variable to avoid warning */
    if (time > 0) {
        /* Time is valid */
    }
    
    return 0;
}