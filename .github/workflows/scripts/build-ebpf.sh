#!/bin/bash
# Build eBPF programs with CO-RE support
# This script is used by the GitHub Actions workflow to build eBPF programs

set -euo pipefail

echo "Building eBPF programs with CO-RE support..."

# Check if we have eBPF source files
if [ ! -d ebpf/src ]; then
    echo "Warning: ebpf/src directory not found, skipping eBPF build"
    exit 0
fi

# Count eBPF source files
ebpf_count=$(find ebpf/src -name "*.bpf.c" -type f 2>/dev/null | wc -l)
if [ "$ebpf_count" -eq 0 ]; then
    echo "Warning: No eBPF source files found in ebpf/src"
    exit 0
fi

echo "Found $ebpf_count eBPF source files to build"

# Always clean build directory completely to ensure fresh builds
echo "Cleaning all eBPF build artifacts for fresh build..."
rm -rf ebpf/build
make clean-ebpf

# Use the main Makefile to build eBPF programs
echo "Building eBPF programs using main Makefile..."
make build-ebpf

echo "eBPF programs built successfully"
echo "Looking for .bpf.o files in ebpf/build/:"

# Verify that eBPF programs were built
found_files=0
for f in $(find ebpf/build -name "*.bpf.o" -type f 2>/dev/null); do
    echo "  Found: $f"
    ls -la "$f"
    found_files=$((found_files + 1))
done

if [ "$found_files" -eq 0 ]; then
    echo "ERROR: No eBPF programs found after build"
    echo "Directory structure of ebpf/build:"
    find ebpf/build -type f -name "*.o" 2>/dev/null | head -20 || echo "No .o files found"
    ls -laR ebpf/build/ 2>/dev/null || echo "ebpf/build not found"
    exit 1
fi

echo "Successfully built $found_files eBPF programs"