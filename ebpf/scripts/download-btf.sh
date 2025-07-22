#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm
# Download and verify BTF archive for vmlinux.h generation

set -euo pipefail

# Configuration
BTF_URL="https://github.com/aquasecurity/btfhub-archive/raw/main/ubuntu/20.04/arm64/5.8.0-63-generic.btf.tar.xz"
EXPECTED_BTF_SHA256="cdd9e65811a4de0e98012dd1c59ea3a90aa57a27b6b1896d2abf83f0713d0138"

echo "=== Downloading pre-generated vmlinux.h ==="

# Download BTF archive
curl -sL "$BTF_URL" -o /tmp/btf.tar.xz

echo "Downloaded BTF archive, verifying integrity..."

# Verify checksum
ACTUAL_SHA256=$(sha256sum /tmp/btf.tar.xz | cut -d' ' -f1)
echo "BTF archive SHA256: $ACTUAL_SHA256"
echo "Expected SHA256: $EXPECTED_BTF_SHA256"

if [ "$ACTUAL_SHA256" != "$EXPECTED_BTF_SHA256" ]; then
    echo "ERROR: BTF archive checksum mismatch!"
    echo "Expected: $EXPECTED_BTF_SHA256"
    echo "Actual:   $ACTUAL_SHA256"
    exit 1
fi

echo "✓ BTF archive integrity verified"

# Extract and generate vmlinux.h
tar -xJf /tmp/btf.tar.xz -C /tmp
bpftool btf dump file /tmp/5.8.0-63-generic.btf format c > /usr/include/vmlinux.h
rm -f /tmp/btf.tar.xz /tmp/5.8.0-63-generic.btf

echo "Downloaded vmlinux.h with $(wc -l < /usr/include/vmlinux.h 2>/dev/null || echo 0) lines"