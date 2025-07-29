# Lima VM Configurations for eBPF Testing

This directory contains Lima VM configurations for testing eBPF functionality across different kernel versions.

## Available Configurations

### ubuntu-kernel-5.8.yaml
- **OS**: Ubuntu 22.04 LTS (Jammy Jellyfish)
- **Kernel**: 5.15+ (supports CAP_BPF and CAP_PERFMON)
- **Purpose**: Test eBPF with new capability model (non-root)
- **Key Features**:
  - Kernel 5.15 supports CAP_BPF and CAP_PERFMON (introduced in 5.8)
  - Supports CO-RE (Compile Once - Run Everywhere)
  - Good for testing minimal privilege requirements
  - Stable LTS release with extended support

### ubuntu-modern-kernel.yaml
- **OS**: Ubuntu 24.04 LTS (Noble Numbat)
- **Kernel**: 6.8+ (latest stable)
- **Purpose**: Test eBPF with latest kernel features
- **Key Features**:
  - Full BTF support
  - Latest eBPF features and performance improvements
  - Rosetta support for ARM64 Macs

## Usage

### Creating VMs

```bash
# Create Ubuntu 22.04 VM with kernel 5.15+
limactl create --name=ebpf-kernel-5.8 ./ubuntu-kernel-5.8.yaml
limactl start ebpf-kernel-5.8

# Create Ubuntu 24.04 VM with modern kernel
limactl create --name=ebpf-modern ./ubuntu-modern-kernel.yaml
limactl start ebpf-modern
```

### Accessing VMs

```bash
# Shell into VM
limactl shell ebpf-kernel-5.8
limactl shell ebpf-modern

# Or use SSH directly
ssh -p 60122 lima@localhost  # kernel 5.15+
ssh -p 60123 lima@localhost  # modern kernel
```

### Testing eBPF

Once in the VM:

```bash
# Check kernel version
uname -r

# Check for BTF support
ls -la /sys/kernel/btf/vmlinux

# Check capabilities (when testing non-root)
capsh --print

# Build and test eBPF programs
cd /tmp/lima/path/to/project
make build-ebpf
```

### Cleaning Up

```bash
# Stop VMs
limactl stop ebpf-kernel-5.8
limactl stop ebpf-modern

# Delete VMs
limactl delete ebpf-kernel-5.8
limactl delete ebpf-modern
```

## Testing Scenarios

### 1. Capability-based eBPF (Kernel 5.8+)
Test running eBPF programs with CAP_BPF and CAP_PERFMON instead of root:
```bash
# In the VM
sudo setcap cap_bpf,cap_perfmon=eip ./your-ebpf-program
./your-ebpf-program  # Should work without sudo
```

### 2. CO-RE Compatibility
Test that CO-RE eBPF programs built on one kernel work on another:
```bash
# Build on modern kernel
limactl shell ebpf-modern -c "cd /tmp/lima/project && make build-ebpf"

# Test on older kernel
limactl shell ebpf-kernel-5.8 -c "cd /tmp/lima/project && ./test-ebpf"
```

### 3. BTF Availability
Compare BTF support across kernels:
```bash
# Both VMs should have BTF, but with different features
limactl shell ebpf-kernel-5.8 -c "bpftool btf dump file /sys/kernel/btf/vmlinux format c | head -20"
limactl shell ebpf-modern -c "bpftool btf dump file /sys/kernel/btf/vmlinux format c | head -20"
```

## Notes

- The VMs mount your home directory as read-only at `~`
- Writable mount is available at `/tmp/lima`
- Each VM uses different SSH ports to avoid conflicts
- VMs are configured with minimal resources: 2 CPUs, 1GB RAM, 20GB disk
- Only essential eBPF tools are installed (bpftool, linux-tools, libcap2-bin)