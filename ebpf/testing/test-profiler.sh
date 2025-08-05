#!/bin/bash
# Copyright Antimetal, Inc. All rights reserved.
#
# Test harness for eBPF profiler across different Lima VMs

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test results
PASSED=0
FAILED=0
SKIPPED=0

log() {
    echo -e "${BLUE}[$(date '+%Y-%m-%d %H:%M:%S')]${NC} $*"
}

success() {
    echo -e "${GREEN}✓${NC} $*"
    ((PASSED++))
}

error() {
    echo -e "${RED}✗${NC} $*"
    ((FAILED++))
}

skip() {
    echo -e "${YELLOW}⚠${NC} $* (skipped)"
    ((SKIPPED++))
}

# Check if Lima is installed
check_lima() {
    if ! command -v limactl &> /dev/null; then
        error "Lima not installed. Please install Lima first."
        exit 1
    fi
}

# Get list of running Lima VMs
get_lima_vms() {
    limactl list --format '{{.Name}}' 2>/dev/null | grep -E '^(ebpf-|ubuntu-|debian-)' || true
}

# Check if VM supports eBPF
check_ebpf_support() {
    local vm=$1
    
    log "Checking eBPF support on $vm..."
    
    # Check kernel version
    kernel_version=$(limactl shell "$vm" uname -r 2>/dev/null || echo "unknown")
    log "  Kernel version: $kernel_version"
    
    # Check for BTF support
    if limactl shell "$vm" test -f /sys/kernel/btf/vmlinux 2>/dev/null; then
        log "  BTF support: YES"
        echo "btf"
    else
        log "  BTF support: NO"
        echo "no-btf"
    fi
}

# Build profiler binary and BPF object
build_profiler() {
    log "Building profiler..."
    
    # Build eBPF object
    if ! make -C "$PROJECT_ROOT" build-ebpf; then
        error "Failed to build eBPF programs"
        return 1
    fi
    
    # Build test binary for Linux
    cd "$PROJECT_ROOT"
    if ! GOOS=linux GOARCH=amd64 go build -o "$PROJECT_ROOT/ebpf/testing/profiler-test-amd64" \
        "$PROJECT_ROOT/ebpf/testing/profiler-test.go"; then
        error "Failed to build AMD64 test binary"
        return 1
    fi
    
    if ! GOOS=linux GOARCH=arm64 go build -o "$PROJECT_ROOT/ebpf/testing/profiler-test-arm64" \
        "$PROJECT_ROOT/ebpf/testing/profiler-test.go"; then
        error "Failed to build ARM64 test binary"
        return 1
    fi
    
    success "Built profiler test binaries"
}

# Copy files to VM
copy_to_vm() {
    local vm=$1
    
    log "Copying files to $vm..."
    
    # Detect architecture
    arch=$(limactl shell "$vm" uname -m)
    if [[ "$arch" == "x86_64" ]]; then
        binary="profiler-test-amd64"
    else
        binary="profiler-test-arm64"
    fi
    
    # Copy binary
    limactl copy "$PROJECT_ROOT/ebpf/testing/$binary" "$vm:/tmp/profiler-test"
    limactl shell "$vm" chmod +x /tmp/profiler-test
    
    # Copy BPF object
    limactl copy "$PROJECT_ROOT/ebpf/build/profiler.bpf.o" "$vm:/tmp/"
    
    success "Files copied to $vm"
}

# Run basic profiler test
test_basic_profiling() {
    local vm=$1
    local btf_support=$2
    
    log "Testing basic CPU profiling on $vm..."
    
    # Run the test
    if limactl shell "$vm" sudo /tmp/profiler-test -mode basic -bpf-path /tmp/profiler.bpf.o 2>&1; then
        success "Basic CPU profiling works on $vm"
    else
        if [[ "$btf_support" == "no-btf" ]]; then
            skip "Basic CPU profiling failed on $vm (expected - no BTF)"
        else
            error "Basic CPU profiling failed on $vm"
        fi
    fi
}

# Test different event types
test_event_types() {
    local vm=$1
    local btf_support=$2
    
    log "Testing different event types on $vm..."
    
    local events=("cpu-cycles" "cache-misses" "branch-misses" "instructions")
    
    for event in "${events[@]}"; do
        if limactl shell "$vm" sudo /tmp/profiler-test -mode event -event "$event" -bpf-path /tmp/profiler.bpf.o 2>&1; then
            success "Event $event profiling works on $vm"
        else
            if [[ "$btf_support" == "no-btf" ]]; then
                skip "Event $event profiling failed on $vm (expected - no BTF)"
            else
                error "Event $event profiling failed on $vm"
            fi
        fi
    done
}

