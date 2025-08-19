#!/bin/bash
# Quick test runner for memory leak simulators

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}=== Memory Leak Simulator Quick Tests ===${NC}"
echo "Running abbreviated tests (5-10 seconds each)"
echo

# Function to run a test with timeout
run_test() {
    local test_name=$1
    local binary=$2
    local args=$3
    local timeout_sec=$4
    
    echo -e "\n${YELLOW}Test: $test_name${NC}"
    echo "Command: ./$binary $args"
    echo "Timeout: ${timeout_sec}s"
    echo
    
    # Run with timeout, capture both stdout and stderr
    if timeout $timeout_sec ./$binary $args 2>&1; then
        echo -e "${GREEN}✓ Test completed${NC}"
    else
        local exit_code=$?
        if [ $exit_code -eq 124 ]; then
            echo -e "${YELLOW}⚠ Test timed out (expected for some tests)${NC}"
        else
            echo -e "${RED}✗ Test failed with exit code $exit_code${NC}"
        fi
    fi
    
    echo "----------------------------------------"
}

# Build if needed
if [ ! -f vsz_divergence ]; then
    echo "Building tests..."
    make all
fi

# Test 1: VSZ/RSS Divergence (quick version)
echo -e "\n${BLUE}=== Test 1: VSZ/RSS Divergence ===${NC}"
echo "Expected: VSZ grows much faster than RSS"
echo "Target: VSZ/RSS ratio > 2.0"
run_test "VSZ/RSS Divergence" "vsz_divergence" "5 5" 8

# Test 2: Monotonic Growth (abbreviated)
echo -e "\n${BLUE}=== Test 2: Monotonic Growth ===${NC}"
echo "Expected: Steady RSS growth without drops"
echo "Target: Continuous growth pattern"
run_test "Monotonic Growth" "monotonic_growth" "10 1024" 12

# Test 3: Anonymous Ratio
echo -e "\n${BLUE}=== Test 3: Anonymous Memory Ratio ===${NC}"
echo "Expected: High heap allocation"
echo "Target: Anonymous ratio > 80%"
run_test "Anonymous Ratio" "anon_ratio" "10 50" 12

# Test 4: Combined Leak (abbreviated)
echo -e "\n${BLUE}=== Test 4: Combined Leak ===${NC}"
echo "Expected: All thresholds triggered"
echo "Target: Maximum confidence score"
run_test "Combined Leak" "combined_leak" "15" 18

# Test 5: Cache Growth (false positive test)
echo -e "\n${BLUE}=== Test 5: Cache Growth (False Positive) ===${NC}"
echo "Expected: File-backed growth, NO leak detection"
echo "Target: Low confidence score"
run_test "Cache Growth" "cache_growth" "10" 12

echo
echo -e "${GREEN}=== Quick Test Suite Complete ===${NC}"
echo
echo "Summary:"
echo "- VSZ/RSS Divergence: Tests allocation without use"
echo "- Monotonic Growth: Tests continuous growth"
echo "- Anonymous Ratio: Tests heap-dominant pattern"
echo "- Combined Leak: Tests all thresholds"
echo "- Cache Growth: Validates no false positives"
echo
echo "Note: These are abbreviated tests (5-15s each)."
echo "For full testing, use the parameters in run-tests.sh"