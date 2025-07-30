#!/bin/bash
#
# test-ebpf-locally.sh - Test eBPF programs on multiple kernel versions using LVH
#
# This script builds eBPF programs and tests their compatibility across different
# kernel versions using LVH (Little VM Helper). It verifies that eBPF programs
# can be loaded successfully on each kernel version.
#
# IMPORTANT: This script is designed for Linux systems only, as eBPF programs
# cannot be built on macOS.

set -e

# Script version
VERSION="1.0.0"

# Default values
DEFAULT_KERNELS=("5.10-main" "5.15-main" "6.1-main")
CUSTOM_KERNELS=()
SKIP_BUILD=false

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Function to display help
show_help() {
    cat << EOF
test-ebpf-locally.sh - Test eBPF programs on multiple kernel versions using LVH

USAGE:
    $0 [OPTIONS]

DESCRIPTION:
    This script automates testing of eBPF programs across different kernel versions:
    1. Builds eBPF programs (unless --skip-build is used)
    2. Creates a test environment with LVH
    3. Downloads specified kernel versions
    4. Runs VMs with each kernel and tests eBPF program loading
    5. Reports compatibility results

    IMPORTANT: This script is for Linux systems only. eBPF programs cannot be
    built on macOS.

OPTIONS:
    -h, --help          Show this help message and exit
    -v, --version       Show script version
    -k, --kernel        Add a kernel version to test (can be used multiple times)
    -K, --kernels-only  Test only specified kernels (skip defaults)
    -s, --skip-build    Skip building eBPF programs (use existing .bpf.o files)
    -d, --debug         Enable debug output

EXAMPLES:
    # Test with default kernels (5.10, 5.15, 6.1)
    $0

    # Test with additional kernel
    $0 --kernel 4.19-main

    # Test only specific kernels
    $0 --kernels-only --kernel 5.10-main --kernel 6.5-main

    # Skip build step (use previously built eBPF programs)
    $0 --skip-build

KERNEL VERSIONS:
    Default kernels tested: ${DEFAULT_KERNELS[@]}
    
    Available LVH kernels include:
    - 4.19-main, 5.4-main, 5.10-main, 5.15-main
    - 6.1-main, 6.2-main, 6.5-main, 6.6-main
    - bpf-main, bpf-next (latest development kernels)

REQUIREMENTS:
    - Linux operating system (eBPF build requires Linux)
    - LVH installed (go install github.com/cilium/little-vm-helper/cmd/lvh@latest)
    - eBPF build tools (clang, llvm, libbpf)
    - KVM support for virtualization
    - Internet connection for downloading kernels and VM images
    - About 1GB free disk space per kernel

OUTPUT:
    The script creates a test directory 'lvh-ebpf-test' containing:
    - eBPF object files (.bpf.o)
    - Downloaded kernel files
    - Test VM image
    - Test results for each kernel

    Results show which eBPF programs are compatible with which kernels.

TROUBLESHOOTING:
    If build fails:
        - Ensure you're on a Linux system
        - Install build dependencies: clang, llvm, libbpf-dev
        - Check that vmlinux.h is available or can be generated

    If LVH fails:
        - Run debug-lvh-locally.sh to diagnose LVH issues
        - Ensure KVM is available: ls /dev/kvm
        - Check virtualization is enabled in BIOS

EXIT CODES:
    0 - All tests passed
    1 - Build or setup error
    2 - Some eBPF programs failed verification

AUTHOR:
    Antimetal, Inc.

SEE ALSO:
    debug-lvh-locally.sh - Debug LVH installation and functionality
    https://github.com/cilium/little-vm-helper

EOF
}

