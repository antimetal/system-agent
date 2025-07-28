#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm
# Script to validate the eBPF build environment in Docker

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "=== eBPF Build Environment Validation ==="
echo

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Track if any checks fail
FAILED=0

# Function to check command exists and print version
check_tool() {
    local tool=$1
    local version_flag=${2:-"--version"}
    local required=${3:-true}
    
    echo -n "Checking $tool... "
    
    if command -v "$tool" >/dev/null 2>&1; then
        echo -e "${GREEN}✓${NC} Found"
        echo -n "  Version: "
        $tool $version_flag 2>&1 | head -1 || echo "unknown"
    else
        if [ "$required" = true ]; then
            echo -e "${RED}✗${NC} Not found (required)"
            FAILED=1
        else
            echo -e "${YELLOW}✗${NC} Not found (optional)"
        fi
    fi
}

echo "=== Build Tools ==="
check_tool "clang"
check_tool "llc" "--version" false
check_tool "bpftool" "version" 
check_tool "make" "--version"

echo
echo "=== Go Environment ==="
check_tool "go" "version"

echo
echo "=== eBPF Prerequisites ==="

# Check for vmlinux.h
echo -n "Checking vmlinux.h... "
if [ -f "/usr/include/vmlinux.h" ]; then
    echo -e "${GREEN}✓${NC} Found at /usr/include/vmlinux.h"
    LINES=$(wc -l < /usr/include/vmlinux.h)
    echo "  Size: $LINES lines"
else
    echo -e "${RED}✗${NC} Not found"
    FAILED=1
fi

# Check for libbpf headers
echo -n "Checking libbpf headers... "
if [ -d "/usr/include/bpf" ]; then
    echo -e "${GREEN}✓${NC} Found at /usr/include/bpf"
else
    echo -e "${RED}✗${NC} Not found"
    FAILED=1
fi

echo
echo "=== Clang BPF Target Support ==="
echo -n "Testing clang BPF compilation... "

# Create a minimal BPF program to test compilation
cat > /tmp/test.bpf.c << 'EOF'
// SPDX-License-Identifier: GPL-2.0-only
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

char LICENSE[] SEC("license") = "GPL";

SEC("tracepoint/syscalls/sys_enter_openat")
int trace_openat(void *ctx) {
    return 0;
}
EOF

if clang -target bpf -g -O2 -c /tmp/test.bpf.c -o /tmp/test.bpf.o 2>/dev/null; then
    echo -e "${GREEN}✓${NC} BPF compilation successful"
    
    # Check if BTF information is present
    echo -n "Checking BTF generation... "
    if bpftool btf dump file /tmp/test.bpf.o format raw >/dev/null 2>&1; then
        echo -e "${GREEN}✓${NC} BTF information present"
    else
        echo -e "${YELLOW}✗${NC} BTF generation failed (may still work)"
    fi
    
    rm -f /tmp/test.bpf.o
else
    echo -e "${RED}✗${NC} BPF compilation failed"
    FAILED=1
fi

rm -f /tmp/test.bpf.c

echo
echo "=== Environment Summary ==="

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✓ eBPF build environment is ready${NC}"
    echo "You can now build eBPF programs with CO-RE support."
else
    echo -e "${RED}✗ eBPF build environment has issues${NC}"
    echo "Please install missing dependencies before building eBPF programs."
    exit 1
fi

echo
echo "=== Next Steps ==="
echo "1. Build eBPF programs: make build-ebpf"
echo "2. Generate Go bindings: make generate-ebpf-bindings"
echo "3. Generate type definitions: make generate-ebpf-types"