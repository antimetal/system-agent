#!/bin/bash
#
# debug-lvh-locally.sh - Debug LVH (Little VM Helper) installation and functionality
#
# This script helps diagnose issues with LVH by testing various components:
# - LVH installation and version
# - Virtualization support (KVM on Linux)
# - Kernel download functionality
# - VM launching with different configurations
#
# Works on both Linux and macOS systems.

set -e

# Script version
VERSION="1.0.0"

# Function to display help
show_help() {
    cat << EOF
debug-lvh-locally.sh - Debug LVH (Little VM Helper) installation and functionality

USAGE:
    $0 [OPTIONS]

DESCRIPTION:
    This script helps diagnose issues with LVH by running a series of tests:
    1. Verifies LVH installation
    2. Checks virtualization support (KVM on Linux)
    3. Tests kernel download functionality
    4. Downloads a test VM image
    5. Attempts to run VMs with different configurations

    The script works on both Linux and macOS systems.

OPTIONS:
    -h, --help      Show this help message and exit
    -v, --version   Show script version
    -k, --kernel    Kernel version to test (default: 5.10-main)
    -s, --skip-vm   Skip VM launch tests (only test downloads)

EXAMPLES:
    # Run all tests with default kernel
    $0

    # Test with a specific kernel version
    $0 --kernel 6.1-main

    # Only test downloads, skip VM launches
    $0 --skip-vm

REQUIREMENTS:
    - LVH installed (go install github.com/cilium/little-vm-helper/cmd/lvh@latest)
    - On Linux: KVM support (virtualization enabled in BIOS)
    - On macOS: Virtualization framework support
    - Internet connection for downloading kernels and VM images
    - About 1GB free disk space for VM image

OUTPUT:
    The script creates a test directory 'lvh-debug-test' with:
    - Downloaded kernel files
    - Test VM image (debian.qcow2)
    - Test script that runs inside the VM

TROUBLESHOOTING:
    If LVH is not found:
        go install github.com/cilium/little-vm-helper/cmd/lvh@latest

    If KVM is not available on Linux:
        - Enable virtualization in BIOS
        - Install KVM: sudo apt-get install qemu-kvm libvirt-daemon-system

EXIT CODES:
    0 - All tests passed
    1 - Critical error (missing dependencies, download failures)

AUTHOR:
    Antimetal, Inc.

SEE ALSO:
    test-ebpf-locally.sh - Test eBPF programs on different kernels
    https://github.com/cilium/little-vm-helper

EOF
}

# Parse command line arguments
TEST_KERNEL="5.10-main"
SKIP_VM=false

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -v|--version)
            echo "debug-lvh-locally.sh version $VERSION"
            exit 0
            ;;
        -k|--kernel)
            TEST_KERNEL="$2"
            shift 2
            ;;
        -s|--skip-vm)
            SKIP_VM=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

echo "=== LVH Local Debugging Script v$VERSION ==="
echo "Testing with kernel: $TEST_KERNEL"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if LVH is installed
echo -e "${YELLOW}1. Checking LVH installation...${NC}"
if command -v lvh &> /dev/null; then
    echo -e "${GREEN}✓ LVH found at: $(which lvh)${NC}"
    lvh version 2>/dev/null || echo "LVH version command not available"
else
    echo -e "${RED}✗ LVH not found${NC}"
    echo "Install with: go install github.com/cilium/little-vm-helper/cmd/lvh@latest"
    exit 1
fi

# Check virtualization support
echo -e "\n${YELLOW}2. Checking virtualization support...${NC}"
if [[ "$OSTYPE" == "darwin"* ]]; then
    echo "Running on macOS - checking for virtualization framework"
    # On macOS, LVH might use different virtualization
else
    # On Linux
    if [ -e /dev/kvm ]; then
        echo -e "${GREEN}✓ KVM available${NC}"
    else
        echo -e "${RED}✗ KVM not available${NC}"
        echo "You may need to enable virtualization in BIOS or install KVM"
    fi
fi

# Test kernel download
echo -e "\n${YELLOW}3. Testing kernel download...${NC}"
TEST_KERNEL="5.10-main"
echo "Attempting to pull kernel: $TEST_KERNEL"

