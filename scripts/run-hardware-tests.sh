#!/bin/bash
# Copyright Antimetal, Inc. All rights reserved.
#
# Hardware Test Runner for Hetzner Bare Metal Servers
# This script builds and runs hardware-specific tests that require PMU access

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
TEST_DIR="/tmp/system-agent-hardware-tests"
RESULTS_DIR="${TEST_DIR}/results"
LOG_FILE="${RESULTS_DIR}/hardware-test-$(date +%Y%m%d-%H%M%S).log"

# Architecture detection
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then
    GOARCH="amd64"
elif [ "$ARCH" = "aarch64" ]; then
    GOARCH="arm64"
else
    echo -e "${RED}Unsupported architecture: $ARCH${NC}"
    exit 1
fi

# Function to print colored output
print_status() {
    echo -e "${GREEN}[*]${NC} $1"
}

print_error() {
    echo -e "${RED}[!]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[!]${NC} $1"
}

# Check if running as root
check_root() {
    if [ "$EUID" -ne 0 ]; then
        print_error "This script must be run as root for hardware access"
        exit 1
    fi
}

# Verify hardware environment
verify_hardware() {
    print_status "Verifying hardware environment..."
    
    # Check for PMU support
    if [ ! -d "/sys/bus/event_source/devices/cpu" ]; then
        print_error "No PMU support detected - this requires bare metal hardware"
        exit 1
    fi
    
    # Check virtualization
    if systemd-detect-virt &>/dev/null; then
        VIRT=$(systemd-detect-virt)
        if [ "$VIRT" != "none" ]; then
            print_warning "Virtualization detected: $VIRT"
            print_warning "Hardware tests may not work correctly"
        fi
    fi
    
    # Check perf_event_paranoid
    PARANOID=$(cat /proc/sys/kernel/perf_event_paranoid)
    print_status "perf_event_paranoid level: $PARANOID"
    if [ "$PARANOID" -gt 1 ]; then
        print_warning "High paranoid level may restrict profiling"
        print_status "Setting perf_event_paranoid to -1 for testing..."
        echo -1 > /proc/sys/kernel/perf_event_paranoid
    fi
    
    # Display CPU info
    print_status "CPU Information:"
    lscpu | grep -E "Model name|CPU\(s\)|Thread|Core|Socket|NUMA" | sed 's/^/  /'
    
    # Check available PMU events
    print_status "Available PMU events:"
    if [ -d "/sys/bus/event_source/devices/cpu/events" ]; then
        ls /sys/bus/event_source/devices/cpu/events/ | head -10 | sed 's/^/  /'
        EVENT_COUNT=$(ls /sys/bus/event_source/devices/cpu/events/ | wc -l)
        echo "  ... total $EVENT_COUNT events available"
    fi
}

# Setup test environment
setup_environment() {
    print_status "Setting up test environment..."
    
    # Create test directory
    mkdir -p "$TEST_DIR"
    mkdir -p "$RESULTS_DIR"
    
    # Install dependencies if needed
    if ! command -v go &>/dev/null; then
        print_status "Installing Go..."
        wget -q -O /tmp/go.tar.gz "https://go.dev/dl/go1.24.0.linux-${GOARCH}.tar.gz"
        tar -C /usr/local -xzf /tmp/go.tar.gz
        export PATH=/usr/local/go/bin:$PATH
    fi
    
    # Install performance tools
    if ! command -v perf &>/dev/null; then
        print_status "Installing perf tools..."
        if command -v apt-get &>/dev/null; then
            apt-get update && apt-get install -y linux-tools-generic
        elif command -v yum &>/dev/null; then
            yum install -y perf
        fi
    fi
}

