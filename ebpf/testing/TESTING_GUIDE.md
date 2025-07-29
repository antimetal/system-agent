# eBPF Testing Guide: Building and Testing Across Kernels

This guide explains how to build eBPF programs on macOS using Docker and test them on Lima VMs with different kernel versions.

## Quick Start

For automated load verification across VMs, use:
```bash
./ebpf/testing/test-ebpf-load.sh
```

For detailed manual testing including privilege scenarios, follow the steps below.

## Prerequisites

- macOS development machine
- Docker Desktop installed and running
- Lima installed (`brew install lima`)
- QEMU installed (`brew install qemu`)
- Go 1.24+ installed

## Overview

1. Build eBPF programs on macOS using Docker (ensures consistent build environment)
2. Test on Lima VMs with different kernels to verify compatibility
3. Test both with root and with minimal privileges (CAP_BPF)

## Step 1: Build eBPF Programs on macOS

### Build All eBPF Programs

```bash
# From the project root
make build-ebpf
```

This will:
- Use the Docker builder image (`antimetal/ebpf-builder`)
- Generate vmlinux.h from BTF
- Compile all eBPF programs with CO-RE support
- Output files to `ebpf/build/`

### Build Specific eBPF Program

```bash
# Build just execsnoop
cd ebpf
make build/execsnoop.bpf.o
```

### Verify Build Output

```bash
ls -la ebpf/build/
# Should see: execsnoop.bpf.o and other .bpf.o files
```

## Step 2: Create and Start Lima VMs

### VM 1: Ubuntu 22.04 with Kernel 5.15+ (Supports CAP_BPF)

```bash
# Create VM
limactl create --name=ebpf-kernel-5.15 ./ebpf/testing/lima-vms/ubuntu-kernel-5.8.yaml

# Start VM
limactl start ebpf-kernel-5.15

# Verify kernel version
limactl shell ebpf-kernel-5.15 -- uname -r
# Expected: 5.15.0-xxx-generic
```

### VM 2: Ubuntu 24.04 with Kernel 6.8+ (Latest Features)

```bash
# Create VM
limactl create --name=ebpf-modern ./ebpf/testing/lima-vms/ubuntu-modern-kernel.yaml

# Start VM
limactl start ebpf-modern

# Verify kernel version
limactl shell ebpf-modern -- uname -r
# Expected: 6.8.0-xxx-generic
```

## Step 3: Build Test Binaries

### Build System Agent

```bash
# Build for Linux ARM64 (for Apple Silicon Macs)
make build
# Binary at: dist/linux/arm64/agent

# Build for Linux AMD64 (for Intel Macs)
GOARCH=amd64 make build
# Binary at: dist/linux/amd64/agent
```

### Build Example Programs

```bash
# Build execsnoop example
cd cmd/examples/execsnoop
GOOS=linux GOARCH=arm64 go build -o execsnoop-linux .
cd ../../..
```

## Step 4: Deploy and Test

### Deploy to VMs

```bash
# Function to deploy to a VM
deploy_to_vm() {
    local VM_NAME=$1
    
    # Create directories
    limactl shell $VM_NAME -- sudo mkdir -p /usr/local/lib/antimetal/ebpf/
    
    # Copy eBPF objects
    limactl copy ebpf/build/*.bpf.o $VM_NAME:/tmp/
    limactl shell $VM_NAME -- sudo cp /tmp/*.bpf.o /usr/local/lib/antimetal/ebpf/
    
    # Copy test binary
    limactl copy cmd/examples/execsnoop/execsnoop-linux $VM_NAME:/tmp/execsnoop
    limactl shell $VM_NAME -- chmod +x /tmp/execsnoop
}

# Deploy to both VMs
deploy_to_vm ebpf-kernel-5.15
deploy_to_vm ebpf-modern
```

### Test with Root (Verify Basic Functionality)

```bash
# Test on kernel 5.15
limactl shell ebpf-kernel-5.15 -- sudo /tmp/execsnoop

# Test on kernel 6.8
limactl shell ebpf-modern -- sudo /tmp/execsnoop
```

### Test with CAP_BPF (Minimal Privileges)

