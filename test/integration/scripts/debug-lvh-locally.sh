#!/bin/bash
# Debug script for testing LVH locally
# This replicates what we're trying to do in GitHub Actions

set -x  # Enable debug output

echo "=== LVH Local Debug Script ==="
echo "This script will help debug LVH image issues locally"
echo ""

# Check if LVH is installed
if ! command -v lvh &> /dev/null; then
    echo "ERROR: lvh not found. Installing..."
    echo "Visit: https://github.com/cilium/little-vm-helper#installation"
    echo ""
    echo "Quick install options:"
    echo "1. Using Go: go install github.com/cilium/little-vm-helper/cmd/lvh@latest"
    echo "2. Using Docker: docker pull quay.io/lvh-images/lvh:latest"
    echo "3. Download from releases: https://github.com/cilium/little-vm-helper/releases"
    exit 1
fi

echo "✓ LVH found at: $(which lvh)"
echo "  Version: $(lvh version 2>&1 || echo 'version command not available')"
echo ""

# Test different image configurations
IMAGES=(
    "base:5.10"
    "base:5.15"
    "kind:5.10"
    "kind:5.15"
    "base:5.10-main"
    "kind:5.10-main"
)

echo "=== Testing Image Availability ==="
for img in "${IMAGES[@]}"; do
    IFS=':' read -r image version <<< "$img"
    echo ""
    echo "Testing: image=$image, version=$version"
    
    # Try to pull the image
    echo "Attempting: lvh images pull --image $image --image-version $version"
    if lvh images pull --image "$image" --image-version "$version" 2>&1; then
        echo "✓ Successfully pulled $img"
    else
        echo "✗ Failed to pull $img"
    fi
done

echo ""
echo "=== Testing VM Start ==="
echo "Trying to start a VM with base:5.10..."

# Create a test directory
TEST_DIR="/tmp/lvh-test-$$"
mkdir -p "$TEST_DIR"
echo "Test directory: $TEST_DIR"

# Create a simple test script
cat > "$TEST_DIR/test.sh" << 'EOF'
#!/bin/bash
echo "=== VM Started Successfully ==="
echo "Kernel: $(uname -r)"
echo "OS: $(cat /etc/os-release | grep PRETTY_NAME || echo 'Unknown')"
echo "Date: $(date)"
echo "✓ If you see this, LVH is working!"
EOF
chmod +x "$TEST_DIR/test.sh"

# Try to run a VM
echo ""
echo "Attempting to start VM with various configurations..."
echo ""

# Configuration 1: Minimal
echo "1. Minimal configuration:"
echo "   lvh run --image base --image-version 5.10 --host-mount $TEST_DIR:/host -- /host/test.sh"
lvh run --image base --image-version 5.10 --host-mount "$TEST_DIR:/host" -- /host/test.sh 2>&1

echo ""
echo "2. With explicit port:"
echo "   lvh run --image base --image-version 5.10 --port 2222 --host-mount $TEST_DIR:/host -- /host/test.sh"
lvh run --image base --image-version 5.10 --port 2222 --host-mount "$TEST_DIR:/host" -- /host/test.sh 2>&1

echo ""
echo "3. With kind image:"
echo "   lvh run --image kind --image-version 5.10 --host-mount $TEST_DIR:/host -- /host/test.sh"
lvh run --image kind --image-version 5.10 --host-mount "$TEST_DIR:/host" -- /host/test.sh 2>&1

echo ""
echo "=== Checking QEMU ==="
if command -v qemu-system-x86_64 &> /dev/null; then
    echo "✓ QEMU found at: $(which qemu-system-x86_64)"
    qemu-system-x86_64 --version | head -1
else
    echo "✗ QEMU not found - this might be why VMs aren't starting"
    echo "  Install with: sudo apt-get install qemu-system-x86"
fi

echo ""
echo "=== Checking Docker ==="
if command -v docker &> /dev/null; then
    echo "✓ Docker found"
    echo "  Docker version: $(docker --version)"
    
    echo ""
    echo "4. Testing with Docker-based LVH:"
    echo "   docker run --rm -v $TEST_DIR:/host quay.io/lvh-images/lvh:latest run --image base --image-version 5.10 -- /host/test.sh"
    docker run --rm -v "$TEST_DIR:/host" quay.io/lvh-images/lvh:latest \
        run --image base --image-version 5.10 -- /host/test.sh 2>&1
else
    echo "✗ Docker not found"
fi

# Cleanup
rm -rf "$TEST_DIR"

echo ""
echo "=== Debug Summary ==="
echo "1. Check which images successfully pulled"
echo "2. Check if any VM configuration started"
echo "3. Look for error messages about missing dependencies"
echo "4. Note any 'ssh: connect to host localhost port 2222: Connection refused' errors"
echo ""
echo "Common issues:"
echo "- Missing QEMU: Install with 'sudo apt-get install qemu-system-x86'"
echo "- Wrong image format: Use 'base' or 'kind' with versions like '5.10'"
echo "- Permissions: May need to run with sudo for KVM access"
echo "- KVM not available: Check /dev/kvm exists"