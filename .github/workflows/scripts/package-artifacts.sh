#!/bin/bash
# Package artifacts for integration testing
# This script collects eBPF programs and test scripts for VM testing

set -euo pipefail

echo "=== Packaging Test Artifacts ==="

# Create artifacts directory (clean first to avoid stale artifacts)
rm -r artifacts || true
mkdir -p artifacts

# Copy test runner script (required)
SCRIPT_PATH=".github/workflows/scripts/run-lvh-tests.sh"
if [ ! -f "$SCRIPT_PATH" ]; then
    echo "ERROR: Required script not found: $SCRIPT_PATH"
    exit 1
fi

echo "Copying run-lvh-tests.sh to artifacts..."
cp "$SCRIPT_PATH" artifacts/
chmod +x artifacts/run-lvh-tests.sh

# Copy integration test binaries (required)
if [ ! -d "integration-tests" ] || [ -z "$(ls -A integration-tests)" ]; then
    echo "ERROR: Integration test binaries not found: integration-tests/"
    echo "Did the build step run successfully?"
    exit 1
fi

echo "Copying integration test binaries to artifacts..."
cp -r integration-tests artifacts/
echo "Integration test binaries:"
ls -la artifacts/integration-tests/

# Copy eBPF programs (required)
# The main Makefile builds to ebpf/build/
echo "Packaging eBPF programs..."
mkdir -p artifacts/ebpf

# Check that the build directory exists
if [ ! -d ebpf/build ]; then
    echo "ERROR: eBPF build directory not found: ebpf/build/"
    echo "Did the eBPF build step run successfully?"
    exit 1
fi

ebpf_count=0
for f in $(find ebpf/build -name "*.bpf.o" -type f); do
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

if [ "$ebpf_count" -eq 0 ]; then
    echo "ERROR: No eBPF programs found to package!"
    echo "Expected .bpf.o files in ebpf/build/ (excluding test_* files)"
    echo ""
    echo "Files found in ebpf/build:"
    find ebpf/build -type f -name "*.bpf.o" | head -20 || echo "  No .bpf.o files found"
    exit 1
fi

echo "Successfully packaged $ebpf_count eBPF programs:"
ls -la artifacts/ebpf/

# Verify artifacts were created
if [ ! -f "artifacts/run-lvh-tests.sh" ]; then
    echo "ERROR: Failed to package run-lvh-tests.sh"
    exit 1
fi

if [ ! -d "artifacts/integration-tests" ] || [ -z "$(ls -A artifacts/integration-tests)" ]; then
    echo "ERROR: Failed to package integration test binaries"
    exit 1
fi
if [ ! -d "artifacts/ebpf" ] || [ -z "$(ls -A artifacts/ebpf)" ]; then
    echo "ERROR: Failed to package eBPF programs"
    exit 1
fi

# Summary
echo -e "\n=== Packaging Summary ==="
echo "Artifacts directory contents:"
find artifacts -type f -exec ls -la {} \;
echo ""
echo "Total: $ebpf_count eBPF programs packaged"
echo "Packaging completed successfully"