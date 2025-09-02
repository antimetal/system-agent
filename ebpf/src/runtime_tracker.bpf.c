// SPDX-License-Identifier: GPL-2.0-only
#include "vmlinux.h"

#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

#include "../include/common.h"
#include "../include/runtime_types.h"

char LICENSE[] SEC("license") = "GPL";

// Ring buffer for sending events to userspace
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 8 << 20);  // 8 MB
} events SEC(".maps");

// Per-CPU array to store large event data (avoids stack overflow)
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, struct runtime_event);
} heap SEC(".maps");

// Helper to get current timestamp
static __always_inline __u64 get_timestamp() {
    return bpf_ktime_get_ns();
}

// Helper to get current task struct
static __always_inline struct task_struct *get_current_task() {
    return (struct task_struct *)bpf_get_current_task();
}

// Helper to send event to ring buffer
static __always_inline int send_event(struct runtime_event *event) {
    struct runtime_event *e;
    
    e = bpf_ringbuf_reserve(&events, sizeof(struct runtime_event), 0);
    if (!e)
        return -1;
    
    // Copy fields individually to avoid memcpy
    e->type = event->type;
    e->timestamp = event->timestamp;
    e->pid = event->pid;
    e->ppid = event->ppid;
    e->uid = event->uid;
    e->gid = event->gid;
    e->cgroup_id = event->cgroup_id;
    
    // Copy arrays using loops
    #pragma unroll
    for (int i = 0; i < MAX_COMM_LEN; i++) {
        e->comm[i] = event->comm[i];
    }
    
    #pragma unroll
    for (int i = 0; i < MAX_FILENAME_LEN; i++) {
        e->filename[i] = event->filename[i];
    }
    
    e->args_size = event->args_size;
    
    // Copy args - do it in chunks to satisfy verifier
    #pragma unroll
    for (int i = 0; i < 512; i++) {
        if (i < event->args_size && i < MAX_ARGS_SIZE)
            e->args[i] = event->args[i];
    }
    
    bpf_ringbuf_submit(e, 0);
    return 0;
}

// Tracepoint for process fork
SEC("tracepoint/sched/sched_process_fork")
int tracepoint__sched__sched_process_fork(struct trace_event_raw_sched_process_fork *ctx) {
    u32 zero = 0;
    struct runtime_event *event;
    struct task_struct *parent;
    
    event = bpf_map_lookup_elem(&heap, &zero);
    if (!event)
        return 0;
    
    event->type = RUNTIME_EVENT_PROCESS_FORK;
    event->timestamp = get_timestamp();
    
    // Get parent and child PIDs from context
    parent = (struct task_struct *)bpf_get_current_task();
    event->ppid = BPF_CORE_READ(parent, tgid);
    event->pid = ctx->child_pid;
    
    // Get UID/GID
    event->uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    event->gid = bpf_get_current_uid_gid() >> 32;
    
    // Get comm from parent (child will inherit it initially)
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Get cgroup ID
    event->cgroup_id = bpf_get_current_cgroup_id();
    
    send_event(event);
    return 0;
}

