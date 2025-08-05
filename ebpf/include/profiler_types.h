// SPDX-License-Identifier: GPL-2.0-only
/*
 * This header defines the data structures shared between BPF and userspace
 * for the perf event-based profiler.
 */

#ifndef __PROFILER_TYPES_H
#define __PROFILER_TYPES_H

#define MAX_STACK_DEPTH 64
#define PROF_COMM_LEN 16

// Key for tracking unique stack traces
struct stack_key {
    __s32 pid;
    __s32 tid;
    __s32 user_stack_id;
    __s32 kernel_stack_id;
};

// Value for counting stack occurrences
struct stack_count {
    __u64 count;
    __u32 cpu;
    __u32 padding; // For alignment
};

#endif /* __PROFILER_TYPES_H */