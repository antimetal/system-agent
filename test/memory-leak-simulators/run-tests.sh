#!/bin/bash
# Run memory leak tests locally and validate behavior

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}=== Memory Leak Simulator Test Suite ===${NC}"
echo "Testing leak detection thresholds"
echo

# Function to run a test and check its output
run_test() {
    local test_name=$1
    local binary=$2
    local args=$3
    local expected_pattern=$4
    
    echo -e "\n${YELLOW}Test: $test_name${NC}"
    echo "Command: ./$binary $args"
    echo "Expected: $expected_pattern"
    echo
    
    # Run test with timeout and capture output
    if timeout 30 ./$binary $args 2>&1 | tee test-output.tmp | grep -E "(Initial|Running|Final|THRESHOLD|EXCEEDED)"; then
        echo -e "${GREEN}✓ Test completed${NC}"
    else
        echo -e "${RED}✗ Test failed or timed out${NC}"
    fi
    
    # Check for expected patterns
    if grep -q "$expected_pattern" test-output.tmp; then
        echo -e "${GREEN}✓ Expected pattern found: $expected_pattern${NC}"
    else
        echo -e "${RED}✗ Expected pattern not found${NC}"
    fi
    
    rm -f test-output.tmp
    echo "----------------------------------------"
}

# Build all tests
echo "Building tests..."
make all

# Test 1: VSZ/RSS Divergence
run_test "VSZ/RSS Divergence (30 sec)" \
    "vsz_divergence" "30 5" \
    "Ratio: [2-9]\."

# Test 2: Monotonic Growth (shortened for demo)
run_test "Monotonic Growth (30 sec demo)" \
    "monotonic_growth" "30 2048" \
    "RSS:.*MB"

# Test 3: Anonymous Ratio
run_test "Anonymous Memory Ratio (30 sec)" \
    "anon_ratio" "30 100" \
    "Anonymous.*[8-9][0-9]\.[0-9]%"

# Test 4: Combined Leak (shortened)
run_test "Combined Leak (30 sec demo)" \
    "combined_leak" "30" \
    "Estimated confidence: [7-9][0-9]"

# Test 5: Cache Growth (false positive test)
run_test "Cache Growth - False Positive (30 sec)" \
    "cache_growth" "30" \
    "File-backed.*[6-9][0-9]\.[0-9]%"

echo
echo -e "${GREEN}=== Test Suite Complete ===${NC}"
echo
echo "Summary:"
echo "- VSZ/RSS Divergence: Tests allocation without use pattern"
echo "- Monotonic Growth: Tests continuous growth detection"
echo "- Anonymous Ratio: Tests heap-dominant memory pattern"
echo "- Combined Leak: Tests all thresholds together"
echo "- Cache Growth: Validates no false positives from file cache"
echo
echo "Next steps:"
echo "1. Deploy to Hetzner: ./deploy-to-hetzner.sh"
echo "2. Load eBPF monitors on target system"
echo "3. Run tests and observe confidence scores"