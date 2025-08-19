#!/bin/bash
# Deploy memory leak tests to Hetzner server

set -e

HETZNER_HOST="${HETZNER_HOST:-TestServer}"
REMOTE_DIR="/tmp/memory-leak-tests"

echo "=== Deploying Memory Leak Tests to Hetzner ==="
echo "Host: $HETZNER_HOST"
echo "Remote dir: $REMOTE_DIR"
echo

# Build for x86_64
echo "Building tests for x86_64..."
make clean
make build-x64

# Create deployment package
echo "Creating deployment package..."
cat > run-on-hetzner.sh << 'EOF'
#!/bin/bash
# Run memory leak tests on Hetzner with eBPF monitoring

set -e

TEST_DIR="/tmp/memory-leak-tests"
EBPF_DIR="/root/system-agent"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}=== Memory Leak Test Suite ===${NC}"
echo "Starting at $(date)"
echo

# Check if eBPF programs are loaded
check_ebpf() {
    echo "Checking eBPF programs..."
    if bpftool prog list | grep -q "memgrowth"; then
        echo -e "${GREEN}✓ eBPF memory monitors loaded${NC}"
        return 0
    else
        echo -e "${YELLOW}⚠ eBPF monitors not loaded, attempting to load...${NC}"
        return 1
    fi
}

# Load eBPF programs
load_ebpf() {
    if [ -d "$EBPF_DIR" ]; then
        cd "$EBPF_DIR"
        
        # Compile if needed
        if [ ! -f "ebpf/memgrowth_thresholds.o" ]; then
            echo "Compiling eBPF programs..."
            make build-ebpf
        fi
        
        # Load the threshold-based monitor
        echo "Loading memgrowth_thresholds..."
        bpftool prog load ebpf/memgrowth_thresholds.o /sys/fs/bpf/memgrowth_thresholds
        
        # Attach to tracepoints
        echo "Attaching to tracepoints..."
        # TODO: Add attachment commands
    else
        echo -e "${RED}✗ System agent directory not found${NC}"
        exit 1
    fi
}

# Monitor eBPF output in background
start_monitoring() {
    echo "Starting eBPF monitor in background..."
    
    # Clear previous trace
    echo > /sys/kernel/debug/tracing/trace
    
    # Start monitoring
    cat /sys/kernel/debug/tracing/trace_pipe | grep -E "confidence|HIGH RISK|THRESHOLD" > ebpf-output.log &
    MONITOR_PID=$!
    echo "Monitor PID: $MONITOR_PID"
}

# Run a test and check results
run_test() {
    local test_name=$1
    local test_binary=$2
    local test_args=$3
    local expected_confidence=$4
    
    echo -e "\n${YELLOW}=== Running: $test_name ===${NC}"
    echo "Binary: $test_binary"
    echo "Args: $test_args"
    echo "Expected confidence: $expected_confidence"
    echo
    
    # Clear eBPF output
    echo > ebpf-output.log
    
    # Run the test
    ./${test_binary} ${test_args} &
    TEST_PID=$!
    
    echo "Test PID: $TEST_PID"
    echo "Waiting for test to complete..."
    
    # Wait for test or timeout
    timeout 500 tail --pid=$TEST_PID -f /dev/null
    
    # Give eBPF time to process final events
    sleep 5
    
    # Check results
    echo -e "\n${GREEN}Results:${NC}"
    
    # Extract confidence scores from eBPF output
    if grep -q "HIGH RISK" ebpf-output.log; then
        echo -e "${GREEN}✓ High risk detected${NC}"
        grep "confidence" ebpf-output.log | tail -5
    else
        echo -e "${YELLOW}⚠ No high risk events${NC}"
    fi
    
    # Show threshold triggers
    if grep -q "THRESHOLD" ebpf-output.log; then
        echo -e "\n${GREEN}Thresholds triggered:${NC}"
        grep "THRESHOLD" ebpf-output.log | sort -u
    fi
    
    echo
    echo "----------------------------------------"
}

# Main test execution
cd $TEST_DIR

# Check/load eBPF
if ! check_ebpf; then
    load_ebpf
fi

# Start monitoring
start_monitoring

# Run tests
echo -e "\n${GREEN}Starting test suite...${NC}\n"

# Test 1: VSZ/RSS Divergence
run_test "VSZ/RSS Divergence" "vsz_divergence_x64" "120 5" "60-70"

# Test 2: Monotonic Growth (shorter for demo)
run_test "Monotonic Growth" "monotonic_growth_x64" "360 1024" "70-80"

# Test 3: Anonymous Ratio
run_test "Anonymous Ratio" "anon_ratio_x64" "120 150" "65-75"

# Test 4: Combined (all thresholds)
run_test "Combined Leak" "combined_leak_x64" "360" "95-100"

# Test 5: Cache Growth (false positive test)
run_test "Cache Growth (Should NOT trigger)" "cache_growth_x64" "120" "<20"

# Stop monitoring
kill $MONITOR_PID 2>/dev/null || true

# Summary
echo -e "\n${GREEN}=== Test Suite Complete ===${NC}"
echo "eBPF output saved to: ebpf-output.log"
echo "Completed at $(date)"

# Show final statistics
echo -e "\n${GREEN}Final eBPF Statistics:${NC}"
bpftool map dump name process_states 2>/dev/null | head -20 || true
EOF

chmod +x run-on-hetzner.sh

# Package everything
tar czf memory-leak-tests.tar.gz *_x64 run-on-hetzner.sh README.md

# Deploy to Hetzner
echo "Deploying to $HETZNER_HOST..."
ssh "$HETZNER_HOST" "mkdir -p $REMOTE_DIR"
scp memory-leak-tests.tar.gz "$HETZNER_HOST:$REMOTE_DIR/"

# Extract and prepare
echo "Extracting on remote..."
ssh "$HETZNER_HOST" "cd $REMOTE_DIR && tar xzf memory-leak-tests.tar.gz"

echo
echo "=== Deployment Complete ==="
echo
echo "To run tests on Hetzner:"
echo "  ssh $HETZNER_HOST"
echo "  cd $REMOTE_DIR"
echo "  sudo ./run-on-hetzner.sh"
echo
echo "To monitor eBPF output:"
echo "  ssh $HETZNER_HOST 'sudo cat /sys/kernel/debug/tracing/trace_pipe | grep memgrowth'"
echo