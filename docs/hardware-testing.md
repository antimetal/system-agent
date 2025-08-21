# Hardware Testing Guide

This guide covers running hardware-specific tests for the System Agent on bare metal servers, particularly for eBPF profilers that require PMU (Performance Monitoring Unit) access.

## Overview

Hardware tests validate features that cannot be tested in virtual environments:
- Hardware Performance Monitoring Unit (PMU) events
- CPU cache events and counters
- NUMA topology and memory access patterns
- Uncore PMU events (memory controllers, interconnects)
- High-frequency profiling capabilities

## Prerequisites

### Hardware Requirements
- **Bare metal server** (not a VM or container)
- **x86_64 or ARM64 architecture**
- **Linux kernel 5.8+** (for ring buffer support)
- **Root access** or CAP_BPF + CAP_PERFMON capabilities

### Recommended: Hetzner AX41-NVMe
- AMD Ryzen™ 5 3600 (6 cores/12 threads)
- 64 GB DDR4 RAM
- Full PMU access
- ~€41/month

## Quick Start

### 1. Deploy to Hetzner Server

```bash
# SSH to your Hetzner server
ssh root@your-server-ip

# Clone the repository
git clone https://github.com/antimetal/system-agent.git
cd system-agent

# Run hardware tests
sudo ./scripts/run-hardware-tests.sh
```

### 2. Manual Test Execution

```bash
# Build eBPF objects
make build-ebpf

# Build hardware tests
GOOS=linux GOARCH=amd64 go test -c -tags=hardware \
    -o /tmp/hardware-tests \
    ./pkg/performance/collectors

# Run tests
sudo ANTIMETAL_BPF_PATH=./ebpf/build /tmp/hardware-tests -test.v
```

## Test Categories

### PMU Enumeration Tests
Tests that enumerate and validate available PMU events:
```bash
./hardware-tests -test.v -test.run "TestPMU"
```

Validates:
- CPU PMU event discovery
- Cache event availability
- Uncore PMU detection
- Raw event configuration

### Hardware Profiler Tests
Tests the eBPF profiler with real hardware events:
```bash
./hardware-tests -test.v -test.run "TestHardwareProfiler"
```

Includes:
- Basic PMU event collection
- Multiple hardware event types
- High-frequency sampling (>1000 Hz)
- NUMA-aware profiling
- Stress testing

### Performance Benchmarks
Measures profiler overhead and throughput:
```bash
./hardware-tests -test.bench "BenchmarkHardware" -test.benchtime=10s
```

## Interpreting Results

### Successful Test Output
```
=== RUN   TestHardwareProfiler_PMUEvents
    profiler_hardware_test.go:45: Found 45 CPU PMU events:
    profiler_hardware_test.go:47:   - cpu-cycles: config=0x0
    profiler_hardware_test.go:47:   - instructions: config=0x1
    profiler_hardware_test.go:47:   - cache-references: config=0x2
    profiler_hardware_test.go:65: Collected 523 events in 5s
--- PASS: TestHardwareProfiler_PMUEvents (5.12s)
```

### Common Issues

#### No PMU Support
```
No PMU support detected - this test requires bare metal hardware
```
**Solution**: Run on bare metal, not in a VM.

#### High perf_event_paranoid
```
perf_event_paranoid level: 2
```
**Solution**: Set to -1 for testing: `echo -1 > /proc/sys/kernel/perf_event_paranoid`

#### Missing Capabilities
```
Failed to remove memlock limit
```
**Solution**: Run as root or with CAP_BPF capability.

## Hardware Event Types

### Standard Hardware Events
- `cpu-cycles`: CPU clock cycles
- `instructions`: Retired instructions
- `cache-references`: Cache accesses
- `cache-misses`: Cache miss events
- `branch-instructions`: Branch instructions
- `branch-misses`: Branch mispredictions

### x86-Specific Events
- L1/L2/L3 cache events
- TLB events
- Memory bandwidth events
- Intel/AMD-specific performance counters
- PMU exposed as `/sys/bus/event_source/devices/cpu`

### ARM64-Specific Events
- ARM PMU exposed as `/sys/bus/event_source/devices/armv8_pmuv3_0`
- Different event naming conventions:
  - `cpu_cycles` instead of `cpu-cycles`
  - `inst_retired` instead of `instructions`
  - `l1d_cache` instead of `cache-references`
  - `br_retired` instead of `branch-instructions`
