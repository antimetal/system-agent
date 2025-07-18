#!/bin/bash
# Generate or download vmlinux.h for eBPF compilation

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EBPF_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
INCLUDE_DIR="${EBPF_DIR}/include"
VMLINUX_H="${INCLUDE_DIR}/vmlinux.h"

# Create include directory if it doesn't exist
mkdir -p "${INCLUDE_DIR}"

# Check if vmlinux.h already exists and is recent
if [ -f "${VMLINUX_H}" ]; then
    # Check if file is less than 7 days old
    if [ "$(find "${VMLINUX_H}" -mtime -7 -print)" ]; then
        echo "vmlinux.h already exists and is recent, skipping generation"
        exit 0
    fi
fi

echo "Generating vmlinux.h..."

# Try to generate from system BTF first
if [ -f /sys/kernel/btf/vmlinux ]; then
    echo "Found system BTF, generating vmlinux.h from kernel BTF..."
    if command -v bpftool &> /dev/null; then
        bpftool btf dump file /sys/kernel/btf/vmlinux format c > "${VMLINUX_H}"
        echo "Successfully generated vmlinux.h from system BTF"
        exit 0
    else
        echo "Warning: bpftool not found, falling back to download"
    fi
fi

# Fallback: Download from btfhub
echo "Downloading pre-generated vmlinux.h from btfhub..."

# Detect architecture
ARCH=$(uname -m)
case "${ARCH}" in
    x86_64)
        BTFHUB_ARCH="x86"
        ;;
    aarch64)
        BTFHUB_ARCH="arm64"
        ;;
    *)
        echo "Error: Unsupported architecture ${ARCH}"
        exit 1
        ;;
esac

# Get kernel version for best match
KERNEL_VERSION=$(uname -r | cut -d'-' -f1)
KERNEL_MAJOR=$(echo "${KERNEL_VERSION}" | cut -d'.' -f1)
KERNEL_MINOR=$(echo "${KERNEL_VERSION}" | cut -d'.' -f2)

# Try to find a close match in btfhub
# For simplicity, we'll use a known good version that works across many kernels
# This is the same approach used in the Dockerfile
BTFHUB_URL="https://raw.githubusercontent.com/aquasecurity/btfhub-archive/main/ubuntu/20.04/${BTFHUB_ARCH}/5.8.0-63-generic.btf.tar.xz"

# Create temporary directory
TMP_DIR=$(mktemp -d)
trap "rm -rf ${TMP_DIR}" EXIT

# Download BTF file
echo "Downloading BTF from ${BTFHUB_URL}..."
if ! curl -sL "${BTFHUB_URL}" -o "${TMP_DIR}/vmlinux.btf.tar.xz"; then
    echo "Error: Failed to download BTF file"
    exit 1
fi

# Extract BTF file from tar.xz
echo "Extracting BTF file..."
if ! tar -xJf "${TMP_DIR}/vmlinux.btf.tar.xz" -C "${TMP_DIR}"; then
    echo "Error: Failed to extract BTF archive"
    exit 1
fi

# Find the extracted BTF file (should be named like 5.4.0-91-generic.btf)
BTF_FILE=$(find "${TMP_DIR}" -name "*.btf" -type f | head -1)
if [ -z "${BTF_FILE}" ]; then
    echo "Error: No BTF file found in archive"
    exit 1
fi

# Generate vmlinux.h from downloaded BTF
if command -v bpftool &> /dev/null; then
    echo "Generating vmlinux.h from downloaded BTF..."
    bpftool btf dump file "${BTF_FILE}" format c > "${VMLINUX_H}"
    echo "Successfully generated vmlinux.h"
else
    echo "Error: bpftool is required to generate vmlinux.h"
    echo "Please install bpftool (usually in linux-tools-common or bpf-tools package)"
    exit 1
fi