# Test profiler under load
test_under_load() {
    local vm=$1
    local btf_support=$2
    
    log "Testing profiler under CPU load on $vm..."
    
    # Start CPU load in background
    limactl shell "$vm" bash -c 'for i in {1..4}; do while :; do :; done & done' &
    load_pid=$!
    
    # Give it a moment to start
    sleep 2
    
    # Run profiler
    if limactl shell "$vm" sudo /tmp/profiler-test -mode load -duration 5s -bpf-path /tmp/profiler.bpf.o 2>&1; then
        success "Profiling under load works on $vm"
    else
        if [[ "$btf_support" == "no-btf" ]]; then
            skip "Profiling under load failed on $vm (expected - no BTF)"
        else
            error "Profiling under load failed on $vm"
        fi
    fi
    
    # Kill the load
    limactl shell "$vm" pkill -f "while : ; do : ; done" || true
    kill $load_pid 2>/dev/null || true
}

# Test stack trace collection
test_stack_traces() {
    local vm=$1
    local btf_support=$2
    
    log "Testing stack trace collection on $vm..."
    
    if limactl shell "$vm" sudo /tmp/profiler-test -mode stacks -bpf-path /tmp/profiler.bpf.o 2>&1; then
        success "Stack trace collection works on $vm"
    else
        if [[ "$btf_support" == "no-btf" ]]; then
            skip "Stack trace collection failed on $vm (expected - no BTF)"
        else
            error "Stack trace collection failed on $vm"
        fi
    fi
}

# Test performance overhead
test_overhead() {
    local vm=$1
    local btf_support=$2
    
    log "Testing profiler overhead on $vm..."
    
    if limactl shell "$vm" sudo /tmp/profiler-test -mode overhead -bpf-path /tmp/profiler.bpf.o 2>&1; then
        success "Overhead test passed on $vm"
    else
        if [[ "$btf_support" == "no-btf" ]]; then
            skip "Overhead test failed on $vm (expected - no BTF)"
        else
            error "Overhead test failed on $vm"
        fi
    fi
}

# Main test execution
main() {
    log "eBPF Profiler Test Harness"
    log "========================="
    
    check_lima
    
    # Build profiler
    if ! build_profiler; then
        error "Build failed, cannot continue"
        exit 1
    fi
    
    # Get VMs
    vms=$(get_lima_vms)
    if [[ -z "$vms" ]]; then
        error "No suitable Lima VMs found. Please start some VMs first."
        echo "Suggested VMs: ebpf-kernel-5.8, ebpf-modern, ubuntu-22.04"
        exit 1
    fi
    
    log "Found VMs: $(echo $vms | tr '\n' ' ')"
    echo
    
    # Test each VM
    for vm in $vms; do
        log "================================"
        log "Testing VM: $vm"
        log "================================"
        
        # Check eBPF support
        btf_support=$(check_ebpf_support "$vm")
        
        # Copy files
        copy_to_vm "$vm"
        
        # Run tests
        test_basic_profiling "$vm" "$btf_support"
        test_event_types "$vm" "$btf_support"
        test_under_load "$vm" "$btf_support"
        test_stack_traces "$vm" "$btf_support"
        test_overhead "$vm" "$btf_support"
        
        echo
    done
    
    # Summary
    log "================================"
    log "Test Summary"
    log "================================"
    echo -e "${GREEN}Passed:${NC} $PASSED"
    echo -e "${RED}Failed:${NC} $FAILED"
    echo -e "${YELLOW}Skipped:${NC} $SKIPPED"
    
    if [[ $FAILED -eq 0 ]]; then
        success "All tests passed!"
        exit 0
    else
        error "Some tests failed"
        exit 1
    fi
}

# Handle script arguments
if [[ $# -gt 0 && "$1" == "--help" ]]; then
    echo "Usage: $0"
    echo "Test eBPF profiler across Lima VMs"
    echo ""
    echo "This script will:"
    echo "  1. Build the profiler test binary and BPF objects"
    echo "  2. Copy them to all suitable Lima VMs"
    echo "  3. Run comprehensive profiling tests"
    echo "  4. Report results"
    exit 0
fi

main "$@"