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
            echo -e "${RED}✗${NC} Not found (REQUIRED)"
            FAILED=1
        else
            echo -e "${YELLOW}⚠${NC} Not found (optional)"
        fi
    fi
    echo
}

# Function to check file exists
check_file() {
    local file=$1
    local description=$2
    
    echo -n "Checking $description... "
    
    if [ -f "$file" ]; then
        echo -e "${GREEN}✓${NC} Found at $file"
    else
        echo -e "${YELLOW}⚠${NC} Not found at $file"
    fi
    echo
}

# Build the Docker image first
echo "Building eBPF builder Docker image..."
if ! docker build -t ebpf-builder -f "${PROJECT_ROOT}/ebpf/Dockerfile" "${PROJECT_ROOT}/ebpf" >/dev/null 2>&1; then
    echo -e "${RED}Failed to build Docker image${NC}"
    exit 1
fi
echo -e "${GREEN}✓${NC} Docker image built successfully"
echo

# Check if BTF mount is available (Linux only)
BTF_MOUNT=""
if [ -d "/sys/kernel/btf" ]; then
    BTF_MOUNT="-v /sys/kernel/btf:/sys/kernel/btf:ro"
fi

# Run validation inside Docker
docker run --rm \
    -v "${PROJECT_ROOT}:/workspace" \
    ${BTF_MOUNT} \
    -w /workspace \
    ebpf-builder \
    bash -c '
set -euo pipefail

# Colors for output
RED="\033[0;31m"
GREEN="\033[0;32m"
YELLOW="\033[1;33m"
NC="\033[0m" # No Color

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
            echo -e "${RED}✗${NC} Not found (REQUIRED)"
            FAILED=1
        else
            echo -e "${YELLOW}⚠${NC} Not found (optional)"
        fi
    fi
    echo
}

echo "=== Core Build Tools ==="
check_tool "clang" "--version"
check_tool "llc" "--version" 
check_tool "make" "--version"
check_tool "git" "--version"

echo "=== Go Environment ==="
check_tool "go" "version"
if command -v go >/dev/null 2>&1; then
    echo "  GOROOT: ${GOROOT:-$(go env GOROOT)}"
    echo "  GOPATH: ${GOPATH:-$(go env GOPATH)}"
    echo "  PATH includes Go: $(echo $PATH | grep -q "/usr/local/go/bin" && echo "yes" || echo "no")"
fi
echo

echo "=== eBPF Tools ==="
# Special handling for bpftool
echo -n "Checking bpftool... "
BPFTOOL=$(which bpftool || find /usr/lib/linux-tools -name bpftool -executable 2>/dev/null | head -1 || echo "")
if [ -n "$BPFTOOL" ]; then
    echo -e "${GREEN}✓${NC} Found at $BPFTOOL"
    echo -n "  Version: "
    $BPFTOOL --version 2>&1 | head -1 || echo "unknown"
else
    echo -e "${RED}✗${NC} Not found (REQUIRED)"
    FAILED=1
fi
echo

echo "=== BTF Support ==="
echo -n "Checking host BTF availability... "
if [ -f /sys/kernel/btf/vmlinux ]; then
    echo -e "${GREEN}✓${NC} Available"
    echo "  Size: $(du -h /sys/kernel/btf/vmlinux | cut -f1)"
else
    echo -e "${YELLOW}⚠${NC} Not available (vmlinux.h generation will not work)"
fi
echo

echo "=== Project Structure ==="
echo -n "Checking workspace mount... "
if [ -d /workspace ] && [ -f /workspace/go.mod ]; then
    echo -e "${GREEN}✓${NC} Mounted correctly"
else
    echo -e "${RED}✗${NC} Not mounted correctly"
    FAILED=1
fi

echo -n "Checking eBPF include directory... "
if [ -d /workspace/ebpf/include ]; then
    echo -e "${GREEN}✓${NC} Exists"
    if [ -f /workspace/ebpf/include/vmlinux.h ]; then
        echo "  vmlinux.h: present ($(wc -l < /workspace/ebpf/include/vmlinux.h) lines)"
    else
        echo "  vmlinux.h: not generated yet"
    fi
else
    echo -e "${YELLOW}⚠${NC} Not created yet"
fi
echo

echo "=== Go Module Dependencies ==="
if [ -f /workspace/go.mod ]; then
    echo -n "Checking cilium/ebpf dependency... "
    if grep -q "github.com/cilium/ebpf" /workspace/go.mod; then
        echo -e "${GREEN}✓${NC} Present"
        grep "github.com/cilium/ebpf" /workspace/go.mod | head -1
    else
        echo -e "${RED}✗${NC} Not found in go.mod"
        FAILED=1
    fi
fi
echo

echo "=== Summary ==="
if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}All required tools are present!${NC}"
    echo "The eBPF build environment is ready."
else
    echo -e "${RED}Some required tools are missing!${NC}"
    echo "Please check the errors above."
    exit 1
fi
'

# Check exit status
if [ $? -eq 0 ]; then
    echo
    echo -e "${GREEN}=== Validation Passed ===${NC}"
    echo "The eBPF build environment is properly configured."
else
    echo
    echo -e "${RED}=== Validation Failed ===${NC}"
    echo "Please fix the issues above before proceeding."
    exit 1
fi