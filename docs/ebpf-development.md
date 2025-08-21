# eBPF Development Guide

This guide covers eBPF development for the Antimetal Agent, including CO-RE (Compile Once - Run Everywhere) support, development workflows, and best practices.

## CO-RE (Compile Once - Run Everywhere) Support

The Antimetal Agent uses **CO-RE** technology for portable eBPF programs that work across different kernel versions without recompilation. This provides significant operational benefits:

### Key Benefits
- **Single Binary Deployment**: Same eBPF program runs on kernels 4.18+
- **Automatic Field Relocation**: Kernel structure changes handled automatically
- **No Runtime Compilation**: Pre-compiled programs with BTF relocations
- **Improved Reliability**: Reduced kernel compatibility issues

### Technical Implementation

#### 1. BTF (BPF Type Format) Support
- Native kernel BTF on kernels 5.2+ at `/sys/kernel/btf/vmlinux`
- Pre-generated vmlinux.h from BTF hub for portability
- BTF verification during build process

#### 2. Compilation Flags
```makefile
CFLAGS := -g -O2 -Wall -target bpf -D__TARGET_ARCH_$(ARCH) \
    -fdebug-types-section -fno-stack-protector
```
- `-g`: Enable BTF generation
- `-fdebug-types-section`: Improve BTF quality
- `-fno-stack-protector`: Required for BPF

#### 3. CO-RE Macros
- Use `BPF_CORE_READ()` for field access
- Automatic offset calculation at load time
- Example: `BPF_CORE_READ(task, real_parent, tgid)`

#### 4. Runtime Support
- `pkg/ebpf/core` package for kernel feature detection
- Automatic BTF discovery and loading
- cilium/ebpf v0.19.0 handles relocations

### Kernel Compatibility Matrix

| Kernel Version | BTF Support | CO-RE Support | Notes |
|----------------|-------------|---------------|-------|
| 5.2+           | Native      | Full          | Best performance, native BTF |
| 4.18-5.1       | External    | Partial       | Requires BTF from btfhub |
| <4.18          | None        | None          | Traditional compilation only |

## CO-RE Development Workflow

### 1. Write CO-RE Compatible Code
```c
#include "vmlinux.h"
#include <bpf/bpf_core_read.h>

// Use CO-RE macros for kernel struct access
pid_t ppid = BPF_CORE_READ(task, real_parent, tgid);
```

### 2. Build with CO-RE Support
```bash
make build-ebpf  # Automatically uses CO-RE flags
```

### 3. Verify BTF Generation
- Build process includes BTF verification step
- Check with: `bpftool btf dump file <program>.bpf.o`

### 4. Test Compatibility
```bash
./ebpf/scripts/check_core_support.sh  # Check system CO-RE support
```

## Adding New eBPF Programs

### For new `.bpf.c` files:
1. Create `ebpf/src/your_program.bpf.c`
2. Include CO-RE headers:
   ```c
   #include "vmlinux.h"
   #include <bpf/bpf_core_read.h>
   ```
3. Use CO-RE macros for kernel struct access
4. Add to `BPF_PROGS` in `ebpf/Makefile`
5. Run `make build-ebpf`

### For new struct definitions:
1. Create `ebpf/include/your_collector_types.h` with C structs
2. Run `make generate-ebpf-types` to generate Go types
3. Generated files appear in `pkg/performance/collectors/`

## eBPF Commands

| Command | Purpose |
|---------|---------|
| `make build-ebpf` | Build eBPF programs with CO-RE support |
| `make generate-ebpf-bindings` | Generate Go bindings from eBPF C code |
| `make generate-ebpf-types` | Generate Go types from eBPF header files |
| `make build-ebpf-builder` | Build/rebuild eBPF Docker image |
| `./ebpf/scripts/check_core_support.sh` | Check system CO-RE capabilities |

## CO-RE Best Practices

### 1. Always Use CO-RE Macros
- Prefer `BPF_CORE_READ()` over direct field access
- Use `BPF_CORE_READ_STR()` for string fields
- Check field existence with `BPF_CORE_FIELD_EXISTS()`