# Parse command line arguments
KERNELS_ONLY=false
DEBUG=false

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -v|--version)
            echo "test-ebpf-locally.sh version $VERSION"
            exit 0
            ;;
        -k|--kernel)
            CUSTOM_KERNELS+=("$2")
            shift 2
            ;;
        -K|--kernels-only)
            KERNELS_ONLY=true
            shift
            ;;
        -s|--skip-build)
            SKIP_BUILD=true
            shift
            ;;
        -d|--debug)
            DEBUG=true
            set -x
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Determine which kernels to test
if [ "$KERNELS_ONLY" = true ]; then
    if [ ${#CUSTOM_KERNELS[@]} -eq 0 ]; then
        echo -e "${RED}Error: --kernels-only specified but no kernels provided with --kernel${NC}"
        exit 1
    fi
    KERNELS=("${CUSTOM_KERNELS[@]}")
else
    KERNELS=("${DEFAULT_KERNELS[@]}" "${CUSTOM_KERNELS[@]}")
fi

echo "=== Local eBPF Testing with LVH v$VERSION ==="
echo "Testing on kernels: ${KERNELS[@]}"
echo ""

# Check if we're in the right directory
if [ ! -f "ebpf/Makefile" ]; then
    echo -e "${RED}Error: Not in the system-agent-kernel-testing directory${NC}"
    echo "Please run this script from the repository root"
    exit 1
fi

# Build eBPF programs
if [ "$SKIP_BUILD" = true ]; then
    echo -e "${YELLOW}1. Skipping eBPF build (--skip-build specified)${NC}"
else
    echo -e "${YELLOW}1. Building eBPF programs...${NC}"
    cd ebpf
    if [ ! -f "include/vmlinux.h" ]; then
        echo "Generating vmlinux.h..."
        ./scripts/generate_vmlinux.sh || {
            echo "Failed to generate vmlinux.h, using fallback..."
            mkdir -p include
            curl -sL https://raw.githubusercontent.com/libbpf/libbpf-bootstrap/master/examples/c/vmlinux.h -o include/vmlinux.h
        }
    fi

    echo "Building eBPF programs..."
    make clean all || {
        echo -e "${RED}Build failed. Ensure you have clang and libbpf-dev installed.${NC}"
        exit 1
    }
    cd ..
fi

# List built programs
echo -e "\n${YELLOW}2. Built eBPF programs:${NC}"
find internal/ebpf -name "*.bpf.o" -type f | while read -r obj; do
    echo "  - $obj"
done

# Create test directory
TEST_DIR="lvh-ebpf-test"
mkdir -p "$TEST_DIR"

# Copy eBPF objects
echo -e "\n${YELLOW}3. Copying eBPF objects to test directory...${NC}"
mkdir -p "$TEST_DIR/ebpf-objects"
find internal/ebpf -name "*.bpf.o" -type f -exec cp {} "$TEST_DIR/ebpf-objects/" \;

# Create the test script that will run inside VM
cat > "$TEST_DIR/test-in-vm.sh" << 'EOF'
#!/bin/bash
set -e

echo "=== BPF Test Runner (Inside VM) ==="
echo "Date: $(date)"
echo "Hostname: $(hostname)"
echo "Kernel: $(uname -r)"
echo "Architecture: $(uname -m)"

# Check for bpftool
if ! command -v bpftool &> /dev/null; then
    echo "Installing bpftool..."
    apt-get update -qq
    apt-get install -y -qq bpftool || {
        echo "ERROR: Failed to install bpftool"
        exit 1
    }
fi

# Mount BPF filesystem if needed
if ! mount | grep -q "type bpf"; then
    echo "Mounting BPF filesystem..."
    mount -t bpf bpf /sys/fs/bpf
fi

# Test each BPF program
cd /host/ebpf-objects
TOTAL=0
PASSED=0
FAILED=0

echo ""
echo "=== Testing eBPF Programs ==="

for obj in *.bpf.o; do
    if [ -f "$obj" ]; then
        TOTAL=$((TOTAL + 1))
        echo -n "Testing $obj: "
        
        if bpftool prog load "$obj" /sys/fs/bpf/test_prog 2>&1; then
            echo "✅ PASS"
            PASSED=$((PASSED + 1))
            # Show program info
            bpftool prog show name test_prog || true
            # Cleanup
            rm -f /sys/fs/bpf/test_prog
        else
            echo "❌ FAIL"
            FAILED=$((FAILED + 1))
        fi
    fi
done

echo ""
echo "=== Test Summary ==="
echo "Total programs: $TOTAL"
echo "Passed: $PASSED"
echo "Failed: $FAILED"

if [ $FAILED -eq 0 ]; then
    echo ""
    echo "✅ All BPF programs passed verification on kernel $(uname -r)"
    exit 0
else
    echo ""
    echo "❌ Some BPF programs failed verification"
    exit 1
fi
EOF

chmod +x "$TEST_DIR/test-in-vm.sh"

# Download VM image if needed
echo -e "\n${YELLOW}4. Preparing VM image...${NC}"
cd "$TEST_DIR"
if [ ! -f "debian.qcow2" ]; then
    echo "Downloading Debian cloud image..."
    wget -q -O debian.qcow2 https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2
    echo -e "${GREEN}✓ Downloaded VM image${NC}"
fi

# Test with different kernels
echo -e "\n${YELLOW}5. Testing with different kernels...${NC}"
echo "Will test ${#KERNELS[@]} kernel(s): ${KERNELS[*]}"
echo ""

OVERALL_FAILED=0

for kernel in "${KERNELS[@]}"; do
    echo -e "\n${YELLOW}Testing kernel $kernel...${NC}"
    
    # Try to download kernel if not present
    if [ ! -d "$kernel" ]; then
        echo "Downloading kernel $kernel..."
        lvh kernels pull "$kernel" || {
            echo -e "${RED}Failed to download kernel $kernel, skipping${NC}"
            continue
        }
    fi
    
    # Find kernel file
    KERNEL_FILE=$(find "$kernel/boot" -name "vmlinuz*" -o -name "bzImage*" 2>/dev/null | head -1)
    
    if [ -n "$KERNEL_FILE" ]; then
        echo "Found kernel at: $KERNEL_FILE"
        echo "Running VM with kernel $kernel..."
        echo "(Press Ctrl+C to stop after seeing results)"
        
        lvh run \
            --kernel "$KERNEL_FILE" \
            --image debian.qcow2 \
            --host-mount $(pwd) \
            -- /host/test-in-vm.sh || {
                echo -e "${RED}Test failed for kernel $kernel${NC}"
                OVERALL_FAILED=$((OVERALL_FAILED + 1))
            }
    else
        echo -e "${RED}No kernel file found for $kernel${NC}"
        OVERALL_FAILED=$((OVERALL_FAILED + 1))
    fi
done

# Final summary
echo ""
echo -e "${GREEN}=== Local testing complete ===${NC}"
echo "Tested ${#KERNELS[@]} kernel(s)"

if [ $OVERALL_FAILED -eq 0 ]; then
    echo -e "${GREEN}✅ All kernel tests completed successfully${NC}"
    exit 0
else
    echo -e "${RED}❌ $OVERALL_FAILED kernel(s) had issues${NC}"
    echo "Check the detailed output above for specifics"
    exit 2
fi