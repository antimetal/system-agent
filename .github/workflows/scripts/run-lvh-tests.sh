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
        echo "ERROR: Kernel version mismatch!"
        echo "  Expected: ${EXPECTED_VERSION}"
        echo "  Actual: ${ACTUAL_VERSION}"
        exit 1
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
    echo "ERROR: No BTF support - required for CO-RE eBPF"
    exit 1
fi

# Check BPF filesystem
if mount | grep -q "type bpf"; then
    echo "✅ BPF filesystem mounted"
else
    echo "ERROR: BPF filesystem not mounted"
    exit 1
fi

# Install dependencies (minimal set - no Go needed since we use pre-built binary)
echo -e "\n=== Installing Dependencies ==="
apt-get update -qq
apt-get install -y -qq build-essential git

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
    ebpf_count=$(find /host/artifacts/ebpf -name "*.bpf.o" -type f | wc -l)
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

# Verify integration test binaries exist
echo -e "\n=== Verifying Integration Test Binaries ==="
if [ ! -d /host/artifacts/integration-tests ] || [ -z "$(ls -A /host/artifacts/integration-tests)" ]; then
    echo "ERROR: Integration test binaries not found at /host/artifacts/integration-tests/"
    exit 1
fi
echo "Integration test binaries found:"
ls -la /host/artifacts/integration-tests/

# Fix execute permissions (GitHub Actions artifact upload/download strips them)
echo "Restoring execute permissions for integration test binaries..."
chmod +x /host/artifacts/integration-tests/*
echo "Updated permissions:"
ls -la /host/artifacts/integration-tests/

# Run the actual tests
echo -e "\n=== Running Integration Tests ==="

# Set environment variable for eBPF program location
export EBPF_BUILD_DIR=/usr/local/lib/antimetal/ebpf

# Run pre-built integration test binary with verbose output and save results
echo "Running pre-built integration test binary..."

# Create test results file
TEST_RESULTS_FILE="/host/integration-test-results.txt"
VM_OUTPUT_FILE="/host/vm-output.log"

# Save VM output
{
    echo "=== VM Output Log ==="
    echo "Kernel: $(uname -r)"
    echo "Time: $(date)"
    echo "Integration Test Binaries:"
    ls -la /host/artifacts/integration-tests/
    echo ""
} > "$VM_OUTPUT_FILE"

# Run tests and capture output
echo "Running integration test binaries and saving results..."
cd /host
TEST_STATUS="PASS"

# Run each test binary individually
for test_binary in /host/artifacts/integration-tests/*.test; do
    if [ -f "$test_binary" ]; then
        binary_name=$(basename "$test_binary")
        echo "Running $binary_name..."
        
        if $test_binary -test.v 2>&1 | tee -a "$VM_OUTPUT_FILE" >> "$TEST_RESULTS_FILE"; then
            echo "✅ $binary_name PASSED"
        else
            echo "❌ $binary_name FAILED"
            TEST_STATUS="FAIL"
        fi
        echo "" >> "$TEST_RESULTS_FILE"
    fi
done

if [ "$TEST_STATUS" = "PASS" ]; then
    echo -e "\n✅ All integration tests PASSED"
else
    echo -e "\n❌ Some integration tests FAILED"
fi

# Append summary to results file
{
    echo ""
    echo "=== Test Summary ==="
    echo "Status: $TEST_STATUS"
    echo "Kernel: $(uname -r)"
    echo "Time: $(date)"
    echo "Integration Test Binaries:"
    ls -la /host/artifacts/integration-tests/
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