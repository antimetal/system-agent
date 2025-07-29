# eBPF Testing Infrastructure

This directory contains testing tools and configurations for validating eBPF programs across different Linux kernel versions.

## Overview

The testing infrastructure uses Lima VMs to test eBPF programs (specifically `execsnoop`) across different kernel versions to ensure CO-RE (Compile Once - Run Everywhere) compatibility.

## Components

### Test Script: `test-ebpf-load.sh`

Load verification script that:
- Builds eBPF programs with CO-RE support using Docker
- Verifies BPF objects can be loaded on different kernel versions
- Tests BTF support and kernel compatibility
- Generates verification reports showing load success/failure

### VM Configurations

- `lima-vms/ubuntu-kernel-5.8.yaml` - Ubuntu 22.04 LTS with kernel 5.15+ (supports CAP_BPF)
- `lima-vms/ubuntu-modern-kernel.yaml` - Ubuntu 24.04 LTS with kernel 6.8+ (latest features)

## Setting Up Test VMs

1. Install Lima:
   ```bash
   brew install lima
   ```

2. Create test VMs:
   ```bash
   limactl create --name=ebpf-kernel-5.8 lima-vms/ubuntu-kernel-5.8.yaml
   limactl create --name=ebpf-modern lima-vms/ubuntu-modern-kernel.yaml
   ```

3. Start VMs:
   ```bash
   limactl start ebpf-kernel-5.8
   limactl start ebpf-modern
   ```

## Running Tests

Execute the load verification script from the project root:

```bash
./ebpf/testing/test-ebpf-load.sh
```

The script will:
1. Check prerequisites (Docker, Lima, VMs)
2. Build eBPF programs with CO-RE support using Docker
3. Copy BPF objects to each VM
4. Attempt to load programs using bpftool
5. Generate a summary report with load results

For full end-to-end testing with privilege scenarios, follow the manual testing steps in `TESTING_GUIDE.md`.

## Test Scenarios

The `test-ebpf-load.sh` script focuses on load verification:

### 1. BPF Object Loading
- Attempts to load BPF programs using bpftool
- Verifies BTF information in compiled objects
- Tests kernel verifier acceptance

### 2. Kernel Compatibility
- Tests loading on different kernel versions
- Checks for required kernel features (CONFIG_BPF, CONFIG_DEBUG_INFO_BTF)
- Identifies kernel-specific loading issues

### 3. CO-RE Functionality
- Verifies BTF support in kernel (/sys/kernel/btf/vmlinux)
- Validates CO-RE relocations in eBPF objects
- Ensures single binary works across kernels

For testing with different privilege levels (root, CAP_BPF, unprivileged), see the manual testing procedures in `TESTING_GUIDE.md`.

## Expected Results

### Kernel Compatibility Matrix

| Kernel Version | BTF Support | CAP_BPF | CAP_SYS_ADMIN | Root |
|----------------|-------------|---------|---------------|------|
| 5.15+ (Ubuntu 22.04) | Yes | Yes* | Yes | Yes |
| 6.8+ (Ubuntu 24.04) | Yes | Yes | Yes | Yes |

*Note: CAP_BPF requires kernel 5.8+

### Key Findings

1. **CO-RE Support**: Single eBPF binary works across all tested kernels
2. **Root Access**: Reliable across all kernel versions
3. **Modern Capabilities**: CAP_BPF + CAP_PERFMON for least privilege (5.8+)
4. **Legacy Support**: CAP_SYS_ADMIN works on older kernels
5. **BTF Integration**: Native support on modern kernels

## Troubleshooting

### VM Issues
- Ensure VMs are running: `limactl list`
- Check VM logs: `limactl shell <vm-name> -- dmesg | tail`
- Restart VM: `limactl stop <vm-name> && limactl start <vm-name>`

### eBPF Loading Failures
- Check kernel version: `limactl shell <vm-name> -- uname -r`
- Verify BTF support: `limactl shell <vm-name> -- ls /sys/kernel/btf/vmlinux`
- Check verifier errors: Look for "Failed to load" messages in test output

### Build Issues
- Ensure Docker is running with rootless mode enabled
- Check containerd snapshotter is enabled
- Verify clang version supports BTF generation

## Adding New Tests

To add a new test VM configuration:

1. Create a new Lima configuration in `lima-vms/`
2. Add the VM name to the `VMS` array in `test-ebpf-load.sh`
3. Run the load verification script to validate

To add new BPF programs to test:

1. Add the BPF object name to the `BPF_OBJECTS` array in `test-ebpf-load.sh`
2. Ensure the program is built by `make build-ebpf`
3. Run the script to verify loading on all VMs

## CI Integration

The test script is designed to be CI-friendly:
- Returns non-zero exit code on failures
- Provides structured output for parsing
- Supports headless operation