// Tracepoint for process exec
SEC("tracepoint/syscalls/sys_enter_execve")
int tracepoint__syscalls__sys_enter_execve(struct trace_event_raw_sys_enter *ctx) {
    u32 zero = 0;
    struct runtime_event *event;
    struct task_struct *task;
    const char *filename_ptr;
    const char **argv;
    
    event = bpf_map_lookup_elem(&heap, &zero);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    
    event->type = RUNTIME_EVENT_PROCESS_EXEC;
    event->timestamp = get_timestamp();
    
    task = get_current_task();
    event->pid = BPF_CORE_READ(task, tgid);
    event->ppid = BPF_CORE_READ(task, real_parent, tgid);
    
    // Get UID/GID
    event->uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    event->gid = bpf_get_current_uid_gid() >> 32;
    
    // Get comm (will be updated after exec completes)
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Get cgroup ID
    event->cgroup_id = bpf_get_current_cgroup_id();
    
    // Read filename
    filename_ptr = (const char *)ctx->args[0];
    bpf_probe_read_user_str(&event->filename, sizeof(event->filename), filename_ptr);
    
    // Read up to 5 arguments (simplified for verifier)
    argv = (const char **)ctx->args[1];
    __u32 args_offset = 0;
    
    // Unroll manually for verifier
    const char *arg_ptr;
    int len;
    
    // Arg 0
    if (bpf_probe_read_user(&arg_ptr, sizeof(arg_ptr), &argv[0]) == 0 && arg_ptr) {
        len = bpf_probe_read_user_str(&event->args[args_offset], 
                                      MAX_ARGS_SIZE - args_offset, arg_ptr);
        if (len > 0 && args_offset + len < MAX_ARGS_SIZE)
            args_offset += len;
    }
    
    // Arg 1
    if (args_offset < MAX_ARGS_SIZE - 64) {
        if (bpf_probe_read_user(&arg_ptr, sizeof(arg_ptr), &argv[1]) == 0 && arg_ptr) {
            len = bpf_probe_read_user_str(&event->args[args_offset], 
                                          MAX_ARGS_SIZE - args_offset, arg_ptr);
            if (len > 0 && args_offset + len < MAX_ARGS_SIZE)
                args_offset += len;
        }
    }
    
    // Arg 2
    if (args_offset < MAX_ARGS_SIZE - 64) {
        if (bpf_probe_read_user(&arg_ptr, sizeof(arg_ptr), &argv[2]) == 0 && arg_ptr) {
            len = bpf_probe_read_user_str(&event->args[args_offset], 
                                          MAX_ARGS_SIZE - args_offset, arg_ptr);
            if (len > 0 && args_offset + len < MAX_ARGS_SIZE)
                args_offset += len;
        }
    }
    
    event->args_size = args_offset;
    
    send_event(event);
    return 0;
}

// Tracepoint for process exit
SEC("tracepoint/sched/sched_process_exit")
int tracepoint__sched__sched_process_exit(struct trace_event_raw_sched_process_template *ctx) {
    u32 zero = 0;
    struct runtime_event *event;
    struct task_struct *task;
    
    event = bpf_map_lookup_elem(&heap, &zero);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    
    event->type = RUNTIME_EVENT_PROCESS_EXIT;
    event->timestamp = get_timestamp();
    
    task = get_current_task();
    event->pid = BPF_CORE_READ(task, tgid);
    event->ppid = BPF_CORE_READ(task, real_parent, tgid);
    
    // Get UID/GID
    event->uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    event->gid = bpf_get_current_uid_gid() >> 32;
    
    // Get comm
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Get cgroup ID
    event->cgroup_id = bpf_get_current_cgroup_id();
    
    send_event(event);
    return 0;
}

// Kprobe for cgroup creation (container start)
SEC("kprobe/cgroup_mkdir")
int kprobe__cgroup_mkdir(struct pt_regs *ctx) {
    u32 zero = 0;
    struct runtime_event *event;
    struct task_struct *task;
    
    event = bpf_map_lookup_elem(&heap, &zero);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    
    event->type = RUNTIME_EVENT_CGROUP_CREATE;
    event->timestamp = get_timestamp();
    
    task = get_current_task();
    event->pid = BPF_CORE_READ(task, tgid);
    event->ppid = BPF_CORE_READ(task, real_parent, tgid);
    
    // Get UID/GID of the process creating the cgroup
    event->uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    event->gid = bpf_get_current_uid_gid() >> 32;
    
    // Get comm of creating process (usually containerd, dockerd, etc.)
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Get the new cgroup ID if possible
    // Note: This requires kernel 5.7+ for bpf_get_current_cgroup_id
    event->cgroup_id = bpf_get_current_cgroup_id();
    
    send_event(event);
    return 0;
}

// Kretprobe for cgroup removal (container stop)
SEC("kretprobe/cgroup_rmdir")
int kretprobe__cgroup_rmdir(struct pt_regs *ctx) {
    u32 zero = 0;
    struct runtime_event *event;
    struct task_struct *task;
    
    event = bpf_map_lookup_elem(&heap, &zero);
    if (!event)
        return 0;
    
    __builtin_memset(event, 0, sizeof(*event));
    
    event->type = RUNTIME_EVENT_CGROUP_DESTROY;
    event->timestamp = get_timestamp();
    
    task = get_current_task();
    event->pid = BPF_CORE_READ(task, tgid);
    event->ppid = BPF_CORE_READ(task, real_parent, tgid);
    
    // Get UID/GID of the process removing the cgroup
    event->uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
    event->gid = bpf_get_current_uid_gid() >> 32;
    
    // Get comm of removing process
    bpf_get_current_comm(&event->comm, sizeof(event->comm));
    
    // Get the cgroup ID
    event->cgroup_id = bpf_get_current_cgroup_id();
    
    send_event(event);
    return 0;
}