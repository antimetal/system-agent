# Lima VM Configurations for eBPF Testing

This directory contains Lima VM configurations for testing eBPF functionality across different kernel versions.

## Available Configurations

### rocky-kernel-4.18.yaml
- **OS**: Rocky Linux 8 (RHEL 8 clone)
- **Kernel**: 4.18 (minimal CO-RE support)
- **Purpose**: Test eBPF with minimal kernel version for CO-RE
- **SSH Port**: 60124
- **Key Features**:
  - Kernel 4.18 is the minimum for CO-RE support
  - Limited BTF support (may require external BTF)
  - Basic eBPF functionality
  - Tests backward compatibility

### ubuntu-kernel-5.8.yaml
- **OS**: Ubuntu 22.04 LTS (Jammy Jellyfish)
- **Kernel**: 5.15+ (supports CAP_BPF and CAP_PERFMON)
- **Purpose**: Test eBPF with new capability model (non-root)
- **SSH Port**: 60122
- **Key Features**:
  - Kernel 5.15 supports CAP_BPF and CAP_PERFMON (introduced in 5.8)
  - Native BTF support
  - Full CO-RE (Compile Once - Run Everywhere)
  - Good for testing minimal privilege requirements
  - Stable LTS release with extended support

### ubuntu-modern-kernel.yaml
- **OS**: Ubuntu 24.04 LTS (Noble Numbat)
- **Kernel**: 6.8+ (latest stable)
- **Purpose**: Test eBPF with modern kernel features
- **SSH Port**: 60123
- **Key Features**:
  - Full BTF support
  - Latest eBPF features and performance improvements
  - Advanced verifier capabilities
  - Rosetta support for ARM64 Macs

### arch-latest-kernel.yaml
- **OS**: Arch Linux (rolling release)
- **Kernel**: Latest available (6.11+)
- **Purpose**: Test eBPF with cutting-edge kernel features
- **SSH Port**: 60125
- **Key Features**:
  - Bleeding edge kernel features
  - Experimental eBPF capabilities
  - Latest verifier improvements
  - Note: Uses x86_64 emulation on ARM64 hosts

## Usage

### Creating VMs

```bash
# Create Rocky Linux 8 VM with kernel 4.18 (minimal CO-RE)
limactl create --name=rocky-4.18 ./rocky-kernel-4.18.yaml
limactl start rocky-4.18

# Create Ubuntu 22.04 VM with kernel 5.15+
limactl create --name=ebpf-kernel-5.8 ./ubuntu-kernel-5.8.yaml
limactl start ebpf-kernel-5.8

# Create Ubuntu 24.04 VM with modern kernel
limactl create --name=ebpf-modern ./ubuntu-modern-kernel.yaml
limactl start ebpf-modern

# Create Arch Linux VM with latest kernel
limactl create --name=arch-latest ./arch-latest-kernel.yaml
limactl start arch-latest
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

## Kernel Feature Matrix

| Feature | Rocky 4.18 | Ubuntu 5.15 | Ubuntu 6.8 | Arch Latest |
|---------|------------|-------------|------------|-------------|
| **Kernel Version** | 4.18.x | 5.15.x | 6.8.x | 6.11+ |
| **Basic eBPF** | ✅ | ✅ | ✅ | ✅ |
| **CO-RE Support** | ⚠️ Limited | ✅ Full | ✅ Full | ✅ Full |
| **Native BTF** | ❌ | ✅ | ✅ | ✅ |
| **BTF in vmlinux** | ❌ | ✅ | ✅ | ✅ |
| **CAP_BPF** | ❌ | ✅ | ✅ | ✅ |
| **CAP_PERFMON** | ❌ | ✅ | ✅ | ✅ |
| **BPF Ring Buffer** | ❌ | ✅ | ✅ | ✅ |
| **BPF Timer** | ❌ | ✅ | ✅ | ✅ |
| **BPF Iterator** | ❌ | ✅ | ✅ | ✅ |
| **Sleepable BPF** | ❌ | ⚠️ Basic | ✅ | ✅ |
| **BPF LSM** | ❌ | ✅ | ✅ | ✅ |
| **Task Local Storage** | ❌ | ✅ | ✅ | ✅ |
| **Typed dynptrs** | ❌ | ❌ | ✅ | ✅ |
| **BPF Arena** | ❌ | ❌ | ❌ | ✅ |

### Legend
- ✅ Full Support
- ⚠️ Partial/Limited Support
- ❌ Not Available

### Notes
- **Rocky 4.18**: Requires external BTF files for CO-RE. Basic eBPF works but with limitations.
- **Ubuntu 5.15**: First LTS with full CO-RE and new capability model (CAP_BPF/CAP_PERFMON).
- **Ubuntu 6.8**: Modern stable kernel with all production-ready eBPF features.
- **Arch Latest**: Bleeding edge, may include experimental features not yet in stable kernels.

## Testing Across All VMs

Run the provided test script to verify eBPF functionality across all kernel versions:

```bash
cd /path/to/antimetal-agent/ebpf/testing
./test-ebpf-load.sh
```

This will:
1. Build eBPF programs in Docker
2. Test loading on each VM
3. Report compatibility status
4. Show kernel feature availability