### 2. Test Across Kernels
- Test on minimum supported kernel (4.18)
- Verify on latest stable kernel
- Use KIND clusters with different kernel versions

### 3. Handle Missing Fields Gracefully
- Not all kernel versions have all fields
- Use conditional compilation or runtime checks
- Provide fallback behavior

### 4. Monitor BTF Size
- BTF adds ~100KB to each eBPF object
- Worth it for portability benefits
- Strip BTF for size-critical deployments if needed

## Troubleshooting CO-RE

### BTF Verification Failures
- Ensure clang has `-g` flag
- Check clang version (10+ recommended)
- Verify vmlinux.h is accessible

### Runtime Loading Errors
- Check kernel has BTF: `ls /sys/kernel/btf/vmlinux`
- Verify CO-RE support: `./ebpf/scripts/check_core_support.sh`
- Check dmesg for BPF verifier errors

### Field Access Errors
- Ensure using CO-RE macros not direct access
- Verify field exists in target kernel version
- Check struct definitions in vmlinux.h

## Example eBPF Collector

For a complete example of an eBPF-based collector implementation, see:
- `ebpf/src/execsnoop.bpf.c` - eBPF C program
- `pkg/performance/collectors/execsnoop.go` - Go integration
- `pkg/performance/collectors/execsnoop_test.go` - Testing approach

## Hardware Testing for eBPF Profilers

### Overview
eBPF profilers that use `perf_event` programs require access to hardware Performance Monitoring Units (PMUs), which are typically not available in virtualized environments. This necessitates testing on bare metal hardware.

### When Hardware Testing is Required
- **Perf event-based profilers**: CPU cycles, instructions, cache events
- **Hardware PMU counters**: L1/L2/L3 cache misses, TLB events
- **Uncore events**: Memory controller, interconnect monitoring
- **High-frequency sampling**: Testing overhead and event rates

### Running Hardware Tests
```bash
# Build with hardware tag
GOOS=linux GOARCH=amd64 go test -c -tags=hardware \
    -o hardware-tests ./pkg/performance/collectors

# Run on bare metal server
sudo ANTIMETAL_BPF_PATH=./ebpf/build ./hardware-tests -test.v

# Or use the automated script
sudo ./scripts/run-hardware-tests.sh
```

### Key Differences from VM Testing
| Feature | VM Testing | Hardware Testing |
|---------|------------|------------------|
| PMU Access | ❌ Not available | ✅ Full access |
| perf_event_open | Limited/Fails | Full functionality |
| Hardware Events | Software only | All event types |
| Event Rates | Low/Unreliable | Accurate sampling |

### Common Issues and Solutions

#### "invalid argument" on perf_event attachment
- **In VMs**: Expected - PMU not available
- **Solution**: Use software events (SW_CPU_CLOCK) or test on bare metal

#### Integration tests hanging
- **Cause**: Perf events not generating samples
- **Solution**: Ensure proper `Sample_type` and `Wakeup` configuration

#### bpf_link not supported (kernel < 5.15)
- **Solution**: Use legacy ioctl attachment (PERF_EVENT_IOC_SET_BPF)
- **See**: `profiler.go:attachPerfEvent()` for implementation

### Hardware Test Infrastructure
For comprehensive hardware testing, we recommend:
- **Hetzner AX41-NVMe**: ~€41/month, full PMU access
- **AWS Bare Metal**: i3.metal instances (expensive)
- **Local servers**: Any physical Linux machine

For detailed hardware testing procedures, see [Hardware Testing Guide](hardware-testing.md).

## Additional Resources

- [Linux Kernel BPF Documentation](https://www.kernel.org/doc/html/latest/bpf/)
- [libbpf CO-RE Guide](https://nakryiko.com/posts/bpf-portability-and-co-re/)
- [BTF Hub for Pre-generated BTF](https://github.com/aquasecurity/btfhub)
- [cilium/ebpf Go Library](https://github.com/cilium/ebpf)