# Create a test directory
TEST_DIR="lvh-debug-test"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

# Pull kernel
echo "Running: lvh kernels pull $TEST_KERNEL"
if lvh kernels pull "$TEST_KERNEL"; then
    echo -e "${GREEN}✓ Kernel pull succeeded${NC}"
else
    echo -e "${RED}✗ Kernel pull failed${NC}"
    exit 1
fi

# Find what was downloaded
echo -e "\n${YELLOW}4. Examining downloaded files...${NC}"
echo "Contents of current directory:"
find . -type f | head -20

# Look for kernel files
echo -e "\nLooking for kernel files..."
if [ -d "$TEST_KERNEL" ]; then
    echo "Found kernel directory: $TEST_KERNEL"
    echo "Contents:"
    find "$TEST_KERNEL" -type f | head -20
    
    # Find the actual kernel file
    KERNEL_FILE=$(find "$TEST_KERNEL/boot" -name "vmlinuz*" -o -name "bzImage*" 2>/dev/null | head -1)
    if [ -n "$KERNEL_FILE" ]; then
        echo -e "${GREEN}✓ Found kernel file: $KERNEL_FILE${NC}"
    else
        echo -e "${RED}✗ No kernel file found${NC}"
    fi
else
    echo -e "${RED}✗ No kernel directory found${NC}"
fi

# Test VM image
echo -e "\n${YELLOW}5. Testing VM image...${NC}"
echo "Downloading a test VM image..."
if [ ! -f "test.qcow2" ]; then
    echo "Downloading Debian cloud image..."
    wget -q -O test.qcow2 https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Downloaded test VM image${NC}"
    else
        echo -e "${RED}✗ Failed to download VM image${NC}"
        exit 1
    fi
else
    echo "Using existing test.qcow2"
fi

# Create a simple test script
echo -e "\n${YELLOW}6. Creating test script...${NC}"
cat > test-script.sh << 'EOF'
#!/bin/bash
echo "=== VM Test Script ==="
echo "Hostname: $(hostname)"
echo "Kernel: $(uname -r)"
echo "Date: $(date)"
echo "✓ VM booted successfully!"
EOF
chmod +x test-script.sh

# Test running a VM
if [ "$SKIP_VM" = true ]; then
    echo -e "\n${YELLOW}7. Skipping VM launch tests (--skip-vm specified)${NC}"
else
    echo -e "\n${YELLOW}7. Testing LVH run command...${NC}"
    echo "Attempting to run a VM..."
    echo "Note: Each test will wait 3 seconds before starting. Press Ctrl+C to stop the VM after it boots."

    # Try different command variations
    echo -e "\nTest 1: With downloaded kernel path"
    if [ -n "$KERNEL_FILE" ]; then
        CMD="lvh run --kernel $(pwd)/$KERNEL_FILE --image test.qcow2 --host-mount $(pwd) -- /host/test-script.sh"
        echo "Command: $CMD"
        echo -e "${YELLOW}Starting in 3 seconds...${NC}"
        sleep 3
        $CMD || echo -e "${RED}Test 1 failed with exit code: $?${NC}"
    fi

    echo -e "\nTest 2: With kernel version string"
    CMD="lvh run --kernel $TEST_KERNEL --image test.qcow2 --host-mount $(pwd) -- /host/test-script.sh"
    echo "Command: $CMD"
    echo -e "${YELLOW}Starting in 3 seconds...${NC}"
    sleep 3
    $CMD || echo -e "${RED}Test 2 failed with exit code: $?${NC}"

    echo -e "\nTest 3: Without kernel (use VM's default)"
    CMD="lvh run --image test.qcow2 --host-mount $(pwd) -- /host/test-script.sh"
    echo "Command: $CMD"
    echo -e "${YELLOW}Starting in 3 seconds...${NC}"
    sleep 3
    $CMD || echo -e "${RED}Test 3 failed with exit code: $?${NC}"
fi

echo -e "\n${GREEN}=== Debugging complete ===${NC}"
echo "Check the output above to see which command format works"