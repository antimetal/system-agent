#!/bin/bash
# Build eBPF programs with CO-RE support
# This script is used by the GitHub Actions workflow to build eBPF programs

set -euo pipefail

echo "Building eBPF programs with CO-RE support..."

# Check if we have eBPF source files
if [ ! -d ebpf/src ]; then
    echo "ERROR: ebpf/src directory not found"
    echo "Expected eBPF source files in ebpf/src/"
    exit 1
fi

# Count eBPF source files
ebpf_count=$(find ebpf/src -name "*.bpf.c" -type f | wc -l)
if [ "$ebpf_count" -eq 0 ]; then
    echo "ERROR: No eBPF source files found in ebpf/src"
    echo "Expected .bpf.c files in ebpf/src/"
    exit 1
fi

echo "Found $ebpf_count eBPF source files to build"

# Always clean build directory completely to ensure fresh builds
echo "Cleaning all eBPF build artifacts for fresh build..."
make clean-ebpf

# Use the main Makefile to build eBPF programs
echo "Building eBPF programs using main Makefile..."
make build-ebpf

echo "eBPF programs built successfully"

# Build integration test binaries
echo "Building integration test binaries..."

# Find packages with integration tests
packages_with_tests=""
for pkg in $(go list -tags integration ./...); do
    if go list -f '{{or .TestGoFiles .XTestGoFiles}}' -tags integration "$pkg" | grep -q "\.go"; then
        if [ -z "$packages_with_tests" ]; then
            packages_with_tests="$pkg"
        else
            packages_with_tests="$packages_with_tests $pkg"
        fi
    fi
done

if [ -z "$packages_with_tests" ]; then
    echo "ERROR: No packages with integration tests found"
    exit 1
fi

echo "Found packages with integration tests: $packages_with_tests"

# Build integration test binaries for packages with integration tests
echo "Creating integration test binaries"
mkdir -p integration-tests

# Build each package separately (go test -c only supports one package at a time)
for pkg in $packages_with_tests; do
    # Create a simple name for each test binary based on the package path
    pkg_name=$(echo "$pkg" | sed 's|github.com/antimetal/agent/||' | sed 's|/|_|g')
    echo "Building test binary for $pkg -> integration-tests/${pkg_name}.test"
    GOOS=linux GOARCH=amd64 go test -c -tags integration -o "integration-tests/${pkg_name}.test" "$pkg"
done

if [ ! -d "integration-tests" ] || [ -z "$(ls -A integration-tests)" ]; then
    echo "ERROR: Failed to build integration test binaries"
    exit 1
fi

echo "Integration test binaries built successfully:"
ls -la integration-tests/
echo "Looking for .bpf.o files in ebpf/build/:"

# Verify that eBPF programs were built
found_files=0
for f in $(find ebpf/build -name "*.bpf.o" -type f); do
    echo "  Found: $f"
    ls -la "$f"
    found_files=$((found_files + 1))
done

if [ "$found_files" -eq 0 ]; then
    echo "ERROR: No eBPF programs found after build"
    echo "Directory structure of ebpf/build:"
    find ebpf/build -type f -name "*.o" | head -20
    ls -laR ebpf/build/
    exit 1
fi

echo "Successfully built $found_files eBPF programs"
