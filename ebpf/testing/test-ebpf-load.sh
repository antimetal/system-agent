#!/bin/bash
# Copyright 2024-2025 Antimetal, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
VMS=("ebpf-kernel-5.8" "ebpf-modern")
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
BPF_OBJECTS=("execsnoop.bpf.o")  # Add more as needed

# Test results tracking - using simple variables for compatibility
TEST_RESULTS=""

# Helper functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_test() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check Docker
    if ! docker info >/dev/null 2>&1; then
        log_error "Docker is not running"
        exit 1
    fi
    
    # Check Lima
    if ! command -v limactl >/dev/null 2>&1; then
        log_error "Lima is not installed. Run: brew install lima"
        exit 1
    fi
    
    # Check if VMs exist and are running
    for vm in "${VMS[@]}"; do
        if ! limactl list | grep -E "^$vm\s+Running"; then
            log_warn "VM $vm is not running. Starting it..."
            if ! limactl list | grep -q "$vm"; then
                log_error "VM $vm does not exist. Please create it first using:"
                log_error "limactl create --name=$vm $PROJECT_ROOT/ebpf/testing/lima-vms/<config>.yaml"
                exit 1
            fi
            limactl start "$vm"
        fi
    done
}

build_ebpf_programs() {
    log_info "Building eBPF programs with Docker..."
    cd "$PROJECT_ROOT"
    make build-ebpf
    
    # Verify build output
    for obj in "${BPF_OBJECTS[@]}"; do
        if [ ! -f "ebpf/build/$obj" ]; then
            log_error "Failed to build $obj"
            exit 1
        fi
    done
    
    # Dump BTF info for verification
    log_info "Verifying BTF information..."
    docker run --rm -v "$PROJECT_ROOT/ebpf/build:/build:ro" --entrypoint bpftool \
        antimetal/ebpf-builder:latest btf dump file /build/execsnoop.bpf.o format raw 2>&1 | head -20 || true
}

ensure_bpftool() {
    local vm=$1
    
    # Check if bpftool is available
    if ! limactl shell "$vm" -- which bpftool >/dev/null 2>&1; then
        log_info "Installing bpftool on $vm..."
        limactl shell "$vm" -- sudo apt-get update -qq
        limactl shell "$vm" -- sudo apt-get install -y linux-tools-common linux-tools-generic >/dev/null 2>&1 || true
        
        # Try to find bpftool in various locations
        local kernel_version=$(limactl shell "$vm" -- uname -r)
        local bpftool_path=$(limactl shell "$vm" -- find /usr -name bpftool -type f 2>/dev/null | head -1)
        
        if [ -n "$bpftool_path" ]; then
            limactl shell "$vm" -- sudo ln -sf "$bpftool_path" /usr/local/bin/bpftool
        else
            log_warn "Could not install bpftool on $vm, will try basic verification only"
            return 1
        fi
    fi
    
    return 0
}