```bash
# Function to test with capabilities
test_with_caps() {
    local VM_NAME=$1
    
    # Install capability tools if needed
    limactl shell $VM_NAME -- sudo apt-get update
    limactl shell $VM_NAME -- sudo apt-get install -y libcap2-bin
    
    # Copy binary to user-accessible location
    limactl shell $VM_NAME -- cp /tmp/execsnoop ~/execsnoop
    
    # Set capabilities (CAP_BPF + CAP_PERFMON for kernel 5.8+)
    limactl shell $VM_NAME -- sudo setcap cap_bpf,cap_perfmon=eip ~/execsnoop
    
    # Verify capabilities
    limactl shell $VM_NAME -- getcap ~/execsnoop
    
    # Run without root
    limactl shell $VM_NAME -- ~/execsnoop
}

# Test on both VMs
test_with_caps ebpf-kernel-5.15
test_with_caps ebpf-modern
```

## Step 5: Troubleshooting

### Check BTF Support

```bash
limactl shell <VM_NAME> -- ls -la /sys/kernel/btf/vmlinux
```

### Check Capabilities

```bash
# Check current process capabilities
limactl shell <VM_NAME> -- capsh --print

# Check binary capabilities
limactl shell <VM_NAME> -- getcap /path/to/binary
```

### Debug BPF Loading Issues

```bash
# Check kernel logs
limactl shell <VM_NAME> -- sudo dmesg | tail -50

# List loaded BPF programs
limactl shell <VM_NAME> -- sudo bpftool prog list

# List BPF maps
limactl shell <VM_NAME> -- sudo bpftool map list
```

### Common Issues and Solutions

1. **"permission denied" when loading BPF**
   - Ensure BTF is available: `/sys/kernel/btf/vmlinux`
   - Check capabilities are set correctly
   - Verify kernel version supports CAP_BPF (5.8+)

2. **"unbounded memory access" verifier errors**
   - May indicate vmlinux.h mismatch
   - Try building BPF objects inside the target VM

3. **"executable file not found"**
   - Check architecture matches (arm64 vs amd64)
   - Ensure binary has execute permissions

## Building eBPF Inside VMs (Alternative Approach)

If CO-RE relocations fail, build directly in the target VM:

```bash
# Install build tools in VM
limactl shell <VM_NAME> -- sudo apt-get install -y \
    build-essential \
    clang \
    llvm \
    linux-tools-$(uname -r) \
    linux-headers-$(uname -r)

# Copy source code
limactl copy ebpf/ <VM_NAME>:/tmp/ebpf/

# Build inside VM
limactl shell <VM_NAME> -- bash -c "
    cd /tmp/ebpf
    # Generate vmlinux.h from local BTF
    sudo bpftool btf dump file /sys/kernel/btf/vmlinux format c > include/vmlinux.h
    # Build eBPF programs
    make build
"

# Copy back the built objects
limactl copy <VM_NAME>:/tmp/ebpf/build/*.bpf.o ./ebpf/build/
```

## Clean Up

```bash
# Stop VMs
limactl stop ebpf-kernel-5.15
limactl stop ebpf-modern

# Delete VMs
limactl delete ebpf-kernel-5.15
limactl delete ebpf-modern
```

## CI/CD Integration

For automated testing:

```yaml
# Example GitHub Actions workflow
test-ebpf:
  runs-on: ubuntu-latest
  strategy:
    matrix:
      kernel: ["5.15", "6.8"]
  steps:
    - name: Build eBPF
      run: make build-ebpf
    
    - name: Test on kernel ${{ matrix.kernel }}
      run: |
        # Use kernel-specific test container
        docker run --privileged \
          -v $PWD:/workspace \
          ubuntu:${{ matrix.kernel }} \
          /workspace/scripts/test-ebpf.sh
```

## Key Takeaways

1. **Build Once, Run Everywhere**: CO-RE enables building on macOS and running on Linux
2. **Test Multiple Kernels**: Different kernels have different BPF features and verifier behavior
3. **Minimal Privileges**: Use CAP_BPF instead of root on kernel 5.8+
4. **Verify BTF**: Ensure target systems have BTF support for CO-RE
5. **Architecture Matters**: Build for the correct target architecture (arm64/amd64)