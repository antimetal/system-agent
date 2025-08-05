#!/bin/bash
# PolyForm Shield License: https://polyformproject.org/licenses/shield/1.0.0
# Copyright 2024 Antimetal Inc.

set -euo pipefail

# Script to set up the test environment inside the VM
# This prepares the VM for running integration tests

echo "=== Setting up test environment ==="
echo "Kernel: $(uname -r)"
echo "================================="

# Detect the distribution
if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS=$ID
    VER=$VERSION_ID
else
    echo "Cannot detect OS distribution"
    exit 1
fi

echo "Detected OS: $OS $VER"

# Install required packages based on distribution
case "$OS" in
    ubuntu|debian)
        echo "Installing packages for Debian-based system..."
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -qq
        apt-get install -y -qq \
            build-essential \
            linux-headers-$(uname -r) \
            libbpf-dev \
            clang \
            llvm \
            gcc \
            make \
            pkg-config \
            libelf-dev \
            bpftool \
            stress-ng \
            sysstat \
            procinfo \
            2>&1 | grep -v "debconf: delaying package configuration"
        ;;
    
    fedora|centos|rhel)
        echo "Installing packages for Red Hat-based system..."
        yum install -y \
            kernel-devel-$(uname -r) \
            libbpf-devel \
            clang \
            llvm \
            gcc \
            make \
            elfutils-libelf-devel \
            bpftool \
            stress-ng \
            sysstat \
            procinfo
        ;;
    
    alpine)
        echo "Installing packages for Alpine Linux..."
        apk add --no-cache \
            build-base \
            linux-headers \
            libbpf-dev \
            clang \
            llvm \
            elfutils-dev \
            bpftool \
            stress-ng \
            sysstat
        ;;
    
    *)
        echo "Unsupported distribution: $OS"
        echo "Proceeding without package installation..."
        ;;
esac

# Set up kernel parameters for eBPF testing
echo "Configuring kernel parameters..."

# Enable BPF JIT compiler
if [ -w /proc/sys/net/core/bpf_jit_enable ]; then
    echo 1 > /proc/sys/net/core/bpf_jit_enable
    echo "✓ Enabled BPF JIT compiler"
fi

# Increase RLIMIT_MEMLOCK for eBPF
if command -v ulimit &> /dev/null; then
    ulimit -l unlimited 2>/dev/null || true
    echo "✓ Set unlimited locked memory"
fi

# Mount required filesystems if not already mounted
echo "Checking required filesystems..."

# Mount debugfs (required for some eBPF features)
if ! mountpoint -q /sys/kernel/debug; then
    mount -t debugfs none /sys/kernel/debug 2>/dev/null || true
fi

# Mount tracefs (required for tracing)
if ! mountpoint -q /sys/kernel/tracing; then
    mount -t tracefs none /sys/kernel/tracing 2>/dev/null || true
fi

# Check BPF filesystem
if ! mountpoint -q /sys/fs/bpf; then
    mount -t bpf none /sys/fs/bpf 2>/dev/null || true
fi

# Verify eBPF support
echo ""
echo "=== Verifying eBPF Support ==="

# Check for BPF syscall support
if [ -f /proc/kallsyms ]; then
    if grep -q "sys_bpf" /proc/kallsyms; then
        echo "✓ BPF syscall available"
    else
        echo "✗ BPF syscall not found"
    fi
fi

# Check for BTF support
if [ -f /sys/kernel/btf/vmlinux ]; then
    echo "✓ BTF (BPF Type Format) available"
    ls -la /sys/kernel/btf/vmlinux
else
    echo "✗ BTF not available (kernel $(uname -r))"
fi

# Check for BPF filesystem
if [ -d /sys/fs/bpf ]; then
    echo "✓ BPF filesystem available"
else
    echo "✗ BPF filesystem not available"
fi

# Create test directories
echo ""
echo "Creating test directories..."
mkdir -p /tests/{results,logs,temp}
chmod 755 /tests

# Generate system information
echo ""
echo "=== System Information ==="
echo "CPU Info:"
lscpu | grep -E "^(Architecture|CPU\(s\)|Model name)" || true

echo ""
echo "Memory Info:"
free -h | head -2

echo ""
echo "Kernel Config (BPF-related):"
if [ -f /boot/config-$(uname -r) ]; then
    grep -E "CONFIG_BPF|CONFIG_HAVE_EBPF|CONFIG_DEBUG_INFO_BTF" /boot/config-$(uname -r) | head -10 || true
elif [ -f /proc/config.gz ]; then
    zcat /proc/config.gz | grep -E "CONFIG_BPF|CONFIG_HAVE_EBPF|CONFIG_DEBUG_INFO_BTF" | head -10 || true
else
    echo "Kernel config not available"
fi

echo ""
echo "=== Setup Complete ==="
echo "Test environment is ready for kernel integration tests"