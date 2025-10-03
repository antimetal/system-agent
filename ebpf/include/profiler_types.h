// SPDX-License-Identifier: GPL-2.0-only
/*
 * This header defines the data structures shared between BPF and userspace
 * for the continuous profiler.
 *
 * The profile_event structure is designed to be exactly 32 bytes for optimal
 * memory efficiency and to enable zero-copy operations with BPF ring buffers.
 */

#ifndef __PROFILER_TYPES_H
#define __PROFILER_TYPES_H

/*
 * Event flags for profiler events.
 * These flags provide metadata about the profiling event and stack trace quality.
 */

/* User stack trace was truncated due to depth limit */
#define PROF_FLAG_USER_STACK_TRUNCATED   (1 << 0)

/* Kernel stack trace was truncated due to depth limit */
#define PROF_FLAG_KERNEL_STACK_TRUNCATED (1 << 1)

/* Stack ID collision occurred (hash collision in stack map) */
#define PROF_FLAG_STACK_COLLISION        (1 << 2)

/* Timestamp was adjusted (e.g., for clock skew correction) */
#define PROF_FLAG_TIME_ADJUSTED          (1 << 3)

/* Reserved for future use: bits 4-31 */

/*
 * Fixed 32-byte profiling event structure.
 *
 * This structure is carefully designed to be exactly 32 bytes with proper
 * alignment on all supported architectures (x86_64, arm64). All fields are
 * naturally aligned to avoid padding.
 *
 * Memory layout:
 *   Offset  Size  Field
 *   0       8     timestamp
 *   8       4     pid
 *   12      4     tid
 *   16      4     user_stack_id
 *   20      4     kernel_stack_id
 *   24      4     cpu
 *   28      4     flags
 *   Total: 32 bytes
 */
struct profile_event {
	/* Timestamp in nanoseconds since boot (from bpf_ktime_get_ns()) */
	__u64 timestamp;

	/* Process ID of the sampled task */
	__s32 pid;

	/* Thread ID of the sampled task */
	__s32 tid;

	/* Stack trace ID for user-space stack (from BPF_MAP_TYPE_STACK_TRACE) */
	__s32 user_stack_id;

	/* Stack trace ID for kernel-space stack (from BPF_MAP_TYPE_STACK_TRACE) */
	__s32 kernel_stack_id;

	/* CPU number where the sample was taken */
	__u32 cpu;

	/* Event flags (combination of PROF_FLAG_* values) */
	__u32 flags;
} __attribute__((packed)); /* Ensure no compiler-added padding */

/*
 * Compile-time assertions to verify structure size and alignment.
 * These will cause compilation to fail if the structure is not exactly 32 bytes.
 */
_Static_assert(sizeof(struct profile_event) == 32,
	       "profile_event must be exactly 32 bytes");

#endif /* __PROFILER_TYPES_H */


