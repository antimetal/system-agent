#!/usr/bin/env bash
# Generate vmlinux.h for eBPF programs

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
VMLINUX_H="${PROJECT_ROOT}/ebpf/include/vmlinux.h"

if [ -f "${VMLINUX_H}" ]; then
    echo "vmlinux.h already exists"
    exit 0
fi

echo "Generating vmlinux.h..."

# Use Docker to generate vmlinux.h
docker run --rm \
    -v "${PROJECT_ROOT}:/workspace" \
    -w /workspace \
    ebpf-builder \
    bash -c "
        apt-get update && apt-get install -y bpftool
        bpftool btf dump file /sys/kernel/btf/vmlinux format c > /workspace/ebpf/include/vmlinux.h
    " || {
    echo "Failed to generate vmlinux.h using Docker, creating minimal version..."
    cat > "${VMLINUX_H}" << 'EOF'
/* Minimal vmlinux.h for eBPF compilation */
#ifndef __VMLINUX_H__
#define __VMLINUX_H__

typedef unsigned char __u8;
typedef unsigned short __u16;
typedef unsigned int __u32;
typedef unsigned long long __u64;

typedef signed char __s8;
typedef signed short __s16;
typedef signed int __s32;
typedef signed long long __s64;

struct trace_event_raw_sys_enter {
    __u64 args[6];
};

#endif /* __VMLINUX_H__ */
EOF
}

echo "vmlinux.h generated successfully"