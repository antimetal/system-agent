#!/bin/bash
set -e

echo "=== Packaging Hardware Tests for Ring Buffer Profiler ==="
echo ""

# Create test directory
TEST_DIR=/tmp/profiler-hardware-tests
rm -rf $TEST_DIR
mkdir -p $TEST_DIR

# Build all test binaries for Linux AMD64
echo "Building test binaries..."

# 1. Hardware tests
echo "  - Building hardware tests..."
GOOS=linux GOARCH=amd64 go test -c -tags=hardware \
    github.com/antimetal/agent/pkg/performance/collectors \
    -o $TEST_DIR/profiler-hardware-test

# 2. Integration tests  
echo "  - Building integration tests..."
GOOS=linux GOARCH=amd64 go test -c -tags=integration \
    github.com/antimetal/agent/pkg/performance/collectors \
    -o $TEST_DIR/profiler-integration-test

# 3. Build eBPF programs
echo "  - Building eBPF programs..."
make build-ebpf
cp -r ebpf/build $TEST_DIR/ebpf-build

# 4. Copy test scripts
echo "  - Copying test scripts..."
cp scripts/test-profiler-hardware.sh $TEST_DIR/
cp scripts/test-drop-counter.sh $TEST_DIR/
cp scripts/run-profiler-stability-test.sh $TEST_DIR/
cp scripts/profiler-test-dashboard.sh $TEST_DIR/

# 5. Create runner script
cat > $TEST_DIR/run-all-tests.sh << 'EOF'
#!/bin/bash
set -e

echo "=== Ring Buffer Profiler Hardware Test Suite ==="
echo ""
echo "System: $(uname -a)"
echo "CPU: $(lscpu | grep 'Model name' | cut -d: -f2 | xargs)"
echo ""

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

# Export BPF path
export ANTIMETAL_BPF_PATH=$(pwd)/ebpf-build

# Function to run test
run_test() {
    local name=$1
    local cmd=$2
    
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Running: $name"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    
    if eval $cmd; then
        echo -e "${GREEN}✓ $name passed${NC}"
    else
        echo -e "${RED}✗ $name failed${NC}"
        return 1
    fi
    echo ""
}

# Check for root
if [ "$EUID" -ne 0 ]; then 
    echo "Please run as root (required for eBPF)"
    exit 1
fi

# Tests to run
TESTS=(
    "Drop Counter Test|./profiler-integration-test -test.v -test.run=TestProfiler.*Drop"
    "Heavy Load Test|./profiler-hardware-test -test.v -test.run=TestProfilerRingBuffer_HeavyLoad"
    "Memory Stability|PROFILER_STABILITY_DURATION=2m ./profiler-hardware-test -test.v -test.run=TestProfilerRingBuffer_MemoryStability"
    "Wraparound Test|./profiler-hardware-test -test.v -test.run=TestProfilerRingBuffer_Wraparound"
    "Performance Bench|./profiler-hardware-test -test.v -test.run=TestProfilerRingBuffer_Performance"
)

FAILED=0
for test in "${TESTS[@]}"; do
    IFS='|' read -r name cmd <<< "$test"
    if ! run_test "$name" "$cmd"; then
        ((FAILED++))
    fi
done

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "           TEST SUMMARY            "
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
else
    echo -e "${RED}$FAILED test(s) failed${NC}"
    exit 1
fi
EOF

chmod +x $TEST_DIR/run-all-tests.sh

# 6. Create README
cat > $TEST_DIR/README.md << 'EOF'
# Ring Buffer Profiler Hardware Tests

This package contains comprehensive hardware tests for validating the ring buffer profiler implementation.

## Contents

- `profiler-hardware-test`: Hardware-specific tests (requires PMU)
- `profiler-integration-test`: Integration tests 
- `ebpf-build/`: Compiled eBPF programs
- `run-all-tests.sh`: Run complete test suite
- `test-profiler-hardware.sh`: Individual hardware test runner
- `test-drop-counter.sh`: Drop counter validation

## Requirements

- Linux kernel 5.8+ with BTF support
- Root access (for eBPF)
- PMU support (for hardware events)
- clang, llvm, bpftool installed

## Quick Start

```bash
# Run all tests
sudo ./run-all-tests.sh

# Run 24-hour stability test in tmux
./run-profiler-stability-test.sh 24h

# Monitor running tests with dashboard
./profiler-test-dashboard.sh

# Run specific test
sudo ./profiler-hardware-test -test.v -test.run=TestProfilerRingBuffer_HeavyLoad
```

## Long-Running Tests with Tmux

The stability test uses tmux for session management:

```bash
# Start 24-hour test
./run-profiler-stability-test.sh 24h

# Attach to running session
tmux attach -t profiler-stability-<timestamp>

# Monitor from dashboard
./profiler-test-dashboard.sh

# Detach from session (test continues)
Ctrl+b d

# List tmux sessions
tmux ls | grep profiler
```

### Tmux Windows
- **Window 1**: Main test execution
- **Window 2**: Live monitoring (updates every 30s)
- **Window 3**: Real-time log tail

### Dashboard Features
- Live test status monitoring
- Memory usage tracking
- Event statistics
- System resource usage
- Quick access to logs and tmux sessions

## Test Descriptions

### 1. Drop Counter Test
Validates that the ring buffer correctly counts dropped events under extreme load (10KHz).

### 2. Heavy Load Test  
Tests ring buffer stability at 10KHz sampling with all CPUs under load.

### 3. Memory Stability Test
Monitors for memory leaks over extended duration (default 5 min, configurable to 24h).

### 4. Wraparound Test
Validates correct handling when ring buffer wraps around.

### 5. Performance Benchmarks
Measures CPU overhead and event throughput at various sampling rates (100Hz to 10KHz).

## Expected Results

- **100Hz**: No drops, <1% CPU overhead
- **1KHz**: <0.1% drops, <5% CPU overhead  
- **10KHz**: Some drops expected (3-10%), <15% CPU overhead
- **Memory**: Stable at ~15-20MB total (BPF + Go)
- **Throughput**: >10K events/sec capability

## Troubleshooting

If tests fail:

1. Check kernel version: `uname -r` (need 5.8+)
2. Verify BTF support: `ls -la /sys/kernel/btf/vmlinux`
3. Check PMU: `ls /sys/bus/event_source/devices/`
4. Review dmesg for eBPF errors: `dmesg | tail -20`
EOF

# Create tarball
cd /tmp
tar czf profiler-hardware-tests.tar.gz profiler-hardware-tests/

echo ""
echo "✓ Test package created: /tmp/profiler-hardware-tests.tar.gz"
echo "  Size: $(ls -lh /tmp/profiler-hardware-tests.tar.gz | awk '{print $5}')"
echo ""
echo "To deploy to Hetzner:"
echo "  1. Copy: scp /tmp/profiler-hardware-tests.tar.gz TestServer:/tmp/"
echo "  2. Extract: ssh TestServer 'cd /tmp && tar xzf profiler-hardware-tests.tar.gz'"
echo "  3. Run: ssh TestServer 'cd /tmp/profiler-hardware-tests && sudo ./run-all-tests.sh'"
echo ""