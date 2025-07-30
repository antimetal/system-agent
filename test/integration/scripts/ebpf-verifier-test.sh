#!/bin/bash
# eBPF Verifier Test Script
# This script tests eBPF program loading and CO-RE relocations across different kernels

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
EBPF_DIR="${EBPF_DIR:-/host_mount/ebpf-artifacts/objects}"
RESULTS_DIR="${RESULTS_DIR:-/tmp/ebpf-test-results}"
VERBOSE="${VERBOSE:-0}"

# Create results directory
mkdir -p "$RESULTS_DIR"

# Function to log messages
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

# Function to test if a BPF helper is available
test_helper_available() {
    local helper_name="$1"
    local helper_id="$2"
    
    if [ -f /sys/kernel/debug/tracing/available_filter_functions ]; then
        grep -q "bpf_$helper_name" /sys/kernel/debug/tracing/available_filter_functions 2>/dev/null && return 0
    fi
    
    # Fallback: try to check via kernel version (simplified)
    return 1
}

# Function to get kernel version as comparable number
get_kernel_version() {
    local version=$(uname -r | cut -d- -f1)
    local major=$(echo "$version" | cut -d. -f1)
    local minor=$(echo "$version" | cut -d. -f2)
    local patch=$(echo "$version" | cut -d. -f3)
    echo $((major * 10000 + minor * 100 + patch))
}

# Function to test CO-RE features
test_core_features() {
    log "Testing CO-RE (Compile Once - Run Everywhere) features..."
    
    # Check for BTF support
    if [ -f /sys/kernel/btf/vmlinux ]; then
        echo -e "${GREEN}✓${NC} Native kernel BTF support detected"
        echo "  BTF size: $(stat -c%s /sys/kernel/btf/vmlinux 2>/dev/null || echo 'unknown') bytes"
    else
        echo -e "${YELLOW}⚠${NC} No native kernel BTF support (requires kernel 5.2+)"
    fi
    
    # Check for CO-RE helper availability
    local kernel_ver=$(get_kernel_version)
    
    # Ring buffer support (5.8+)
    if [ $kernel_ver -ge 50800 ]; then
        echo -e "${GREEN}✓${NC} Ring buffer support available (kernel 5.8+)"
    else
        echo -e "${YELLOW}⚠${NC} Ring buffer not supported (requires kernel 5.8+)"
    fi
    
    # bpf_probe_read_kernel/user (5.5+)
    if [ $kernel_ver -ge 50500 ]; then
        echo -e "${GREEN}✓${NC} CO-RE probe helpers available (kernel 5.5+)"
    else
        echo -e "${YELLOW}⚠${NC} Legacy probe helpers only (CO-RE helpers require kernel 5.5+)"
    fi
    
    echo ""
}

# Function to analyze verifier error
analyze_verifier_error() {
    local error_log="$1"
    local prog_name="$2"
    
    echo "Analyzing verifier error for $prog_name:"
    
    # Check for common error patterns
    if grep -q "invalid bpf_context access" "$error_log"; then
        echo "  - Context access error: Program may be using wrong context type"
    fi
    
    if grep -q "unknown func bpf_" "$error_log"; then
        echo "  - Missing BPF helper: Program uses helpers not available in this kernel"
        grep "unknown func" "$error_log" | head -5 | sed 's/^/    /'
    fi
    
    if grep -q "invalid map type" "$error_log"; then
        echo "  - Unsupported map type: Program uses map types not available in this kernel"
    fi
    
    if grep -q "R[0-9] type=inv" "$error_log"; then
        echo "  - Type verification error: Invalid register types detected"
    fi
    
    if grep -q "back-edge" "$error_log"; then
        echo "  - Loop detection: Kernel may not support bounded loops"
    fi
    
    # Show last few lines of actual error
    echo "  - Verifier output (last 10 lines):"
    tail -10 "$error_log" | sed 's/^/    /'
}

