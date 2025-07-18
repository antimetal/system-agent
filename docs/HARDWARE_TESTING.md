# Hardware Collector Testing Guide

This document describes the real hardware testing infrastructure for hardware collectors in the Antimetal agent.

## Overview

The hardware collectors need to work across diverse hardware configurations. Our testing strategy uses:

1. **Real Hardware Testing** - Run collectors on actual GitHub runner hardware
2. **Cross-Platform Verification** - Test build compatibility across architectures  
3. **Hardware Sample Collection** - Gather real hardware data for test cases
4. **Cross-Compilation Testing** - Verify builds work for target architectures

## Test Matrix

The GitHub Actions workflow tests on real GitHub runner hardware:

| Platform | Architecture | Expected Result | Purpose |
|----------|--------------|-----------------|---------|
| Ubuntu Latest | x86_64 | ✅ Success | Primary Linux testing |
| Ubuntu 22.04 | x86_64 | ✅ Success | LTS compatibility |
| Ubuntu 20.04 | x86_64 | ✅ Success | Older LTS support |
| Ubuntu ARM64 | ARM64 | ✅ Success | ARM64 native testing |

## How It Works

### 1. Real Hardware Discovery

The workflow automatically discovers hardware on each GitHub runner:

- **CPU Information**: Reads actual `/proc/cpuinfo` and reports cores, architecture, flags
- **Memory Information**: Reads `/proc/meminfo` for real memory configuration  
- **NUMA Topology**: Checks for `/sys/devices/system/node/` and runs `numactl`
- **Block Devices**: Enumerates `/sys/block/` for storage devices
- **Network Interfaces**: Lists `/sys/class/net/` interfaces with types and drivers

### 2. Collector Testing

Each platform runs:

```bash
# Run all collector unit tests
go test ./pkg/performance/collectors -v

# Test benchmark tool execution  
cd tools/collector-bench && ./collector-bench -show-data
```

### 3. Hardware Sample Collection

On Linux platforms, the workflow automatically collects hardware samples and uploads them as artifacts that you can download and use for future testing.

### 4. Cross-Compilation Verification

Tests building for multiple architectures:

```bash
GOOS=linux GOARCH=amd64 go build tools/collector-bench/main.go
GOOS=linux GOARCH=arm64 go build tools/collector-bench/main.go  
GOOS=linux GOARCH=arm go build tools/collector-bench/main.go
```

## Running Tests Locally

### Basic Testing

```bash
# Run collector unit tests
go test ./pkg/performance/collectors -v

# Test benchmark tool
cd tools/collector-bench
go run main.go -show-data

# Check what hardware data is available
echo "=== CPU ===" && cat /proc/cpuinfo | head -20
echo "=== Memory ===" && cat /proc/meminfo | head -10
```

### Cross-Compilation Testing

```bash
# Test building for different architectures
GOOS=linux GOARCH=amd64 go build -o dist/collector-bench-amd64 tools/collector-bench/main.go
GOOS=linux GOARCH=arm64 go build -o dist/collector-bench-arm64 tools/collector-bench/main.go
GOOS=linux GOARCH=arm go build -o dist/collector-bench-arm tools/collector-bench/main.go
```

### Manual Hardware Discovery

```bash
# See what your system looks like
echo "=== CPU ==="
cat /proc/cpuinfo | head -20

echo "=== Memory ==="  
cat /proc/meminfo | head -10

echo "=== Block Devices ==="
ls /sys/block/

echo "=== Network Interfaces ==="
ls /sys/class/net/
```

## What Gets Tested

### Real Hardware Discovery

The workflow shows you exactly what hardware GitHub provides:

- **CPU**: Intel/AMD processors with varying core counts and features
- **Memory**: Different memory configurations across runner types  
- **Storage**: Various block device types (SSD, NVMe, etc.)
- **Network**: Virtual network interfaces in GitHub's infrastructure
- **NUMA**: Detection of multi-socket configurations (if present)

## Continuous Integration

The GitHub Actions workflow runs on:

- **Push** to paths affecting collectors
- **Pull Requests** modifying collector code
- **Manual trigger** via workflow_dispatch

### Test Results

- **Artifacts** are uploaded for each test run
- **Cross-compilation** is verified for all target architectures
- **Test summary** is generated showing results across all configurations

### Adding Self-Hosted Runners

For testing on real hardware, add self-hosted runners:

```yaml
# .github/workflows/real-hardware-tests.yml
jobs:
  real-hardware:
    runs-on: [self-hosted, arm64, numa]
    steps:
    - name: Run on real ARM64 NUMA system
      run: tools/collector-bench/collector-bench -bench -show-data
```

## Troubleshooting

### Common Issues

1. **QEMU emulation slow** - Expected, use shorter timeouts for emulated tests
2. **Missing test data** - Ensure `.cpuinfo` files exist in testdata/
3. **Permission errors** - Mock filesystem avoids real `/proc` and `/sys`
4. **Cross-compilation fails** - Check Go version and target architecture support

### Debug Failed Tests

1. Download test artifacts from GitHub Actions
2. Check mock filesystem structure in artifacts
3. Verify test data matches expected format
4. Run tests locally with verbose output

### Adding Debug Output

```go
func TestDebugCollector(t *testing.T) {
    // Enable verbose logging
    config := performance.DefaultCollectionConfig()
    config.HostProcPath = os.Getenv("TEST_PROC_PATH")
    config.HostSysPath = os.Getenv("TEST_SYS_PATH")
    
    collector := collectors.NewCPUInfoCollector(logr.TestLogger(t), config)
    
    data, err := collector.Collect(context.Background())
    if err != nil {
        t.Logf("Collector error: %v", err)
    }
    t.Logf("Collected data: %+v", data)
}
```

## Performance Expectations

Expected test execution times:

| Test Type | x86_64 Native | ARM64 Emulated | ARM32 Emulated |
|-----------|---------------|----------------|----------------|
| Unit Tests | 10-30s | 60-120s | 90-180s |
| Integration | 30-60s | 120-300s | 180-400s |
| Cross-compilation | 5-15s | 5-15s | 5-15s |

## Contributing

When adding new hardware support:

1. Collect samples from real hardware
2. Add test configurations following the existing pattern
3. Ensure tests pass on both native and emulated platforms
4. Document any architecture-specific behavior
5. Update this guide if adding new test patterns