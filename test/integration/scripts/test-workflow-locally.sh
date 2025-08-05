#!/bin/bash
# Script to test the kernel integration workflow locally using act

set -euo pipefail

echo "=== Testing Kernel Integration Workflow Locally ==="

# Check if act is installed
if ! command -v act &> /dev/null; then
    echo "Error: 'act' is not installed."
    echo "Install it with: brew install act"
    exit 1
fi

# Check if Docker is running
if ! docker info &> /dev/null; then
    echo "Error: Docker is not running."
    echo "Please start Docker Desktop and try again."
    exit 1
fi

# Function to run act with specific kernel version
test_kernel_version() {
    local kernel_version=$1
    echo ""
    echo "Testing with kernel version: $kernel_version"
    echo "================================"
    
    # Create a custom event payload
    cat > /tmp/test-kernel-event.json <<EOF
{
  "pull_request": {
    "number": 1,
    "head": {
      "ref": "test-branch",
      "sha": "$(git rev-parse HEAD 2>/dev/null || echo "1234567890abcdef")"
    },
    "base": {
      "ref": "main"
    }
  }
}
EOF
    
    # Run act for specific kernel version
    # Note: This is a dry run since LVH requires actual GitHub Actions environment
    act pull_request \
        -j kernel-tests \
        --matrix kernel:$kernel_version \
        --eventpath /tmp/test-kernel-event.json \
        --dryrun \
        --verbose
}

# Check current directory
if [ ! -f ".github/workflows/kernel-integration-tests.yml" ]; then
    echo "Error: Must be run from repository root"
    echo "Current directory: $(pwd)"
    exit 1
fi

echo "Repository root: $(pwd)"
echo ""

# List available jobs
echo "Available jobs in kernel integration workflow:"
act -l --workflows .github/workflows/kernel-integration-tests.yml

echo ""
echo "Note: The kernel integration tests use LVH (Little VM Helper) which"
echo "requires GitHub Actions environment. We'll do a dry run to validate"
echo "the workflow syntax and job configuration."
echo ""

# Test with different kernel versions
kernels=("4.18" "5.4" "5.15")

for kernel in "${kernels[@]}"; do
    test_kernel_version "$kernel"
done

echo ""
echo "=== Local Validation Complete ==="
echo ""
echo "To run the actual kernel tests:"
echo "1. Push your changes to a branch"
echo "2. Create a pull request"
echo "3. The workflow will run automatically"
echo ""
echo "Or trigger manually with:"
echo "gh workflow run kernel-integration-tests.yml"

# Clean up
rm -f /tmp/test-kernel-event.json