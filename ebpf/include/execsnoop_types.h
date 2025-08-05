// SPDX-License-Identifier: GPL-2.0-only
/*
 * This header defines the data structures shared between BPF and userspace.
 * It is intentionally minimal and GPL-licensed to meet kernel requirements.
 */

#ifndef __EXECSNOOP_TYPES_H
#define __EXECSNOOP_TYPES_H

#define TASK_COMM_LEN 16
#define ARGSIZE 128
#define TOTAL_MAX_ARGS 60
#define FULL_MAX_ARGS_ARR (TOTAL_MAX_ARGS * ARGSIZE)

struct execsnoop_event {
  __s32 pid;
  __s32 ppid;
  __u32 uid;
  __s32 retval;
  __s32 args_count;
  __u32 args_size;
  char comm[TASK_COMM_LEN];
  // Variable length args data follows
};

#endif /* __EXECSNOOP_TYPES_H */