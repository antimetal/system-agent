#!/bin/bash
# Quick profiler test for a single VM

set -euo pipefail

VM="${1:-ebpf-modern}"
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

echo "Quick profiler test on VM: $VM"

# Build eBPF
echo "Building eBPF programs..."
make -C "$PROJECT_ROOT" build-ebpf

# Build test binary
echo "Building test binary..."
cd "$PROJECT_ROOT"
GOOS=linux GOARCH=$(limactl shell "$VM" uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') \
    go build -o /tmp/profiler-quick-test ./ebpf/testing/profiler-test.go

# Copy files
echo "Copying files to VM..."
limactl copy /tmp/profiler-quick-test "$VM:/tmp/profiler-test"
limactl shell "$VM" chmod +x /tmp/profiler-test
limactl copy "$PROJECT_ROOT/ebpf/build/profiler.bpf.o" "$VM:/tmp/"

# Run basic test
echo "Running basic profiler test..."
if limactl shell "$VM" sudo /tmp/profiler-test -mode basic -duration 5s -bpf-path /tmp/profiler.bpf.o; then
    echo "✓ Basic profiler test passed!"
else
    echo "✗ Basic profiler test failed"
    exit 1
fi