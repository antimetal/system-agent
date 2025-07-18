#!/bin/bash
# SPDX-License-Identifier: GPL-2.0-only
#
# Script to check CO-RE (Compile Once - Run Everywhere) support on the current system

set -euo pipefail

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo "=== CO-RE Support Checker ==="
echo

# Function to compare kernel versions
version_ge() {
    test "$(printf '%s\n' "$@" | sort -V | head -n 1)" != "$1"
}

# Check kernel version
KERNEL_VERSION=$(uname -r | cut -d'-' -f1)
echo "Kernel version: $(uname -r)"

# Check for BTF support
BTF_PATH="/sys/kernel/btf/vmlinux"
if [ -f "$BTF_PATH" ]; then
    echo -e "${GREEN}✓${NC} Native kernel BTF found at $BTF_PATH"
    HAS_BTF=true
else
    echo -e "${YELLOW}✗${NC} No native kernel BTF found"
    HAS_BTF=false
fi

# Check for pahole (for generating BTF)
if command -v pahole >/dev/null 2>&1; then
    PAHOLE_VERSION=$(pahole --version | grep -oE '[0-9]+\.[0-9]+')
    echo -e "${GREEN}✓${NC} pahole found (version $PAHOLE_VERSION)"
    HAS_PAHOLE=true
else
    echo -e "${YELLOW}✗${NC} pahole not found (needed for BTF generation on older kernels)"
    HAS_PAHOLE=false
fi

# Check for bpftool
if command -v bpftool >/dev/null 2>&1; then
    echo -e "${GREEN}✓${NC} bpftool found"
    
    # Try to dump BTF
    if [ "$HAS_BTF" = true ]; then
        if bpftool btf dump file "$BTF_PATH" format raw >/dev/null 2>&1; then
            echo -e "${GREEN}✓${NC} BTF can be read successfully"
        else
            echo -e "${RED}✗${NC} BTF exists but cannot be read"
        fi
    fi
else
    echo -e "${YELLOW}✗${NC} bpftool not found"
fi

# Check clang version and BPF target support
if command -v clang >/dev/null 2>&1; then
    CLANG_VERSION=$(clang --version | head -n1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1)
    echo -e "${GREEN}✓${NC} clang found (version $CLANG_VERSION)"
    
    # Check for BPF target
    if clang -target bpf -c -x c /dev/null -o /dev/null 2>/dev/null; then
        echo -e "${GREEN}✓${NC} clang BPF target supported"
    else
        echo -e "${RED}✗${NC} clang BPF target not supported"
    fi
else
    echo -e "${RED}✗${NC} clang not found"
fi

echo
echo "=== CO-RE Support Summary ==="

# Determine CO-RE support level
if version_ge "$KERNEL_VERSION" "5.2.0"; then
    if [ "$HAS_BTF" = true ]; then
        echo -e "${GREEN}Full CO-RE support${NC} - Kernel $KERNEL_VERSION with native BTF"
        echo "You can compile BPF programs once and run them on different kernels."
    else
        echo -e "${YELLOW}Partial CO-RE support${NC} - Kernel $KERNEL_VERSION but BTF not found"
        echo "Kernel supports BTF but it may not be enabled. Check CONFIG_DEBUG_INFO_BTF=y"
    fi
elif version_ge "$KERNEL_VERSION" "4.18.0"; then
    echo -e "${YELLOW}Partial CO-RE support${NC} - Kernel $KERNEL_VERSION"
    echo "CO-RE can work with external BTF. Consider using BTF from btfhub."
    if [ "$HAS_PAHOLE" = true ]; then
        echo "pahole is available for generating BTF if needed."
    fi
else
    echo -e "${RED}No CO-RE support${NC} - Kernel $KERNEL_VERSION is too old"
    echo "CO-RE requires kernel 4.18 or later. Traditional BPF compilation required."
fi

echo
echo "For more information about CO-RE, see:"
echo "  https://nakryiko.com/posts/bpf-portability-and-co-re/"