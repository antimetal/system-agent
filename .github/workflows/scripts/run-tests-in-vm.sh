#!/bin/bash
# Run integration tests in a VM environment
# This script sets up the test environment and runs both unit and integration tests

set -euo pipefail

# Configuration
GO_VERSION="${GO_VERSION:-1.24.0}"
EXPECTED_KERNEL="${EXPECTED_KERNEL:-}"

echo "=========================================="
echo "Integration Test Runner"
echo "Expected Kernel: ${EXPECTED_KERNEL}"
echo "=========================================="

# System information
echo -e "\n=== System Information ==="
ACTUAL_KERNEL=$(uname -r)
echo "Actual Kernel: ${ACTUAL_KERNEL}"
echo "Architecture: $(uname -m)"
echo "Date: $(date)"

# Validate kernel version if expected kernel is specified
if [ -n "${EXPECTED_KERNEL}" ]; then
    echo -e "\n=== Kernel Version Validation ==="
    # Extract major.minor version from actual kernel
    ACTUAL_VERSION=$(echo "${ACTUAL_KERNEL}" | grep -oE '^[0-9]+\.[0-9]+')
    # Extract major.minor from expected kernel (format: 5.15-20250616.013250)
    EXPECTED_VERSION=$(echo "${EXPECTED_KERNEL}" | grep -oE '^[0-9]+\.[0-9]+')
    
    if [ "${ACTUAL_VERSION}" != "${EXPECTED_VERSION}" ]; then
        echo "ERROR: Kernel version mismatch!"
        echo "  Expected: ${EXPECTED_VERSION}"
        echo "  Actual: ${ACTUAL_VERSION}"
        exit 1
    fi
    echo "✅ Kernel version matches expected: ${EXPECTED_VERSION}"
fi

# Check kernel features
echo -e "\n=== Kernel Features ==="
if [ -f /sys/kernel/btf/vmlinux ]; then
    echo "✅ BTF support available"
    ls -la /sys/kernel/btf/vmlinux
else
    echo "ERROR: No BTF support (kernel $(uname -r)) - required for CO-RE eBPF"
    exit 1
fi

# Check filesystems
echo -e "\n=== Filesystems ==="
for fs in /proc /sys /sys/fs/cgroup /sys/fs/bpf; do
    if [ -d "$fs" ]; then
        echo "✅ $fs exists"
    else
        echo "❌ $fs missing"
        if [ "$fs" = "/sys/fs/bpf" ]; then
            echo "  Attempting to mount BPF filesystem..."
            sudo mkdir -p /sys/fs/bpf
            sudo mount -t bpf bpf /sys/fs/bpf || { echo "ERROR: Failed to mount BPF filesystem"; exit 1; }
        fi
    fi
done

# Install Go if not available
echo -e "\n=== Go Installation ==="
export PATH=$PATH:/usr/local/go/bin
if ! command -v go &> /dev/null; then
    echo "Installing Go ${GO_VERSION}..."
    cd /tmp
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
    if [ ! -f "go${GO_VERSION}.linux-amd64.tar.gz" ]; then
        echo "ERROR: Failed to download Go ${GO_VERSION}"
        exit 1
    fi
    sudo tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
    rm "go${GO_VERSION}.linux-amd64.tar.gz"
    
    # Verify installation
    if ! command -v go &> /dev/null; then
        echo "ERROR: Go installation failed"
        exit 1
    fi
fi
echo "Go version: $(go version)"

# Install dependencies
echo -e "\n=== Installing Dependencies ==="
if command -v apt-get &> /dev/null; then
    sudo apt-get update -qq
    sudo apt-get install -y -qq build-essential git
    
    # Install bpftool (required)
    sudo apt-get install -y -qq linux-tools-common linux-tools-generic
else
    echo "ERROR: apt-get not available - cannot install dependencies"
    exit 1
fi

# Mount BPF filesystem if needed
if ! mount | grep -q "type bpf"; then
    echo -e "\n=== Mounting BPF Filesystem ==="
    sudo mount -t bpf bpf /sys/fs/bpf || {
        echo "ERROR: Failed to mount BPF filesystem"
        exit 1
    }
fi

cd /host

# Generate code if needed
if [ -f Makefile ] && grep -q "^generate:" Makefile; then
    echo -e "\n=== Generating Code ==="
    make generate || {
        echo "ERROR: Code generation failed"
        exit 1
    }
fi

# Copy eBPF programs from artifacts to standard location
if [ -d /host/artifacts/ebpf ]; then
    echo -e "\n=== Setting up eBPF Programs ==="
    
    # Count eBPF programs
    ebpf_count=$(find /host/artifacts/ebpf -name "*.bpf.o" -type f | wc -l)
    if [ "$ebpf_count" -eq 0 ]; then
        echo "ERROR: No eBPF programs found in /host/artifacts/ebpf"
        exit 1
    fi
    
    echo "Found $ebpf_count eBPF programs to deploy"
    
    # Copy to standard location expected by collectors
    sudo mkdir -p /usr/local/lib/antimetal/ebpf
    sudo cp -v /host/artifacts/ebpf/*.bpf.o /usr/local/lib/antimetal/ebpf/
    echo "eBPF programs in /usr/local/lib/antimetal/ebpf:"
    ls -la /usr/local/lib/antimetal/ebpf/
    
    # Also copy to /host/ebpf/build for backwards compatibility with tests
    mkdir -p /host/ebpf/build
    cp -v /host/artifacts/ebpf/*.bpf.o /host/ebpf/build/
else
    echo "ERROR: No eBPF artifacts found at /host/artifacts/ebpf"
    exit 1
fi

# Run unit tests
echo -e "\n=== Running Unit Tests ==="
if ! go test ./... -v 2>&1 | tee unit-test-results.txt; then
    echo "ERROR: Unit tests failed"
    exit 1
fi

# Run integration tests
echo -e "\n=== Running Integration Tests (including eBPF verification) ==="
if ! sudo -E PATH="$PATH" go test -tags integration -v ./pkg/ebpf/core -run TestEBPF 2>&1 | tee integration-test-results.txt; then
    echo "ERROR: Integration tests failed"
    exit 1
fi

echo -e "\n=== Test Summary ==="
echo "Tests completed at $(date)"
echo "Kernel: ${ACTUAL_KERNEL}"

# All tests passed if we got here
echo "All tests completed successfully"