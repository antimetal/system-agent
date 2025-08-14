#!/bin/bash
set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo "=== Ring Buffer Profiler Stability Test Runner ==="
echo ""

# Parse arguments
DURATION="${1:-24h}"
SESSION_NAME="profiler-stability-$(date +%Y%m%d-%H%M%S)"
LOG_DIR="/tmp/profiler-stability-logs"
mkdir -p $LOG_DIR

echo "Configuration:"
echo "  Test Duration: $DURATION"
echo "  Session Name: $SESSION_NAME"
echo "  Log Directory: $LOG_DIR"
echo ""

# Check for tmux
if ! command -v tmux &> /dev/null; then
    echo -e "${YELLOW}tmux not found. Installing...${NC}"
    if command -v apt-get &> /dev/null; then
        sudo apt-get update && sudo apt-get install -y tmux
    elif command -v yum &> /dev/null; then
        sudo yum install -y tmux
    else
        echo -e "${RED}Cannot install tmux automatically. Please install manually.${NC}"
        exit 1
    fi
fi

# Build test binary if needed
if [ ! -f /tmp/profiler-hardware-test ]; then
    echo "Building test binary..."
    GOOS=linux GOARCH=amd64 go test -c -tags=hardware \
        github.com/antimetal/agent/pkg/performance/collectors \
        -o /tmp/profiler-hardware-test
fi

# Create test runner script
cat > $LOG_DIR/stability-test.sh << EOF
#!/bin/bash
set -e

echo "=== Starting ${DURATION} Stability Test ==="
echo "Start time: \$(date)"
echo "PID: \$\$"
echo ""

# Save PID for monitoring
echo \$\$ > $LOG_DIR/test.pid

# Export duration
export PROFILER_STABILITY_DURATION="${DURATION}"

# Run the stability test
sudo -E /tmp/profiler-hardware-test \
    -test.v \
    -test.run="TestProfilerRingBuffer_MemoryStability_Hardware" \
    -test.timeout=25h \
    2>&1 | tee $LOG_DIR/stability-test.log

TEST_RESULT=\${PIPESTATUS[0]}

echo ""
echo "=== Test Completed ==="
echo "End time: \$(date)"
echo "Exit code: \$TEST_RESULT"

# Extract summary
if [ \$TEST_RESULT -eq 0 ]; then
    echo -e "${GREEN}✓ Stability test passed${NC}"
    
    # Extract key metrics
    echo ""
    echo "Key Metrics:"
    grep "Initial Heap:" $LOG_DIR/stability-test.log | tail -1
    grep "Final Heap:" $LOG_DIR/stability-test.log | tail -1
    grep "Heap Growth:" $LOG_DIR/stability-test.log | tail -1
    grep "Memory Trend:" $LOG_DIR/stability-test.log | tail -1
    grep "Total Events:" $LOG_DIR/stability-test.log | tail -1
else
    echo -e "${RED}✗ Stability test failed${NC}"
    echo "Check log at: $LOG_DIR/stability-test.log"
fi

# Clean up PID file
rm -f $LOG_DIR/test.pid
EOF

chmod +x $LOG_DIR/stability-test.sh

# Create monitoring script
cat > $LOG_DIR/monitor.sh << 'EOF'
#!/bin/bash

LOG_DIR="/tmp/profiler-stability-logs"
PID_FILE="$LOG_DIR/test.pid"

if [ ! -f "$PID_FILE" ]; then
    echo "Test not running (no PID file found)"
    exit 1
fi

TEST_PID=$(cat $PID_FILE)

if ! ps -p $TEST_PID > /dev/null; then
    echo "Test completed or crashed (PID $TEST_PID not found)"
    echo ""
    echo "Last 20 lines of log:"
    tail -20 $LOG_DIR/stability-test.log
    exit 1
fi

echo "=== Stability Test Monitor ==="
echo "Test PID: $TEST_PID"
echo "Monitoring every 30 seconds. Press Ctrl+C to stop monitoring (test continues)."
echo ""

while true; do
    if ! ps -p $TEST_PID > /dev/null; then
        echo "Test completed!"
        break
    fi
    
    # Get latest metrics from log
    echo -n "$(date '+%H:%M:%S') - "
    
    # Extract latest memory reading
    if [ -f "$LOG_DIR/stability-test.log" ]; then
        tail -100 $LOG_DIR/stability-test.log | grep "Memory -" | tail -1 || echo "Waiting for metrics..."
    fi
    
    sleep 30
done

echo ""
echo "Final results:"
tail -30 $LOG_DIR/stability-test.log | grep -A20 "=== Memory Stability Results ===" || echo "Results not yet available"
EOF

chmod +x $LOG_DIR/monitor.sh

# Create tmux session
echo -e "${GREEN}Starting tmux session: $SESSION_NAME${NC}"
echo ""

# Start tmux session with the test
tmux new-session -d -s "$SESSION_NAME" -n "stability-test" "$LOG_DIR/stability-test.sh"

# Add a monitoring window
tmux new-window -t "$SESSION_NAME:2" -n "monitor" "$LOG_DIR/monitor.sh"

# Add a log viewer window
tmux new-window -t "$SESSION_NAME:3" -n "logs" "tail -f $LOG_DIR/stability-test.log"

# Switch back to first window
tmux select-window -t "$SESSION_NAME:1"

echo "✓ Stability test started in tmux session"
echo ""
echo "Commands:"
echo "  Attach to session:  tmux attach -t $SESSION_NAME"
echo "  List windows:       Ctrl+b w"
echo "  Switch windows:     Ctrl+b <number>"
echo "  Detach:            Ctrl+b d"
echo "  Kill session:      tmux kill-session -t $SESSION_NAME"
echo ""
echo "Windows:"
echo "  1. stability-test - Main test execution"
echo "  2. monitor       - Live monitoring (updates every 30s)"
echo "  3. logs          - Real-time log viewer"
echo ""
echo "Logs:"
echo "  Main log:    $LOG_DIR/stability-test.log"
echo "  Monitor:     $LOG_DIR/monitor.sh"
echo "  PID file:    $LOG_DIR/test.pid"
echo ""

# Offer to attach immediately
read -p "Attach to session now? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    tmux attach -t "$SESSION_NAME"
else
    echo "Session running in background. Attach with:"
    echo "  tmux attach -t $SESSION_NAME"
fi