- Additional ARM events:
  - `stall_frontend` / `stall_backend`: Pipeline stalls
  - `exc_taken` / `exc_return`: Exception handling
  - `ttbr_write_retired`: Translation table updates

## CI Integration

### GitHub Actions Workflow
```yaml
name: Hardware Tests
on:
  schedule:
    - cron: '0 2 * * *'  # Daily at 2 AM
  workflow_dispatch:

jobs:
  hardware-test:
    runs-on: self-hosted  # Requires self-hosted runner on bare metal
    steps:
      - uses: actions/checkout@v4
      - name: Run Hardware Tests
        run: sudo ./scripts/run-hardware-tests.sh
      - name: Upload Results
        uses: actions/upload-artifact@v4
        with:
          name: hardware-test-results
          path: /tmp/hardware-test-results-*.tar.gz
```

## Test Development

### Adding New Hardware Tests

1. Create test file with `//go:build hardware` tag:
```go
//go:build hardware

package collectors

import (
    "testing"
    // ...
)

func TestMyHardwareFeature(t *testing.T) {
    requireBareMetal(t)  // Skip if not on bare metal
    // Test implementation
}
```

2. Use hardware-specific APIs:
```go
// Open raw PMU event
attr := unix.PerfEventAttr{
    Type:   unix.PERF_TYPE_RAW,
    Config: 0x0451,  // Raw event code
    // ...
}
fd, err := unix.PerfEventOpen(&attr, -1, cpu, -1, 0)
```

### Testing Matrix

| Test Scenario | Kernel Version | PMU Required | Duration |
|--------------|---------------|--------------|----------|
| Basic Profiling | 5.8+ | No | ~5s |
| Hardware Events | 5.8+ | Yes | ~30s |
| High Frequency | 5.8+ | Yes | ~10s |
| NUMA Testing | 5.8+ | Yes | ~10s |
| Stress Testing | 5.8+ | Yes | ~5m |

## Troubleshooting

### Debug Commands

```bash
# Check PMU availability
ls -la /sys/bus/event_source/devices/

# List available events
ls /sys/bus/event_source/devices/cpu/events/

# Check perf capabilities
perf list

# Test basic perf event
perf stat -e cpu-cycles sleep 1

# Check kernel config
grep CONFIG_PERF /boot/config-$(uname -r)
```

### Common Error Messages

| Error | Cause | Solution |
|-------|-------|----------|
| `invalid argument` | Unsupported event | Check PMU capabilities |
| `permission denied` | Insufficient privileges | Run as root |
| `no such file or directory` | Missing PMU support | Use bare metal |
| `resource unavailable` | Event conflict | Stop other profilers |

## Best Practices

1. **Always validate hardware environment** before running tests
2. **Use appropriate sampling frequencies** (99 Hz for normal, up to 1000 Hz for testing)
3. **Monitor system impact** during high-frequency profiling
4. **Clean up resources** after test completion
5. **Document hardware-specific requirements** in test comments
6. **Handle architecture differences** - PMU paths and event names vary between x86 and ARM
7. **Use feature detection** - Check for PMU capabilities at runtime, not compile time

## Advanced Topics

### Custom PMU Events
```go
// Intel Skylake L1D cache load misses
rawConfig := uint64(0x0151)  // Event 0x51, Umask 0x01

// AMD Zen 2 retired instructions
rawConfig := uint64(0x00C0)  // Event 0xC0
```

### Event Groups
```go
// Create event group for IPC calculation
leaderFd := openPerfEvent(PERF_COUNT_HW_CPU_CYCLES)
memberFd := openPerfEvent(PERF_COUNT_HW_INSTRUCTIONS, leaderFd)
```

### NUMA Optimization
```go
// Pin profiler to specific NUMA node
cpu := getNumaNodeCPUs(node)[0]
openPerfEvent(config, -1, cpu, -1, 0)
```

## References

- [Linux perf_event Documentation](https://www.kernel.org/doc/html/latest/admin-guide/perf/)
- [Intel 64 and IA-32 Architectures SDM](https://www.intel.com/sdm)
- [AMD Processor Programming Reference](https://developer.amd.com/resources/developer-guides-manuals/)
- [ARM Architecture Reference Manual](https://developer.arm.com/documentation/)
- [eBPF Profiling Guide](https://www.brendangregg.com/blog/2019-01-01/learn-ebpf-tracing.html)