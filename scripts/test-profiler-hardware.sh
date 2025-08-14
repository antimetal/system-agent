#!/bin/bash
set -e

echo "=== Ring Buffer Profiler Hardware Test Suite ==="
echo ""
echo "This script runs comprehensive hardware tests for the ring buffer profiler"
echo "Best run on bare metal systems with PMU support (e.g., Hetzner servers)"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if running on Linux
if [[ "$OSTYPE" != "linux-gnu"* ]]; then
    echo -e "${YELLOW}WARNING: Not running on Linux. Some tests may not work correctly.${NC}"
fi

# Check for PMU support
check_pmu() {
    if [ -d "/sys/bus/event_source/devices/cpu" ]; then
        echo -e "${GREEN}✓ PMU support detected${NC}"
        return 0
    else
        echo -e "${YELLOW}⚠ PMU not detected - some tests may be limited${NC}"
        return 1
    fi
}

# Parse command line arguments
TEST_DURATION="${1:-5m}"
STABILITY_DURATION="${2:-5m}"

echo "Configuration:"
echo "  Heavy Load Test Duration: $TEST_DURATION"
echo "  Stability Test Duration: $STABILITY_DURATION"
echo ""

# Check system
echo "System Information:"
echo "  Kernel: $(uname -r)"
echo "  CPU: $(lscpu | grep 'Model name' | cut -d: -f2 | xargs)"
echo "  Cores: $(nproc)"
echo "  Memory: $(free -h | grep Mem | awk '{print $2}')"
check_pmu
echo ""

# Build the test binary
echo "Building hardware tests..."
GOOS=linux GOARCH=$(go env GOARCH) go test -c -tags=hardware \
    github.com/antimetal/agent/pkg/performance/collectors \
    -o /tmp/profiler-hardware-test

if [ ! -f /tmp/profiler-hardware-test ]; then
    echo -e "${RED}✗ Failed to build test binary${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Test binary built successfully${NC}"
echo ""

# Function to run a test
run_test() {
    local test_name=$1
    local test_func=$2
    local duration=$3
    
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Running: $test_name"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    
    if [ -n "$duration" ]; then
        export PROFILER_STABILITY_DURATION="$duration"
    fi
    
    sudo -E /tmp/profiler-hardware-test -test.v -test.run="$test_func" -test.timeout=30m 2>&1 | tee "/tmp/${test_func}.log"
    
    if [ ${PIPESTATUS[0]} -eq 0 ]; then
        echo -e "${GREEN}✓ $test_name completed successfully${NC}"
    else
        echo -e "${RED}✗ $test_name failed${NC}"
        return 1
    fi
    echo ""
}

# Run tests
FAILED_TESTS=()

# 1. Heavy Load Test (10KHz sampling)
if ! run_test "Heavy Load Test (10KHz)" "TestProfilerRingBuffer_HeavyLoad_Hardware" ""; then
    FAILED_TESTS+=("Heavy Load")
fi

# 2. Memory Stability Test
if ! run_test "Memory Stability Test" "TestProfilerRingBuffer_MemoryStability_Hardware" "$STABILITY_DURATION"; then
    FAILED_TESTS+=("Memory Stability")
fi

# 3. Wraparound Test
if ! run_test "Ring Buffer Wraparound Test" "TestProfilerRingBuffer_Wraparound_Hardware" ""; then
    FAILED_TESTS+=("Wraparound")
fi

# 4. Performance Benchmarks
if ! run_test "Performance Benchmarks" "TestProfilerRingBuffer_Performance_Hardware" ""; then
    FAILED_TESTS+=("Performance")
fi

# Summary
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "                 SUMMARY                 "
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ ${#FAILED_TESTS[@]} -eq 0 ]; then
    echo -e "${GREEN}✓ All tests passed successfully!${NC}"
    echo ""
    echo "Key Results:"
    
    # Extract key metrics from logs
    if [ -f /tmp/TestProfilerRingBuffer_HeavyLoad_Hardware.log ]; then
        echo "Heavy Load (10KHz):"
        grep "Event Rate:" /tmp/TestProfilerRingBuffer_HeavyLoad_Hardware.log | tail -1 | sed 's/^/  /'
        grep "Drop Rate:" /tmp/TestProfilerRingBuffer_HeavyLoad_Hardware.log | tail -1 | sed 's/^/  /'
    fi
    
    if [ -f /tmp/TestProfilerRingBuffer_MemoryStability_Hardware.log ]; then
        echo "Memory Stability:"
        grep "Heap Growth:" /tmp/TestProfilerRingBuffer_MemoryStability_Hardware.log | tail -1 | sed 's/^/  /'
        grep "Memory Trend:" /tmp/TestProfilerRingBuffer_MemoryStability_Hardware.log | tail -1 | sed 's/^/  /'
    fi
    
    if [ -f /tmp/TestProfilerRingBuffer_Performance_Hardware.log ]; then
        echo "Performance:"
        grep "CPU Overhead:" /tmp/TestProfilerRingBuffer_Performance_Hardware.log | sed 's/^/  /'
    fi
else
    echo -e "${RED}✗ Some tests failed:${NC}"
    for test in "${FAILED_TESTS[@]}"; do
        echo "  - $test"
    done
    exit 1
fi

echo ""
echo "Test logs saved in /tmp/*.log"
echo ""

# Offer to run extended stability test
if [ "$STABILITY_DURATION" != "24h" ]; then
    echo "To run a 24-hour stability test, use:"
    echo "  $0 5m 24h"
fi