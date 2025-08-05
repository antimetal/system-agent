# Local VM Testing with LVH

This guide explains how to test eBPF programs and collectors locally using LVH (Little VM Helper) on different kernel versions.

## Overview

Our CI pipeline compiles and validates eBPF programs, but actual kernel loading tests must be done locally with VMs. This approach:
- ✅ Keeps CI fast and reliable
- ✅ Avoids complex VM setup in GitHub Actions
- ✅ Allows thorough testing across multiple kernels locally

## Prerequisites

1. **Linux machine** with KVM support (physical or VM)
   - On macOS: Use Lima to create a Linux VM first
   - On Windows: Use WSL2 with KVM support
2. **LVH installed** (see installation section)
3. **QEMU/KVM support** enabled
4. **Built eBPF programs** (run `make build-ebpf` in the repo root)

### Setting up on macOS with Lima

```bash
# Install Lima
brew install lima

# Create a Linux VM with KVM support
limactl start --name=lvh-testing --cpus=4 --memory=8 template://ubuntu-lts

# Enter the VM
limactl shell lvh-testing

# Now follow the Linux instructions inside the VM
```

## Installing LVH

```bash
# Option 1: Using Go
go install github.com/cilium/little-vm-helper/cmd/lvh@latest
export PATH="$PATH:$(go env GOPATH)/bin"

# Option 2: Download binary
wget https://github.com/cilium/little-vm-helper/releases/download/v0.0.26/lvh_v0.0.26_linux_amd64.tar.gz
tar xzf lvh_v0.0.26_linux_amd64.tar.gz
sudo mv lvh /usr/local/bin/

# Verify installation
lvh --help
```

## VM Images

LVH can use pre-built VM images or download them automatically:

```bash
# Download a Debian cloud image (done automatically by scripts)
wget https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2

# Or use LVH's image management
lvh images pull quay.io/lvh-images/base:latest
```

## Quick Start Scripts

We provide two helper scripts for local testing:

### 1. Debug LVH Setup
```bash
# Test if LVH is working correctly
./scripts/debug-lvh-locally.sh --help
./scripts/debug-lvh-locally.sh --kernel 5.15-main
```

### 2. Test eBPF Programs
```bash
# Test eBPF programs on multiple kernels
./scripts/test-ebpf-locally.sh --help
./scripts/test-ebpf-locally.sh --kernel 5.10-main --kernel 6.1-main
```

## Testing eBPF Programs

### 1. Build eBPF Programs

```bash
# Build all eBPF programs with CO-RE support
cd ebpf
make clean all
cd ..

# Or use the test script which handles this
./scripts/test-ebpf-locally.sh
```

### 2. Pull Kernel Images

```bash
# Pull specific kernels
lvh kernels pull 5.10-main
lvh kernels pull 5.15-main
lvh kernels pull 6.1-main

# List downloaded kernels
ls -la ~/.lvh/kernels/
```

### 3. Run Tests in VM

```bash
# Option 1: Use the automated test script
./scripts/test-ebpf-locally.sh --kernel 5.15-main

# Option 2: Manual testing with LVH
lvh run --kernel 5.15-main \
        --image debian-12-genericcloud-amd64.qcow2 \
        --host-mount $(pwd):/host \
        -- /host/test/integration/scripts/ebpf-verifier-test.sh
```

### 4. Automated Multi-Kernel Testing

Use our provided script for testing across multiple kernels:

```bash
# Test on default kernels (5.10, 5.15, 6.1)
./scripts/test-ebpf-locally.sh

# Test on specific kernels
./scripts/test-ebpf-locally.sh --kernel 4.19-main --kernel 6.6-main

# Skip build if already built
./scripts/test-ebpf-locally.sh --skip-build
```

The script automatically:
- Downloads specified kernels
- Downloads a Debian VM image
- Tests each eBPF program on each kernel
- Reports compatibility results

## Troubleshooting

### Common Issues

1. **"qemu-system-x86_64: command not found"**
   ```bash
   sudo apt-get install qemu-system-x86
   ```

2. **"Could not access KVM kernel module"**
   ```bash
   sudo modprobe kvm-intel  # or kvm-amd
   sudo chmod 666 /dev/kvm
   ```

3. **SSH connection refused**
   - Wait longer for VM to boot
   - Check if SSH is enabled in the image
   - Try using `--provision` flag

4. **Image not found**
   - Build images first with `lvh images build`
   - Check image path is correct

## CI/CD Integration

Our testing workflow:
1. **GitHub Actions** compiles eBPF programs with CO-RE support
2. **eBPF VM Verifier Tests** workflow validates programs on multiple kernels
3. **Local testing** provides additional verification during development

The GitHub Actions workflow (`ebpf-vm-verifier-tests.yml`):
- Builds eBPF programs once
- Tests loading on kernels: 5.10, 5.15, 6.1, 6.5
- Uploads test results as artifacts
- Runs on PRs modifying eBPF code

## Testing Performance Collectors

Beyond eBPF programs, you can test all collectors:

```bash
# Run integration tests in a VM
lvh run --kernel 5.15-main \
        --image debian.qcow2 \
        --host-mount $(pwd):/host \
        -- "cd /host && go test -v ./test/integration/..."
```

## Available Test Scripts

- `test/integration/scripts/ebpf-verifier-test.sh` - Tests eBPF program loading
- `test/integration/scripts/run-integration-tests.sh` - Runs full integration test suite
- `test/integration/scripts/setup-test-env.sh` - Sets up test environment in VM
- `test/integration/scripts/test-workflow-locally.sh` - Validates CI workflow locally

## References

- [Cilium LVH](https://github.com/cilium/little-vm-helper) - Official LVH repository
- [Our eBPF VM Tests Workflow](.github/workflows/ebpf-vm-verifier-tests.yml)
- [Kernel Integration Testing Guide](./KERNEL_INTEGRATION_TESTING.md)