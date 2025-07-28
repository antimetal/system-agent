#!/bin/bash
# Copyright 2024 Antimetal, Inc.
# SPDX-License-Identifier: PolyForm-Shield-1.0.0

# Install Go for eBPF development
# This script installs Go from the official tarball with checksum verification

set -euo pipefail

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64) GO_ARCH="amd64" ;;
    aarch64) GO_ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
esac

# Go version and download URL
GO_VERSION="1.24.1"
GO_URL="https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"

# Expected checksums for Go 1.24.1 (update when changing GO_VERSION)
# To update checksums for new Go versions:
# 1. Temporarily comment out the checksum verification section below
# 2. Run: make build-ebpf-builder
# 3. Copy the displayed SHA256 checksums from the build output
# 4. Update the EXPECTED_GO_SHA256_* variables below
# 5. Uncomment the verification section
# 6. Test with: make build-ebpf-builder
case "$GO_ARCH" in
    amd64) EXPECTED_GO_SHA256="cb2396bae64183cdccf81a9a6df0aea3bce9511fc21469fb89a0c00470088073" ;;
    arm64) EXPECTED_GO_SHA256="8df5750ffc0281017fb6070fba450f5d22b600a02081dceef47966ffaf36a3af" ;;
    *) echo "Unsupported Go architecture: $GO_ARCH" && exit 1 ;;
esac

# Download Go
echo "Downloading Go ${GO_VERSION} for ${GO_ARCH}..."
curl -sL "$GO_URL" -o /tmp/go.tar.gz

# Verify checksum
echo "Downloaded Go ${GO_VERSION} for ${GO_ARCH}, verifying checksum..."
ACTUAL_SHA256=$(sha256sum /tmp/go.tar.gz | cut -d' ' -f1)
echo "Go archive SHA256: $ACTUAL_SHA256"
echo "Expected SHA256: $EXPECTED_GO_SHA256"

if [ "$ACTUAL_SHA256" != "$EXPECTED_GO_SHA256" ]; then
    echo "ERROR: Go archive checksum mismatch!"
    echo "Expected: $EXPECTED_GO_SHA256"
    echo "Actual:   $ACTUAL_SHA256"
    echo "If updating Go version, see comments above for checksum update procedure"
    exit 1
fi

echo "✓ Go archive integrity verified"

# Extract and install
tar -C /usr/local -xzf /tmp/go.tar.gz
rm -f /tmp/go.tar.gz

echo "✓ Go ${GO_VERSION} successfully installed to /usr/local/go"