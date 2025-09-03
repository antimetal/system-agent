// SPDX-License-Identifier: GPL-2.0-only
#ifndef __RUNTIME_TYPES_H__
#define __RUNTIME_TYPES_H__

// Event types must match Go definitions
enum runtime_event_type {
    RUNTIME_EVENT_PROCESS_FORK = 0,
    RUNTIME_EVENT_PROCESS_EXEC = 1,
    RUNTIME_EVENT_PROCESS_EXIT = 2,
    RUNTIME_EVENT_CGROUP_CREATE = 3,
    RUNTIME_EVENT_CGROUP_DESTROY = 4,
};

// Maximum sizes for various fields
#define MAX_COMM_LEN 16
#define MAX_FILENAME_LEN 256
#define MAX_ARGS_SIZE 512  // Reduced to avoid verifier issues

// Runtime event structure - must match Go struct
struct runtime_event {
    __u32 type;          // Event type from enum above
    __u64 timestamp;     // Nanoseconds since boot
    __u32 pid;           // Process ID
    __u32 ppid;          // Parent process ID
    __u32 uid;           // User ID
    __u32 gid;           // Group ID
    __u64 cgroup_id;     // Cgroup ID for container events
    char comm[MAX_COMM_LEN];      // Process command
    char filename[MAX_FILENAME_LEN]; // Executable path for exec events
    __u32 args_size;     // Size of arguments
    char args[MAX_ARGS_SIZE];  // Null-separated arguments
};

#endif /* __RUNTIME_TYPES_H__ */