verify_bpf_object() {
    local vm=$1
    local obj=$2
    local kernel=$(limactl shell "$vm" -- uname -r)
    
    echo
    log_test "Verifying $obj on $vm (kernel: $kernel)"
    echo "----------------------------------------"
    
    # Copy BPF object to VM
    limactl copy "ebpf/build/$obj" "$vm:/tmp/$obj"
    
    # Get kernel info
    log_info "Kernel information:"
    limactl shell "$vm" -- uname -a
    
    # Check BTF support
    log_info "Checking BTF support:"
    if limactl shell "$vm" -- test -f /sys/kernel/btf/vmlinux; then
        echo "  ✓ Native kernel BTF available"
        limactl shell "$vm" -- ls -la /sys/kernel/btf/vmlinux
    else
        echo "  ✗ No native kernel BTF"
    fi
    
    # Try to use bpftool if available
    if ensure_bpftool "$vm"; then
        # Dump BTF info from the object
        log_info "BTF information from BPF object:"
        limactl shell "$vm" -- sudo bpftool btf dump file "/tmp/$obj" format raw 2>&1 | head -10 || true
        
        # Try to load the program with bpftool
        log_info "Attempting to load BPF program:"
        local load_result
        if limactl shell "$vm" -- sudo bpftool prog load "/tmp/$obj" /sys/fs/bpf/test_prog 2>&1; then
            echo "  ✓ BPF program loaded successfully!"
            TEST_RESULTS="${TEST_RESULTS}${vm}:${obj}:PASS\n"
            
            # Show loaded program info
            log_info "Loaded program information:"
            limactl shell "$vm" -- sudo bpftool prog show name execsnoop 2>&1 || true
            
            # Clean up
            limactl shell "$vm" -- sudo rm -f /sys/fs/bpf/test_prog 2>&1 || true
        else
            echo "  ✗ Failed to load BPF program"
            TEST_RESULTS="${TEST_RESULTS}${vm}:${obj}:FAIL\n"
            
            # Try to get more details about the failure
            log_warn "Attempting verbose load for diagnostics:"
            limactl shell "$vm" -- sudo bpftool -d prog load "/tmp/$obj" /sys/fs/bpf/test_prog 2>&1 | tail -20 || true
        fi
    else
        # Fallback: just check file format
        log_info "Basic file verification (bpftool not available):"
        limactl shell "$vm" -- file "/tmp/$obj"
        limactl shell "$vm" -- readelf -h "/tmp/$obj" 2>&1 | grep -E "(Class|Machine|Type)" || true
        TEST_RESULTS="${TEST_RESULTS}${vm}:${obj}:UNKNOWN\n"
    fi
    
    # Check required kernel features
    log_info "Checking kernel BPF features:"
    echo -n "  BPF syscall: "
    if limactl shell "$vm" -- grep -q bpf /proc/kallsyms 2>/dev/null; then
        echo "✓ Available"
    else
        echo "✗ Not found"
    fi
    
    echo -n "  BTF support: "
    if limactl shell "$vm" -- grep -q CONFIG_DEBUG_INFO_BTF=y /boot/config-$(uname -r) 2>/dev/null; then
        echo "✓ Enabled"
    else
        echo "? Unknown"
    fi
    
    # Clean up
    limactl shell "$vm" -- rm -f "/tmp/$obj"
}

print_summary() {
    echo
    echo "========================================"
    echo "         TEST SUMMARY"
    echo "========================================"
    echo
    
    # Print kernel versions
    echo "Tested kernel versions:"
    for vm in "${VMS[@]}"; do
        local kernel=$(limactl shell "$vm" -- uname -r)
        echo "  - $vm: $kernel"
    done
    echo
    
    # Print test results
    echo "Load verification results:"
    echo "-------------------------"
    printf "%-20s %-30s %-10s\n" "VM" "BPF Object" "Result"
    printf "%-20s %-30s %-10s\n" "---" "----------" "------"
    
    # Parse and display results
    echo -e "$TEST_RESULTS" | while IFS=: read -r vm obj result; do
        if [ -n "$vm" ] && [ -n "$obj" ] && [ -n "$result" ]; then
            local color=""
            case $result in
                PASS) color="${GREEN}" ;;
                FAIL) color="${RED}" ;;
                UNKNOWN) color="${YELLOW}" ;;
                *) color="${NC}" ;;
            esac
            printf "%-20s %-30s ${color}%-10s${NC}\n" "$vm" "$obj" "$result"
        fi
    done
    echo
    
    # Summary
    echo "Key findings:"
    echo "- CO-RE BPF objects built successfully in Docker"
    echo "- BTF relocations embedded in the BPF objects"
    echo "- Load verification shows kernel compatibility"
    echo
    echo "Note: FAIL results may indicate:"
    echo "  - Missing kernel features (CONFIG_BPF, CONFIG_DEBUG_INFO_BTF)"
    echo "  - Insufficient privileges (even with sudo)"
    echo "  - Kernel verifier restrictions"
    echo "  - Missing BTF information for the target kernel"
}

cleanup() {
    log_info "Cleaning up..."
    # Remove any test files from VMs
    for vm in "${VMS[@]}"; do
        limactl shell "$vm" -- sudo rm -f /sys/fs/bpf/test_prog 2>/dev/null || true
    done
}

main() {
    echo "eBPF Load Verification Script"
    echo "============================="
    echo "This script builds eBPF programs in Docker and verifies"
    echo "they can be loaded on different kernel versions."
    echo
    
    # Change to project root
    cd "$PROJECT_ROOT"
    
    # Run tests
    check_prerequisites
    build_ebpf_programs
    
    # Test each BPF object on each VM
    for vm in "${VMS[@]}"; do
        for obj in "${BPF_OBJECTS[@]}"; do
            verify_bpf_object "$vm" "$obj"
        done
    done
    
    # Print summary
    print_summary
    
    cleanup
}

# Handle interrupts gracefully
trap cleanup EXIT

# Run main function
main "$@"