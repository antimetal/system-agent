#!/usr/bin/env bash
# Copyright 2024 Antimetal, Inc.
# SPDX-License-Identifier: PolyForm-Shield-1.0.0
#
# Generate vmlinux.h from BTF for CO-RE eBPF programs
# This script can work both in Docker builds and local development

set -euo pipefail

# Configuration
BTF_URL="https://github.com/aquasecurity/btfhub-archive/raw/main/ubuntu/20.04/arm64/5.8.0-63-generic.btf.tar.xz"
EXPECTED_BTF_SHA256="cdd9e65811a4de0e98012dd1c59ea3a90aa57a27b6b1896d2abf83f0713d0138"

# Determine output path based on environment
if [ -n "${DOCKER_BUILD:-}" ] || [ -f /.dockerenv ]; then
    # Running in Docker
    VMLINUX_H="/usr/include/vmlinux.h"
else
    # Running locally
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
fi

echo "=== Generating vmlinux.h ==="

# Try to generate from system BTF first (if not in Docker)
if [ -z "${DOCKER_BUILD:-}" ] && [ -f /sys/kernel/btf/vmlinux ]; then
    echo "Found system BTF, generating vmlinux.h from kernel BTF..."
    if command -v bpftool &> /dev/null; then
        bpftool btf dump file /sys/kernel/btf/vmlinux format c > "${VMLINUX_H}"
        echo "Successfully generated vmlinux.h from system BTF"
        echo "Location: ${VMLINUX_H}"
        echo "Size: $(wc -l < "${VMLINUX_H}") lines"
        exit 0
    else
        echo "Warning: bpftool not found, falling back to download"
    fi
fi

# Download BTF archive
echo "Downloading pre-generated BTF from btfhub..."
echo "URL: ${BTF_URL}"

# Create temporary directory
TMP_DIR=$(mktemp -d)
trap "rm -rf ${TMP_DIR}" EXIT

# Download BTF archive
curl -sL "$BTF_URL" -o "${TMP_DIR}/btf.tar.xz"

echo "Downloaded BTF archive, verifying integrity..."

# Verify checksum
ACTUAL_SHA256=$(sha256sum "${TMP_DIR}/btf.tar.xz" | cut -d' ' -f1)
echo "BTF archive SHA256: $ACTUAL_SHA256"
echo "Expected SHA256: $EXPECTED_BTF_SHA256"

if [ "$ACTUAL_SHA256" != "$EXPECTED_BTF_SHA256" ]; then
    echo "ERROR: BTF archive checksum mismatch!"
    echo "Expected: $EXPECTED_BTF_SHA256"
    echo "Actual:   $ACTUAL_SHA256"
    echo ""
    echo "To update checksums for new BTF versions:"
    echo "1. Update the BTF_URL in this script"
    echo "2. Temporarily comment out the checksum verification"
    echo "3. Run the script to download and see the actual checksum"
    echo "4. Update EXPECTED_BTF_SHA256 with the actual checksum"
    echo "5. Uncomment the verification and test again"
    exit 1
fi

echo "✓ BTF archive integrity verified"

# Extract and generate vmlinux.h
echo "Extracting BTF and generating vmlinux.h..."
tar -xJf "${TMP_DIR}/btf.tar.xz" -C "${TMP_DIR}"

# Find the extracted BTF file
BTF_FILE=$(find "${TMP_DIR}" -name "*.btf" -type f | head -1)
if [ -z "${BTF_FILE}" ]; then
    echo "Error: No BTF file found in archive"
    exit 1
fi

# Check for bpftool
if ! command -v bpftool &> /dev/null; then
    echo "Error: bpftool is required to generate vmlinux.h"
    echo "Please install bpftool (usually in linux-tools-common or bpf-tools package)"
    exit 1
fi

# Generate vmlinux.h
bpftool btf dump file "${BTF_FILE}" format c > "${VMLINUX_H}"

# Verify vmlinux.h was generated successfully
if [ -f "${VMLINUX_H}" ] && [ -s "${VMLINUX_H}" ]; then
    echo "=== vmlinux.h successfully generated ==="
    echo "Location: ${VMLINUX_H}"
    echo "Size: $(wc -l < "${VMLINUX_H}") lines"
    echo "First few type definitions:"
    grep -m 5 "struct\|enum\|typedef" "${VMLINUX_H}" || true
else
    echo "ERROR: vmlinux.h was not created or is empty!"
    exit 1
fi

echo "✓ vmlinux.h generation complete"