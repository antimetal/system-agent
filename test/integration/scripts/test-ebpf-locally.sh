#!/bin/bash
# Test eBPF programs locally with LVH
# This script assumes you've already debugged LVH with debug-lvh-locally.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

echo "=== eBPF Local Test with LVH ==="
echo "Project root: $PROJECT_ROOT"
echo ""

# Check prerequisites
if ! command -v lvh &> /dev/null; then
    echo "ERROR: lvh not found. Please install it first."
    echo "Run: $SCRIPT_DIR/debug-lvh-locally.sh"
    exit 1
fi

# Build eBPF programs
echo "=== Building eBPF Programs ==="
cd "$PROJECT_ROOT/ebpf"

# Generate vmlinux.h if needed
if [ ! -f include/vmlinux.h ]; then
    echo "Generating vmlinux.h..."
    ./scripts/generate_vmlinux.sh || {
        echo "Failed to generate vmlinux.h, downloading from libbpf-bootstrap..."
        mkdir -p include
        curl -sL https://raw.githubusercontent.com/libbpf/libbpf-bootstrap/master/examples/c/vmlinux.h -o include/vmlinux.h
    }
fi

# Build eBPF programs
echo "Building eBPF programs..."
make clean
make all

# List built programs
echo ""
echo "Built eBPF programs:"
find "$PROJECT_ROOT/internal/ebpf" -name "*.bpf.o" -type f

# Prepare test artifacts
TEST_DIR="/tmp/ebpf-lvh-test-$$"
mkdir -p "$TEST_DIR"/{objects,scripts}

# Copy eBPF objects
echo ""
echo "Copying eBPF objects to test directory..."
find "$PROJECT_ROOT/internal/ebpf" -name "*.bpf.o" -type f | while read -r obj; do
    progname=$(basename "$(dirname "$obj")")
    cp "$obj" "$TEST_DIR/objects/${progname}.bpf.o"
    echo "  Copied: ${progname}.bpf.o"
done

# Copy verifier test script
cp "$PROJECT_ROOT/test/integration/scripts/ebpf-verifier-test.sh" "$TEST_DIR/scripts/"
chmod +x "$TEST_DIR/scripts/ebpf-verifier-test.sh"

# Create a wrapper script for the VM
cat > "$TEST_DIR/run-in-vm.sh" << 'EOF'
#!/bin/bash
set -e

echo "=== eBPF Verifier Tests in VM ==="
echo "Kernel: $(uname -r)"
echo "OS: $(cat /etc/os-release | grep PRETTY_NAME || echo 'Unknown')"
echo ""

# Set environment variables for the test script
export EBPF_DIR="/host/objects"
export RESULTS_DIR="/tmp/results"
export VERBOSE=1

# Run the verifier test
/host/scripts/ebpf-verifier-test.sh

# Copy results back to host mount
cp -r "$RESULTS_DIR"/* /host/ 2>/dev/null || true
EOF
chmod +x "$TEST_DIR/run-in-vm.sh"

# Test with different kernel versions
KERNELS=("5.10" "5.15" "5.4")

for kernel in "${KERNELS[@]}"; do
    echo ""
    echo "=== Testing on kernel $kernel ==="
    
    # Create results directory for this kernel
    mkdir -p "$TEST_DIR/results-$kernel"
    
    echo "Starting VM with kernel $kernel..."
    if lvh run \
        --image base \
        --image-version "$kernel" \
        --host-mount "$TEST_DIR:/host" \
        -- /host/run-in-vm.sh 2>&1 | tee "$TEST_DIR/results-$kernel/vm-output.log"; then
        
        echo "✓ Tests completed on kernel $kernel"
        
        # Check if results were created
        if [ -f "$TEST_DIR/verifier-results.json" ]; then
            mv "$TEST_DIR/verifier-results.json" "$TEST_DIR/results-$kernel/"
            echo "  Results saved to: $TEST_DIR/results-$kernel/verifier-results.json"
        fi
    else
        echo "✗ Failed to test on kernel $kernel"
    fi
done

echo ""
echo "=== Test Summary ==="
echo "Results directory: $TEST_DIR"
echo ""
for kernel in "${KERNELS[@]}"; do
    if [ -f "$TEST_DIR/results-$kernel/verifier-results.json" ]; then
        echo "Kernel $kernel:"
        jq -r '"\t✓ Passed: \(.passed)/\(.total_programs)"' "$TEST_DIR/results-$kernel/verifier-results.json" 2>/dev/null || echo "	Error reading results"
    else
        echo "Kernel $kernel: No results"
    fi
done

echo ""
echo "To view detailed results:"
echo "  ls -la $TEST_DIR/results-*/"