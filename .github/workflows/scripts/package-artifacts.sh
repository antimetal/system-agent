#!/bin/bash
# Package artifacts for integration testing
# This script collects eBPF programs and test scripts for VM testing

set -euo pipefail

echo "=== Packaging Test Artifacts ==="

# Create artifacts directory (clean first to avoid stale artifacts)
rm -rf artifacts
mkdir -p artifacts

# Copy test runner script (required)
SCRIPT_PATH=".github/workflows/scripts/run-tests-in-vm.sh"
if [ ! -f "$SCRIPT_PATH" ]; then
    echo "ERROR: Required script not found: $SCRIPT_PATH"
    exit 1
fi

echo "Copying run-tests-in-vm.sh to artifacts..."
cp "$SCRIPT_PATH" artifacts/
chmod +x artifacts/run-tests-in-vm.sh

# Copy eBPF programs if they exist
# The main Makefile builds to ebpf/build/
echo "Searching for eBPF programs to package..."
mkdir -p artifacts/ebpf

ebpf_count=0
if [ -d ebpf/build ]; then
    for f in $(find ebpf/build -name "*.bpf.o" -type f 2>/dev/null); do
        basename_f=$(basename "$f")
        # Skip test eBPF programs - they shouldn't be shipped in production
        if [[ "$basename_f" == "test_"* ]]; then
            echo "Skipping test eBPF program: $basename_f"
            continue
        fi
        echo "Copying $f to artifacts/ebpf/"
        cp "$f" artifacts/ebpf/
        ebpf_count=$((ebpf_count + 1))
    done
fi

# Also check internal/ebpf directory (legacy location)
if [ -d internal/ebpf ]; then
    for f in $(find internal/ebpf -name "*.bpf.o" -type f 2>/dev/null); do
        basename_f=$(basename "$f")
        # Skip test eBPF programs
        if [[ "$basename_f" == "test_"* ]]; then
            echo "Skipping test eBPF program: $basename_f"
            continue
        fi
        if [ ! -f "artifacts/ebpf/$basename_f" ]; then
            echo "Copying $f to artifacts/ebpf/"
            cp "$f" artifacts/ebpf/
            ebpf_count=$((ebpf_count + 1))
        fi
    done
fi

if [ "$ebpf_count" -gt 0 ]; then
    echo "Successfully packaged $ebpf_count eBPF programs:"
    ls -la artifacts/ebpf/
else
    echo "WARNING: No eBPF programs found to package (test programs excluded)"
    echo "Searched in:"
    echo "  - ebpf/build/ (primary location)"
    echo "  - internal/ebpf/ (legacy location)"
    
    # Debug: Show what we can find
    echo "Debug: Files in ebpf/build (first 20):"
    find ebpf/build -type f 2>/dev/null | head -20 || echo "  Directory not found"
    
    echo "Debug: Files in internal/ebpf (first 20):"
    find internal/ebpf -type f 2>/dev/null | head -20 || echo "  Directory not found"
    
    echo "Note: Test programs (test_*.bpf.o) are excluded from packaging"
fi

# Summary
echo -e "\n=== Packaging Summary ==="
echo "Artifacts directory contents:"
find artifacts -type f -exec ls -la {} \; 2>/dev/null || echo "No files found"

# Verify eBPF programs
if [ "$ebpf_count" -eq 0 ]; then
    echo "WARNING: No eBPF programs packaged (tests may be limited)"
fi

echo "Packaging completed successfully"