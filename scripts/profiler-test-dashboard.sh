#!/bin/bash

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

echo "=== Ring Buffer Profiler Test Dashboard ==="
echo ""

# Function to check if test is running
check_test_status() {
    local pid_file=$1
    local test_name=$2
    
    if [ ! -f "$pid_file" ]; then
        echo -e "${RED}✗${NC} $test_name: Not running"
        return 1
    fi
    
    local pid=$(cat $pid_file)
    if ps -p $pid > /dev/null; then
        local runtime=$(ps -o etime= -p $pid | xargs)
        echo -e "${GREEN}✓${NC} $test_name: Running (PID: $pid, Time: $runtime)"
        return 0
    else
        echo -e "${YELLOW}⚠${NC} $test_name: Completed or stopped"
        return 2
    fi
}

# Function to show memory usage
show_memory_usage() {
    local log_file=$1
    if [ -f "$log_file" ]; then
        local last_mem=$(tail -100 "$log_file" | grep "Memory -" | tail -1)
        if [ -n "$last_mem" ]; then
            echo "    Last: $last_mem"
        fi
    fi
}

# Function to show event stats
show_event_stats() {
    local log_file=$1
    if [ -f "$log_file" ]; then
        local events=$(tail -100 "$log_file" | grep "Total Events:" | tail -1 | awk '{print $3}')
        local drops=$(tail -100 "$log_file" | grep "Drop Rate:" | tail -1)
        if [ -n "$events" ]; then
            echo "    Events: $events"
        fi
        if [ -n "$drops" ]; then
            echo "    $drops"
        fi
    fi
}

# Main dashboard loop
while true; do
    clear
    echo "=== Ring Buffer Profiler Test Dashboard ==="
    echo "$(date)"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    
    # Check stability test
    echo ""
    echo -e "${BLUE}[Stability Test]${NC}"
    if check_test_status "/tmp/profiler-stability-logs/test.pid" "24h Stability Test"; then
        show_memory_usage "/tmp/profiler-stability-logs/stability-test.log"
        show_event_stats "/tmp/profiler-stability-logs/stability-test.log"
    fi
    
    # Check for other test processes
    echo ""
    echo -e "${BLUE}[Active eBPF Programs]${NC}"
    if command -v bpftool &> /dev/null; then
        local prog_count=$(sudo bpftool prog list 2>/dev/null | grep -c "profiler" || echo "0")
        echo "  Profiler programs loaded: $prog_count"
        
        if [ "$prog_count" -gt 0 ]; then
            sudo bpftool prog list 2>/dev/null | grep "profiler" | head -3 | sed 's/^/    /'
        fi
    else
        echo "  bpftool not available"
    fi
    
    # System resources
    echo ""
    echo -e "${BLUE}[System Resources]${NC}"
    echo "  CPU Load: $(uptime | awk -F'load average:' '{print $2}')"
    echo "  Memory: $(free -h | grep Mem | awk '{printf "Used: %s/%s (%.0f%%)", $3, $2, $3/$2*100}')"
    
    # Check for tmux sessions
    echo ""
    echo -e "${BLUE}[Tmux Sessions]${NC}"
    if command -v tmux &> /dev/null; then
        local sessions=$(tmux list-sessions 2>/dev/null | grep profiler || echo "  None")
        echo "$sessions" | sed 's/^/  /'
    else
        echo "  tmux not installed"
    fi
    
    # Recent log entries
    echo ""
    echo -e "${BLUE}[Recent Activity]${NC}"
    if [ -f "/tmp/profiler-stability-logs/stability-test.log" ]; then
        tail -3 "/tmp/profiler-stability-logs/stability-test.log" | sed 's/^/  /'
    else
        echo "  No recent activity"
    fi
    
    # Controls
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Commands:"
    echo "  [a] Attach to tmux session"
    echo "  [l] View full logs"
    echo "  [s] Start new stability test"
    echo "  [k] Kill all tests"
    echo "  [q] Quit dashboard"
    echo "  [r] Refresh (auto-refreshes every 5s)"
    echo ""
    
    # Handle user input with timeout
    read -t 5 -n 1 -r -p "Choice: " choice
    echo ""
    
    case $choice in
        a)
            echo "Available sessions:"
            tmux list-sessions 2>/dev/null | grep -n profiler || echo "No sessions"
            read -p "Enter session number or name: " session
            if [ -n "$session" ]; then
                # If number, get session name
                if [[ "$session" =~ ^[0-9]+$ ]]; then
                    session=$(tmux list-sessions | grep profiler | sed -n "${session}p" | cut -d: -f1)
                fi
                tmux attach -t "$session"
            fi
            ;;
        l)
            less /tmp/profiler-stability-logs/stability-test.log
            ;;
        s)
            read -p "Test duration (default 24h): " duration
            duration=${duration:-24h}
            ./run-profiler-stability-test.sh "$duration"
            ;;
        k)
            echo "Killing all profiler tests..."
            pkill -f profiler-hardware-test || true
            pkill -f profiler-integration-test || true
            tmux kill-session -t profiler-stability 2>/dev/null || true
            sudo bpftool prog list | grep profiler | awk '{print $1}' | cut -d: -f1 | xargs -r -I {} sudo bpftool prog detach id {} 2>/dev/null || true
            echo "Done"
            sleep 2
            ;;
        q)
            echo "Exiting dashboard..."
            exit 0
            ;;
        *)
            # Auto-refresh
            ;;
    esac
done