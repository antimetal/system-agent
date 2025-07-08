#ifndef __COMMON_H__
#define __COMMON_H__

// Simple event structure for our hello world program
struct event {
    __u32 pid;
    char comm[16];
};

#endif /* __COMMON_H__ */