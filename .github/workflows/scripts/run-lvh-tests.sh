#!/bin/bash
# Run tests in LVH (Little VM Helper) environment
# This script is executed inside the VM by the LVH GitHub Action

set -euo pipefail

# Configuration
GO_VERSION="${GO_VERSION:-1.24.0}"
KERNEL_VERSION="${1:-}"

# System information
echo "=== System Information ==="
ACTUAL_KERNEL=$(uname -a)
echo "${ACTUAL_KERNEL}"
echo "Expected Kernel Version: ${KERNEL_VERSION}"

# Validate kernel version if provided
if [ -n "${KERNEL_VERSION}" ]; then
    # Extract major.minor version from actual kernel
    ACTUAL_VERSION=$(uname -r | grep -oE '^[0-9]+\.[0-9]+')
    # Extract major.minor from expected kernel (format: 5.15-20250616.013250)
    EXPECTED_VERSION=$(echo "${KERNEL_VERSION}" | grep -oE '^[0-9]+\.[0-9]+')
    
    if [ "${ACTUAL_VERSION}" != "${EXPECTED_VERSION}" ]; then
        echo "WARNING: Kernel version mismatch!"
        echo "  Expected: ${EXPECTED_VERSION}"
        echo "  Actual: ${ACTUAL_VERSION}"
        # Don't fail, just warn - LVH might use slightly different kernel builds
    else
        echo "✅ Kernel version matches expected: ${EXPECTED_VERSION}"
    fi
fi

# Check kernel features
echo -e "\n=== Kernel Features ==="
if [ -f /sys/kernel/btf/vmlinux ]; then
    echo "✅ BTF support available"
    ls -la /sys/kernel/btf/vmlinux
else
    echo "⚠️ No BTF support"
fi

# Check BPF filesystem
if mount | grep -q "type bpf"; then
    echo "✅ BPF filesystem mounted"
else
    echo "⚠️ BPF filesystem not mounted"
fi

# Install dependencies
echo -e "\n=== Installing Dependencies ==="
apt-get update -qq
apt-get install -y -qq build-essential git wget curl

# Install additional tools if available
apt-get install -y -qq linux-tools-common linux-tools-generic 2>/dev/null || true
apt-get install -y -qq clang llvm libbpf-dev 2>/dev/null || true

# Install Go
echo -e "\n=== Installing Go ==="
if ! command -v go &> /dev/null; then
    cd /tmp
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
    if [ ! -f "go${GO_VERSION}.linux-amd64.tar.gz" ]; then
        echo "ERROR: Failed to download Go ${GO_VERSION}"
        exit 1
    fi
    tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
    export PATH=$PATH:/usr/local/go/bin
    rm "go${GO_VERSION}.linux-amd64.tar.gz"
fi

echo "Go version: $(go version)"

# Mount BPF filesystem if not already mounted
echo -e "\n=== Mounting BPF Filesystem ==="
if ! mount | grep -q "type bpf"; then
    mount -t bpf bpf /sys/fs/bpf || {
        echo "ERROR: Failed to mount BPF filesystem"
        exit 1
    }
fi
mount | grep bpf

# Change to host directory
cd /host

# Copy eBPF programs to standard location
echo -e "\n=== Setting up eBPF Programs ==="
if [ -d /host/artifacts/ebpf ]; then
    echo "Setting up eBPF programs..."
    
    # Count eBPF programs
    ebpf_count=$(find /host/artifacts/ebpf -name "*.bpf.o" -type f 2>/dev/null | wc -l)
    if [ "$ebpf_count" -eq 0 ]; then
        echo "ERROR: No eBPF programs found in /host/artifacts/ebpf"
        exit 1
    fi
    
    echo "Found $ebpf_count eBPF programs"
    
    # Copy to standard location expected by collectors (excluding test files)
    mkdir -p /usr/local/lib/antimetal/ebpf
    for f in /host/artifacts/ebpf/*.bpf.o; do
        basename_f=$(basename "$f")
        # Skip test programs that shouldn't be in production
        if [[ "$basename_f" == test_* ]] || [[ "$basename_f" == *_test.* ]] || [[ "$basename_f" == *_fail.* ]]; then
            echo "Skipping test program: $basename_f"
            continue
        fi
        cp -v "$f" /usr/local/lib/antimetal/ebpf/
    done
    echo "eBPF programs in /usr/local/lib/antimetal/ebpf:"
    ls -la /usr/local/lib/antimetal/ebpf/
    
    # Also copy to /host/ebpf/build for backwards compatibility (excluding test files)
    mkdir -p /host/ebpf/build
    for f in /host/artifacts/ebpf/*.bpf.o; do
        basename_f=$(basename "$f")
        # Skip test programs
        if [[ "$basename_f" == test_* ]] || [[ "$basename_f" == *_test.* ]] || [[ "$basename_f" == *_fail.* ]]; then
            continue
        fi
        cp -v "$f" /host/ebpf/build/
    done
else
    echo "ERROR: No eBPF artifacts found at /host/artifacts/ebpf"
    exit 1
fi

# Run the actual tests
echo -e "\n=== Running Integration Tests ==="
export PATH=$PATH:/usr/local/go/bin

# Set environment variable for eBPF program location
export EBPF_BUILD_DIR=/usr/local/lib/antimetal/ebpf

# Run integration tests with verbose output and save results
echo "Running eBPF integration tests..."

# Create test results file
TEST_RESULTS_FILE="/host/integration-test-results.txt"
VM_OUTPUT_FILE="/host/vm-output.log"

# Save VM output
{
    echo "=== VM Output Log ==="
    echo "Kernel: $(uname -r)"
    echo "Time: $(date)"
    echo "Go Version: $(go version)"
    echo ""
} > "$VM_OUTPUT_FILE"

# Run tests and capture output
echo "Running integration tests and saving results..."
if go test -tags integration -v ./pkg/ebpf/core -run TestEBPF 2>&1 | tee -a "$VM_OUTPUT_FILE" > "$TEST_RESULTS_FILE"; then
    TEST_STATUS="PASS"
    echo -e "\n✅ Integration tests PASSED"
else
    TEST_STATUS="FAIL"
    echo -e "\n❌ Integration tests FAILED"
    # Don't exit immediately - save results first
fi

# Append summary to results file
{
    echo ""
    echo "=== Test Summary ==="
    echo "Status: $TEST_STATUS"
    echo "Kernel: $(uname -r)"
    echo "Time: $(date)"
} >> "$TEST_RESULTS_FILE"

echo -e "\n=== Test Completed ==="
echo "Results saved to: $TEST_RESULTS_FILE"
echo "VM output saved to: $VM_OUTPUT_FILE"
echo "Kernel: $(uname -r)"
echo "Time: $(date)"

# Exit with appropriate status
if [ "$TEST_STATUS" = "FAIL" ]; then
    exit 1
fi