# Main test execution
main() {
    log "=== eBPF Verifier Test Suite ==="
    log "Kernel: $(uname -r)"
    log "Architecture: $(uname -m)"
    
    # Test CO-RE features first
    test_core_features
    
    # Check if bpftool is available
    if ! command -v bpftool &> /dev/null; then
        echo -e "${RED}✗${NC} bpftool not found. Installing..."
        apt-get update -qq && apt-get install -y -qq bpftool || {
            echo -e "${RED}✗${NC} Failed to install bpftool"
            exit 1
        }
    fi
    
    # Results tracking
    declare -A RESULTS
    declare -A LOAD_TIMES
    local TOTAL=0
    local PASSED=0
    local FAILED=0
    
    log "Testing eBPF programs in: $EBPF_DIR"
    echo ""
    
    # Test each eBPF program
    for obj in "$EBPF_DIR"/*.bpf.o; do
        if [ ! -f "$obj" ]; then
            continue
        fi
        
        PROGNAME=$(basename "$obj" .bpf.o)
        TOTAL=$((TOTAL + 1))
        
        echo "Testing: $PROGNAME"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        
        # Show object file info
        echo "Object info:"
        echo "  Size: $(stat -c%s "$obj") bytes"
        echo "  Type: $(file -b "$obj")"
        
        # Try to dump BTF info
        if [ "$VERBOSE" = "1" ]; then
            echo "  BTF sections:"
            bpftool btf dump file "$obj" format raw 2>&1 | head -5 | sed 's/^/    /' || echo "    No BTF info"
        fi
        
        # Measure load time
        local start_time=$(date +%s.%N)
        
        # Try to load the program
        local test_path="/sys/fs/bpf/test_${PROGNAME}_$$"
        local error_log="$RESULTS_DIR/${PROGNAME}-verifier.log"
        
        if bpftool prog load "$obj" "$test_path" 2>&1 | tee "$error_log"; then
            local end_time=$(date +%s.%N)
            local load_time=$(echo "$end_time - $start_time" | bc)
            
            echo -e "${GREEN}✓${NC} Successfully loaded $PROGNAME (${load_time}s)"
            PASSED=$((PASSED + 1))
            RESULTS[$PROGNAME]="PASS"
            LOAD_TIMES[$PROGNAME]="$load_time"
            
            # Show program details
            if [ "$VERBOSE" = "1" ]; then
                echo "Program details:"
                bpftool prog show pinned "$test_path" | sed 's/^/  /'
            fi
            
            # Get program ID for more info
            local prog_id=$(bpftool prog show pinned "$test_path" | grep -o 'prog_id [0-9]*' | awk '{print $2}')
            if [ -n "$prog_id" ]; then
                # Show program stats
                echo "  Instructions: $(bpftool prog dump xlated id "$prog_id" | wc -l)"
                
                # Check for map usage
                local map_count=$(bpftool prog show id "$prog_id" | grep -c 'map_ids' || echo "0")
                if [ "$map_count" -gt 0 ]; then
                    echo "  Maps used: $(bpftool prog show id "$prog_id" | grep -o 'map_ids [0-9,]*' | cut -d' ' -f2-)"
                fi
            fi
            
            # Cleanup
            rm -f "$test_path" 2>/dev/null || true
            
        else
            echo -e "${RED}✗${NC} Failed to load $PROGNAME"
            FAILED=$((FAILED + 1))
            RESULTS[$PROGNAME]="FAIL"
            
            # Analyze the error
            analyze_verifier_error "$error_log" "$PROGNAME"
        fi
        
        echo ""
    done
    
    # Test BPF map types
    log "=== Testing BPF Map Types ==="
    echo ""
    
    # Map types with their minimum kernel versions
    declare -A MAP_TYPES=(
        ["hash"]="3.19"
        ["array"]="3.19"
        ["prog_array"]="4.2"
        ["perf_event_array"]="4.3"
        ["percpu_hash"]="4.6"
        ["percpu_array"]="4.6"
        ["stack_trace"]="4.6"
        ["cgroup_array"]="4.8"
        ["lru_hash"]="4.10"
        ["lru_percpu_hash"]="4.10"
        ["lpm_trie"]="4.11"
        ["array_of_maps"]="4.12"
        ["hash_of_maps"]="4.12"
        ["devmap"]="4.14"
        ["sockmap"]="4.14"
        ["cpumap"]="4.15"
        ["xskmap"]="4.18"
        ["sockhash"]="4.18"
        ["queue"]="4.20"
        ["stack"]="4.20"
        ["sk_storage"]="5.2"
        ["devmap_hash"]="5.4"
        ["ringbuf"]="5.8"
    )
    
    for maptype in "${!MAP_TYPES[@]}"; do
        printf "%-20s: " "$maptype"
        
        # Create appropriate parameters for each map type
        case $maptype in
            "ringbuf")
                if bpftool map create /tmp/test-$maptype type $maptype max_entries 4096 2>/dev/null; then
                    echo -e "${GREEN}✓${NC} Supported (min kernel: ${MAP_TYPES[$maptype]})"
                    rm -f /tmp/test-$maptype
                else
                    echo -e "${YELLOW}⚠${NC} Not supported (requires kernel ${MAP_TYPES[$maptype]}+)"
                fi
                ;;
            "queue"|"stack")
                if bpftool map create /tmp/test-$maptype type $maptype key 0 value 8 entries 10 2>/dev/null; then
                    echo -e "${GREEN}✓${NC} Supported (min kernel: ${MAP_TYPES[$maptype]})"
                    rm -f /tmp/test-$maptype
                else
                    echo -e "${YELLOW}⚠${NC} Not supported (requires kernel ${MAP_TYPES[$maptype]}+)"
                fi
                ;;
            *)
                if bpftool map create /tmp/test-$maptype type $maptype key 4 value 8 entries 10 2>/dev/null; then
                    echo -e "${GREEN}✓${NC} Supported (min kernel: ${MAP_TYPES[$maptype]})"
                    rm -f /tmp/test-$maptype
                else
                    echo -e "${YELLOW}⚠${NC} Not supported (requires kernel ${MAP_TYPES[$maptype]}+)"
                fi
                ;;
        esac
    done
    
    echo ""
    log "=== Test Summary ==="
    echo "Total eBPF programs tested: $TOTAL"
    echo "Passed: $PASSED"
    echo "Failed: $FAILED"
    echo ""
    
    # Show results table
    echo "Results by program:"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    printf "%-20s %-10s %-10s\n" "Program" "Status" "Load Time"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    
    for prog in "${!RESULTS[@]}"; do
        local status="${RESULTS[$prog]}"
        local load_time="${LOAD_TIMES[$prog]:-N/A}"
        local status_icon
        
        if [ "$status" = "PASS" ]; then
            status_icon="${GREEN}✓ PASS${NC}"
        else
            status_icon="${RED}✗ FAIL${NC}"
        fi
        
        printf "%-20s %-20b %-10s\n" "$prog" "$status_icon" "${load_time}s"
    done
    
    # Create detailed results file
    cat > "$RESULTS_DIR/verifier-results.json" << EOF
{
  "kernel_version": "$(uname -r)",
  "kernel_version_num": $(get_kernel_version),
  "test_date": "$(date -Iseconds)",
  "architecture": "$(uname -m)",
  "btf_support": $([ -f /sys/kernel/btf/vmlinux ] && echo "true" || echo "false"),
  "total_programs": $TOTAL,
  "passed": $PASSED,
  "failed": $FAILED,
  "programs": {
$(for prog in "${!RESULTS[@]}"; do
    echo "    \"$prog\": {"
    echo "      \"status\": \"${RESULTS[$prog]}\","
    echo "      \"load_time\": \"${LOAD_TIMES[$prog]:-0}\""
    echo "    },"
done | sed '$ s/,$//')
  }
}
EOF
    
    # Exit with appropriate code
    if [ $FAILED -gt 0 ]; then
        log "ERROR: $FAILED eBPF programs failed verification"
        exit 1
    else
        log "SUCCESS: All eBPF programs passed verification"
        exit 0
    fi
}

# Run main function
main "$@"