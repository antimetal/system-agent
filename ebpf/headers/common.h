// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
// Copyright (c) 2024 Antimetal

#ifndef __COMMON_H__
#define __COMMON_H__

// Simple event structure for our hello world program
struct event {
    __u32 pid;
    char comm[16];
};

#endif /* __COMMON_H__ */