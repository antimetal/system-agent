#!/bin/bash
# Test PR using act to run GitHub workflows locally in Lima VM

set -euo pipefail

# Configuration
PR_NUMBER="${1:-}"
VM_NAME="act-test-${PR_NUMBER:-local}"
KEEP_VM=false
VERBOSE=false

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Help function
show_help() {
    cat << EOF
Usage: $0 [PR_NUMBER] [OPTIONS]

Run GitHub Actions workflows locally using act in a Lima VM.

Arguments:
    PR_NUMBER    PR number to test (optional, uses current branch if not specified)

Options:
    --keep-vm    Keep the VM after testing (default: delete)
    --verbose    Enable verbose output
    --help       Show this help message

Examples:
    # Test PR 106
    $0 106

    # Test current branch and keep VM
    $0 --keep-vm

    # Test PR 106 with verbose output
    $0 106 --verbose

Note: This script requires Lima to be installed (brew install lima)
EOF
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --help)
            show_help
            ;;
        --keep-vm)
            KEEP_VM=true
            shift
            ;;
        --verbose)
            VERBOSE=true
            shift
            ;;
        *)
            if [[ -z "$PR_NUMBER" && "$1" =~ ^[0-9]+$ ]]; then
                PR_NUMBER="$1"
            fi
            shift
            ;;
    esac
done

# Logging functions
log() {
    echo -e "${GREEN}[$(date +'%H:%M:%S')]${NC} $*"
}

error() {
    echo -e "${RED}[ERROR]${NC} $*" >&2
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

# Cleanup function
cleanup() {
    if [[ "$KEEP_VM" == "false" ]] && limactl list | grep -q "$VM_NAME"; then
        log "Cleaning up VM: $VM_NAME"
        limactl stop "$VM_NAME" 2>/dev/null || true
        limactl delete "$VM_NAME" 2>/dev/null || true
    else
        log "VM preserved: $VM_NAME"
        log "To connect: limactl shell $VM_NAME"
        log "To delete: limactl delete $VM_NAME"
    fi
}

# Set trap for cleanup
trap cleanup EXIT

# Check prerequisites
check_prerequisites() {
    log "Checking prerequisites..."
    
    if ! command -v limactl &> /dev/null; then
        error "Lima is not installed. Install with: brew install lima"
        exit 1
    fi
    
    if ! command -v docker &> /dev/null; then
        warn "Docker not installed locally, will be installed in VM"
    fi
    
    log "Prerequisites check passed"
}

# Create and setup Lima VM
setup_vm() {
    log "Setting up Lima VM: $VM_NAME"
    
    # Check if VM already exists
    if limactl list | grep -q "$VM_NAME"; then
        warn "VM $VM_NAME already exists, using existing VM"
        limactl start "$VM_NAME" 2>/dev/null || true
    else
        # Create VM with Docker support
        cat > /tmp/lima-act-config.yaml << 'EOF'
images:
- location: "https://cloud-images.ubuntu.com/releases/22.04/release/ubuntu-22.04-server-cloudimg-amd64.img"
  arch: "x86_64"
- location: "https://cloud-images.ubuntu.com/releases/22.04/release/ubuntu-22.04-server-cloudimg-arm64.img"
  arch: "aarch64"

cpus: 4
memory: "8GiB"
disk: "50GiB"

mounts:
- location: "~"
  writable: true
- location: "/tmp/lima"
  writable: true

containerd:
  system: false
  user: false

provision:
- mode: system
  script: |
    #!/bin/bash
    set -eux
    # Update system
    apt-get update
    apt-get install -y ca-certificates curl gnupg lsb-release git make gcc
    
    # Install Docker
    curl -fsSL https://get.docker.com | sh
    usermod -aG docker $LIMA_CIDATA_USER
    
    # Install Go
    wget -q https://go.dev/dl/go1.24.0.linux-amd64.tar.gz
    tar -C /usr/local -xzf go1.24.0.linux-amd64.tar.gz
    echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
    rm go1.24.0.linux-amd64.tar.gz
    
    # Install act
    curl https://raw.githubusercontent.com/nektos/act/master/install.sh | bash
    
    # Install build dependencies for system-agent
    apt-get install -y clang llvm libelf-dev libbpf-dev linux-headers-$(uname -r)

ssh:
  loadDotSSHPubKeys: true

vmType: "qemu"
EOF
        
        log "Creating VM with Docker and act support..."
        limactl create --name="$VM_NAME" /tmp/lima-act-config.yaml
        limactl start "$VM_NAME"
        
        # Wait for VM to be ready
        log "Waiting for VM to be ready..."
        sleep 10
        
        # Verify Docker is working
        limactl shell "$VM_NAME" -- docker version > /dev/null 2>&1 || {
            error "Docker is not working in VM"
            exit 1
        }
    fi
    
    log "VM is ready"
}

# Clone repository and checkout PR
setup_repository() {
    log "Setting up repository in VM..."
    
    if [[ -n "$PR_NUMBER" ]]; then
        log "Cloning repository and checking out PR $PR_NUMBER"
        limactl shell "$VM_NAME" -- bash -c "
            cd ~
            rm -rf system-agent
            git clone https://github.com/antimetal/system-agent.git
            cd system-agent
            git fetch origin pull/$PR_NUMBER/head:pr-$PR_NUMBER
            git checkout pr-$PR_NUMBER
        "
    else
        log "Cloning repository (main branch)"
        limactl shell "$VM_NAME" -- bash -c "
            cd ~
            rm -rf system-agent
            git clone https://github.com/antimetal/system-agent.git
        "
    fi
    
    log "Repository ready"
}

# Run act to execute workflows
run_workflows() {
    log "Running GitHub workflows with act..."
    
    # Prepare act command
    ACT_CMD="cd ~/system-agent && act"
    
    if [[ "$VERBOSE" == "true" ]]; then
        ACT_CMD="$ACT_CMD --verbose"
    fi
    
    # Run the integration tests workflow
    log "Executing integration-tests workflow..."
    limactl shell "$VM_NAME" -- bash -c "
        export PATH=/usr/local/go/bin:\$PATH
        $ACT_CMD -W .github/workflows/integration-tests.yml \
            --container-architecture linux/amd64 \
            --rm \
            -P ubuntu-latest=catthehacker/ubuntu:act-latest \
            -P ubuntu-22.04=catthehacker/ubuntu:act-22.04 \
            --pull=false
    " || {
        error "Workflow execution failed"
        if [[ "$KEEP_VM" == "false" ]]; then
            warn "Use --keep-vm to preserve VM for debugging"
        fi
        return 1
    }
    
    log "Workflow execution completed successfully"
}

# Main execution
main() {
    log "Starting PR testing with act"
    
    check_prerequisites
    setup_vm
    setup_repository
    run_workflows
    
    log "Testing completed successfully!"
    
    if [[ "$KEEP_VM" == "true" ]]; then
        log "VM preserved for further testing"
        log "To run act manually:"
        log "  limactl shell $VM_NAME"
        log "  cd ~/system-agent"
        log "  act -W .github/workflows/integration-tests.yml"
    fi
}

# Run main function
main