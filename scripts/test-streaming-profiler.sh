#!/bin/bash
# Copyright Antimetal, Inc. All rights reserved.
#
# Use of this source code is governed by a source available license that can be found in the
# LICENSE file or at:
# https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

set -e

# Test script for streaming eBPF profiler with ring buffer
# This script validates the profiler across different architectures and kernel versions

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Streaming eBPF Profiler Test Suite ===${NC}"

# Configuration
TEST_DURATION=${TEST_DURATION:-10s}
TEST_VMS=${TEST_VMS:-"default profiler-x86 ebpf-kernel-5.8"}
BUILD_DIR="/tmp/profiler-tests-$$"

# Create build directory
mkdir -p "${BUILD_DIR}"

# Function to print status
print_status() {
    echo -e "${YELLOW}>>> $1${NC}"
}

# Function to print success
print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

# Function to print error
print_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Clean up on exit
cleanup() {
    rm -rf "${BUILD_DIR}"
}
trap cleanup EXIT

# Step 1: Build eBPF objects
print_status "Building eBPF objects..."
cd "${PROJECT_ROOT}"
if make build-ebpf; then
    print_success "eBPF objects built successfully"
else
    print_error "Failed to build eBPF objects"
    exit 1
fi

# Step 2: Create test program
print_status "Creating ring buffer test program..."
cat > "${BUILD_DIR}/test-ringbuf.go" << 'EOF'
//go:build linux

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

func main() {
	zapLog, _ := zap.NewDevelopment()
	logger := zapr.NewLogger(zapLog)

	fmt.Printf("=== Ring Buffer Profiler Test ===\n")
	fmt.Printf("Kernel: %s\n", getKernelVersion())
	fmt.Printf("Arch: %s\n", runtime.GOARCH)
	
	// Test configurations
	tests := []struct {
		name string
		rate uint64
		dur  time.Duration
	}{
		{"100Hz", 10000000, 10 * time.Second},
		{"1KHz", 1000000, 10 * time.Second},
		{"10KHz", 100000, 10 * time.Second},
	}

	allPassed := true
	for _, tc := range tests {
		fmt.Printf("\n=== Test: %s ===\n", tc.name)
		if err := runTest(logger, tc.rate, tc.dur); err != nil {
			fmt.Printf("FAILED: %v\n", err)
			allPassed = false
		}
	}

	if !allPassed {
		os.Exit(1)
	}
}

func runTest(logger logr.Logger, samplePeriod uint64, duration time.Duration) error {
	config := performance.CollectionConfig{
		Interval:    time.Second,
		HostSysPath: "/sys",
	}

	profiler, err := collectors.NewProfiler(logger, config)
	if err != nil {
		return fmt.Errorf("creating profiler: %w", err)
	}
	defer profiler.Stop()

	setupConfig := collectors.ProfilerConfig{
		Event: collectors.PerfEventConfig{
			Name:         "cpu-clock",
			Type:         collectors.PERF_TYPE_SOFTWARE,
			Config:       collectors.PERF_COUNT_SW_CPU_CLOCK,
			SamplePeriod: samplePeriod,
		},
	}

	if err := profiler.Setup(setupConfig); err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	outputChan, err := profiler.Start(ctx)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}

	var totalEvents int64
	var totalDropped uint64

	for {
		select {
		case <-ctx.Done():
			goto done
		case data := <-outputChan:
			if profile, ok := data.(*performance.ProfileStats); ok {
				totalEvents += int64(profile.SampleCount)
				totalDropped += profile.DroppedSamples
			}
		}
	}

done:
	dropRate := float64(totalDropped) / float64(totalEvents+int64(totalDropped)) * 100
	fmt.Printf("Events: %d, Dropped: %d (%.2f%%)\n", totalEvents, totalDropped, dropRate)
	
	if totalDropped > 0 {
		return fmt.Errorf("dropped %d events", totalDropped)
	}
	
	fmt.Printf("✓ PASSED\n")
	return nil
}

func getKernelVersion() string {
	data, _ := os.ReadFile("/proc/version")
	return string(data)
}
EOF

# Step 3: Build test binaries
print_status "Building test binaries..."

# Build for ARM64
if GOOS=linux GOARCH=arm64 go build -o "${BUILD_DIR}/test-ringbuf-arm64" "${BUILD_DIR}/test-ringbuf.go"; then
    print_success "ARM64 binary built"
else
    print_error "Failed to build ARM64 binary"
    exit 1
fi

# Build for AMD64
if GOOS=linux GOARCH=amd64 go build -o "${BUILD_DIR}/test-ringbuf-amd64" "${BUILD_DIR}/test-ringbuf.go"; then
    print_success "AMD64 binary built"
else
    print_error "Failed to build AMD64 binary"
    exit 1
fi

# Step 4: Test on each VM
for vm in $TEST_VMS; do
    print_status "Testing on VM: $vm"
    
    # Check if VM is running
    if ! limactl list | grep -q "^${vm}.*Running"; then
        print_status "Starting VM: $vm"
        limactl start "$vm" || {
            print_error "Failed to start VM: $vm"
            continue
        }
    fi
    
    # Get VM architecture
    vm_arch=$(limactl shell "$vm" uname -m 2>/dev/null)
    if [[ "$vm_arch" == "x86_64" ]]; then
        test_binary="test-ringbuf-amd64"
    else
        test_binary="test-ringbuf-arm64"
    fi
    
    # Get kernel version
    kernel_version=$(limactl shell "$vm" uname -r 2>/dev/null)
    print_status "VM: $vm, Kernel: $kernel_version, Arch: $vm_arch"
    
    # Deploy files
    print_status "Deploying test files..."
    limactl shell "$vm" mkdir -p /tmp/profiler-tests
    limactl copy "${BUILD_DIR}/${test_binary}" "${vm}:/tmp/profiler-tests/" 2>/dev/null
    limactl copy "${PROJECT_ROOT}/ebpf/build/profiler.bpf.o" "${vm}:/tmp/profiler-tests/" 2>/dev/null
    limactl shell "$vm" chmod +x "/tmp/profiler-tests/${test_binary}"
    
    # Run test
    print_status "Running profiler test..."
    if limactl shell "$vm" sudo ANTIMETAL_BPF_PATH=/tmp/profiler-tests \
        TEST_DURATION="${TEST_DURATION}" \
        "/tmp/profiler-tests/${test_binary}" 2>&1 | tee "${BUILD_DIR}/${vm}-test.log"; then
        print_success "Tests passed on $vm"
    else
        print_error "Tests failed on $vm"
        echo "Check log: ${BUILD_DIR}/${vm}-test.log"
    fi
    
    # Clean up VM
    limactl shell "$vm" rm -rf /tmp/profiler-tests
done

print_success "All tests completed!"

# Summary
echo -e "\n${GREEN}=== Test Summary ===${NC}"
for vm in $TEST_VMS; do
    if [[ -f "${BUILD_DIR}/${vm}-test.log" ]]; then
        events=$(grep -E "Events: [0-9]+" "${BUILD_DIR}/${vm}-test.log" | tail -1 | awk '{print $2}')
        drops=$(grep -E "Dropped: [0-9]+" "${BUILD_DIR}/${vm}-test.log" | tail -1 | awk '{print $4}')
        echo -e "${vm}: Events=${events}, Drops=${drops}"
    fi
done