# Build test binaries
build_tests() {
    print_status "Building hardware test binaries..."
    
    cd "$(dirname "$0")/.."
    
    # Build eBPF objects first
    print_status "Building eBPF objects..."
    make build-ebpf || {
        print_error "Failed to build eBPF objects"
        exit 1
    }
    
    # Copy eBPF objects
    cp -r ebpf/build/*.o "$TEST_DIR/"
    
    # Build hardware tests
    print_status "Building hardware integration tests..."
    GOOS=linux GOARCH="$GOARCH" go test -c -tags=hardware \
        -o "$TEST_DIR/hardware-tests" \
        ./pkg/performance/collectors || {
        print_error "Failed to build hardware tests"
        exit 1
    }
    
    print_status "Test binaries built successfully"
}

# Run hardware tests
run_tests() {
    print_status "Running hardware tests..."
    
    cd "$TEST_DIR"
    
    # Set environment for BPF object loading
    export ANTIMETAL_BPF_PATH="$TEST_DIR"
    
    # Run tests with various verbosity levels
    print_status "Running PMU enumeration tests..."
    ./hardware-tests -test.v -test.run "TestPMU" 2>&1 | tee -a "$LOG_FILE"
    
    print_status "Running hardware profiler tests..."
    ./hardware-tests -test.v -test.run "TestHardwareProfiler" 2>&1 | tee -a "$LOG_FILE"
    
    # Run benchmarks
    print_status "Running performance benchmarks..."
    ./hardware-tests -test.bench "BenchmarkHardware" -test.benchtime=10s 2>&1 | tee -a "$LOG_FILE"
    
    # Run stress tests
    print_status "Running stress tests (this may take a while)..."
    ./hardware-tests -test.v -test.run "TestHardwareProfiler_Stress" -test.timeout=5m 2>&1 | tee -a "$LOG_FILE"
}

# Collect system information
collect_system_info() {
    print_status "Collecting system information..."
    
    SYSINFO_FILE="${RESULTS_DIR}/system-info.txt"
    
    {
        echo "=== System Information ==="
        echo "Date: $(date)"
        echo "Hostname: $(hostname)"
        echo "Kernel: $(uname -r)"
        echo "Architecture: $ARCH"
        echo ""
        
        echo "=== CPU Information ==="
        lscpu
        echo ""
        
        echo "=== Memory Information ==="
        free -h
        echo ""
        
        echo "=== PMU Capabilities ==="
        if [ -d "/sys/bus/event_source/devices/cpu" ]; then
            echo "PMU Type: $(cat /sys/bus/event_source/devices/cpu/type)"
            echo "PMU Caps:"
            if [ -d "/sys/bus/event_source/devices/cpu/caps" ]; then
                for cap in /sys/bus/event_source/devices/cpu/caps/*; do
                    echo "  $(basename $cap): $(cat $cap)"
                done
            fi
        fi
        echo ""
        
        echo "=== Kernel Config (BPF/Perf) ==="
        if [ -f "/boot/config-$(uname -r)" ]; then
            grep -E "CONFIG_BPF|CONFIG_PERF|CONFIG_HAVE_EBPF" "/boot/config-$(uname -r)" | head -20
        fi
        
    } > "$SYSINFO_FILE"
    
    print_status "System information saved to $SYSINFO_FILE"
}

# Generate test report
generate_report() {
    print_status "Generating test report..."
    
    REPORT_FILE="${RESULTS_DIR}/test-report.md"
    
    {
        echo "# Hardware Test Report"
        echo ""
        echo "## Test Environment"
        echo "- Date: $(date)"
        echo "- Hostname: $(hostname)"
        echo "- Kernel: $(uname -r)"
        echo "- Architecture: $ARCH"
        echo ""
        
        echo "## Test Results Summary"
        echo ""
        
        # Parse test results from log
        if grep -q "PASS" "$LOG_FILE"; then
            echo "✅ **Overall Status: PASSED**"
        else
            echo "❌ **Overall Status: FAILED**"
        fi
        echo ""
        
        echo "### Test Statistics"
        echo '```'
        grep -E "PASS|FAIL|SKIP" "$LOG_FILE" | sort | uniq -c
        echo '```'
        echo ""
        
        echo "### Benchmark Results"
        echo '```'
        grep -E "Benchmark" "$LOG_FILE" | tail -20
        echo '```'
        echo ""
        
        echo "### PMU Events Detected"
        echo '```'
        grep -E "Found.*events" "$LOG_FILE" | head -10
        echo '```'
        echo ""
        
        echo "## Recommendations"
        echo ""
        if grep -q "FAIL" "$LOG_FILE"; then
            echo "- Review failed tests in the detailed log"
            echo "- Check kernel configuration for missing features"
            echo "- Verify PMU access permissions"
        else
            echo "- All hardware tests passed successfully"
            echo "- System is properly configured for eBPF profiling"
        fi
        
    } > "$REPORT_FILE"
    
    print_status "Test report saved to $REPORT_FILE"
}

# Package results for download
package_results() {
    print_status "Packaging test results..."
    
    PACKAGE_FILE="/tmp/hardware-test-results-$(date +%Y%m%d-%H%M%S).tar.gz"
    
    cd "$TEST_DIR"
    tar -czf "$PACKAGE_FILE" results/
    
    print_status "Results packaged: $PACKAGE_FILE"
    echo ""
    echo "To download results:"
    echo "  scp root@$(hostname -I | awk '{print $1}'):$PACKAGE_FILE ./"
}

# Main execution
main() {
    echo "========================================"
    echo "System Agent Hardware Test Runner"
    echo "========================================"
    echo ""
    
    check_root
    verify_hardware
    setup_environment
    build_tests
    
    # Run tests and collect results
    run_tests
    collect_system_info
    generate_report
    package_results
    
    echo ""
    echo "========================================"
    echo "Hardware testing completed!"
    echo "Results: ${RESULTS_DIR}"
    echo "========================================"
}

# Handle cleanup on exit
cleanup() {
    # Restore perf_event_paranoid if we changed it
    if [ -f "/proc/sys/kernel/perf_event_paranoid" ]; then
        echo 1 > /proc/sys/kernel/perf_event_paranoid 2>/dev/null || true
    fi
}

trap cleanup EXIT

# Run main function
main "$@"