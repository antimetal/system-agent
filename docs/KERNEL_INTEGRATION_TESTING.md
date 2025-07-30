# Kernel Integration Testing Guide

This guide explains how to use and extend the kernel integration testing framework for the Antimetal Agent.

## Overview

The kernel integration testing framework uses [LVH (Little VM Helper)](https://github.com/cilium/little-vm-helper) to test the agent across different Linux kernel versions. This ensures compatibility with various kernel features, particularly for eBPF programs and system interfaces.

## Quick Start

### Running Tests Locally

While the full kernel integration tests require GitHub Actions, you can run the test suite on your current kernel:

```bash
# Run all integration tests
cd test/integration
go test -v ./...

# Run specific test suites
go test -v -run TestKernelCompatibility
go test -v -run TestEBPFProgramLoading
go test -v -run TestCollectorCompatibility
```

**Note**: These tests are Linux-specific and will skip on macOS or Windows systems. To run the full test suite across multiple kernel versions, use the GitHub Actions workflow or the local VM testing scripts.

### Triggering GitHub Actions

The kernel integration tests run automatically on:
- Pull requests that modify collectors or eBPF code
- Pushes to main branch
- Manual workflow dispatch

To manually trigger:
```bash
gh workflow run ebpf-vm-verifier-tests.yml
```

## Test Structure

### Directory Layout
```
test/integration/
├── kernel_compat_test.go    # Kernel feature detection tests
├── ebpf_test.go            # eBPF-specific tests
├── collectors_test.go      # Performance collector tests
├── config.yaml             # Test configuration
├── scripts/
│   ├── run-integration-tests.sh    # Main test runner
│   ├── setup-test-env.sh          # VM environment setup
│   └── test-workflow-locally.sh   # Local validation script
└── testdata/               # Test fixtures (if needed)
```

### Test Categories

1. **Kernel Feature Detection** (`kernel_compat_test.go`)
   - BTF support verification
   - CO-RE compatibility checks
   - BPF helper availability
   - Ring buffer vs perf buffer support

2. **eBPF Tests** (`ebpf_test.go`)
   - Program loading on different kernels
   - Map type support
   - CO-RE relocation testing
   - Verifier compatibility

3. **Collector Tests** (`collectors_test.go`)
   - Collector initialization
   - Data collection integrity
   - Error handling
   - Kernel version-specific features

## Supported Kernel Versions

The test matrix includes:
- **4.18**: Minimum for CO-RE support (partial)
- **5.2**: First kernel with native BTF support
- **5.4**: Ubuntu 20.04 LTS
- **5.8**: First kernel with BPF ring buffer support
- **5.15**: Ubuntu 22.04 LTS, excellent eBPF support
- **6.1**: LTS kernel with advanced eBPF features
- **6.6**: Latest LTS kernel with cutting-edge eBPF features

## Adding New Tests

### 1. Adding a New Kernel Version

Edit `.github/workflows/ebpf-vm-verifier-tests.yml`:
```yaml
matrix:
  kernel:
    - '4.18'
    - '5.2'
    - '5.4'
    - '5.8'
    - '5.15'
    - '6.1'
    - '6.6'  # Add new version
```

Update `test/integration/config.yaml` with expected features:
```yaml
- version: "6.8"
  expected_features:
    co_re: full
    btf: native
    ring_buffer: true
    perf_buffer: true
    psi: true
    cgroup_v2: true
    btf_func: true
    bpf_cookie: true
    multi_attach: true
    collectors:
      all_basic: true
      ebpf_based: true
  notes: "Description of this kernel version"
```

### 2. Adding Collector Tests

Add test cases to `collectors_test.go`:
```go
func TestNewCollectorType(t *testing.T) {
    if runtime.GOOS != "linux" {
        t.Skip("Linux-specific test")
    }
    
    // Test implementation
}
```

### 3. Adding eBPF Tests

For new eBPF programs, add tests to `ebpf_test.go`:
```go
t.Run("NewProgram", func(t *testing.T) {
    // Check kernel requirements
    if currentKernel.Compare(KernelVersion{5, 8, 0}) < 0 {
        t.Skip("Requires kernel 5.8+")
    }
    
    // Test eBPF program loading
})
```

## Configuration

### Test Configuration (`config.yaml`)

The configuration file defines:
- Expected features per kernel version
- Collector requirements
- Test parameters
- eBPF program specifications

### Environment Variables

Tests respect these environment variables:
- `HOST_PROC`: Override `/proc` path (default: `/proc`)
- `HOST_SYS`: Override `/sys` path (default: `/sys`)
- `HOST_DEV`: Override `/dev` path (default: `/dev`)

## Debugging Failed Tests

### 1. Check Test Artifacts

GitHub Actions uploads test results:
```bash
# Download artifacts
gh run download <run-id>
```

Artifacts include:
- `test-results/`: Test output files
- `test-logs/`: Detailed logs

### 2. Run Specific Tests

To debug a specific test:
```bash
# Run with verbose output
go test -v -run TestName ./test/integration

# Run with race detector
go test -race -run TestName ./test/integration
```

### 3. Check Kernel Requirements

Verify kernel features on your system:
```bash
# Check kernel version
uname -r

# Check BTF support
ls -la /sys/kernel/btf/vmlinux

# Check BPF filesystem
mount | grep bpf

# Check kernel config
zgrep CONFIG_BPF /proc/config.gz
```

## CI/CD Integration

### Workflow Triggers

The workflow runs on:
- Pull requests modifying:
  - `pkg/performance/collectors/**`
  - `ebpf/**`
  - Test files themselves
- Pushes to main
- Manual dispatch

### Local Testing Scripts

For local development and testing, use these scripts:

1. **`scripts/debug-lvh-locally.sh`** - Debug LVH installation and VM functionality
   ```bash
   ./scripts/debug-lvh-locally.sh --help  # See all options
   ./scripts/debug-lvh-locally.sh --kernel 5.15-main
   ```

2. **`scripts/test-ebpf-locally.sh`** - Test eBPF programs across multiple kernels locally
   ```bash
   ./scripts/test-ebpf-locally.sh --help  # See all options
   ./scripts/test-ebpf-locally.sh --kernel 6.1-main --kernel 6.6-main
   ```

Both scripts require Linux with KVM support and LVH installed.

### Failure Handling

Tests use a non-fail-fast strategy:
- All kernel versions are tested even if one fails
- Results are collected for analysis
- Clear error messages indicate kernel-specific issues

## Best Practices

### 1. Kernel Version Checks

Always check kernel version before using features:
```go
if currentKernel.Compare(KernelVersion{5, 8, 0}) < 0 {
    t.Skip("Feature requires kernel 5.8+")
}
```

### 2. Root Privilege Handling

Check for root when needed:
```go
if os.Geteuid() != 0 {
    t.Skip("Test requires root privileges")
}
```

### 3. Graceful Degradation

Test that collectors handle missing features:
```go
// Collector should work but with reduced functionality
result, err := collector.Collect(ctx)
assert.NoError(t, err)
assert.NotNil(t, result)
```

### 4. Resource Cleanup

Always clean up resources:
```go
collector, err := NewCollector()
require.NoError(t, err)
defer collector.Stop()
```

## Troubleshooting

### Common Issues

1. **"eBPF tests require root"**
   - Run tests with sudo: `sudo go test -v`
   - Or skip eBPF tests: `go test -v -run "^Test[^E]"`

2. **"BTF not available"**
   - Normal on kernels < 5.2
   - Check `/sys/kernel/btf/vmlinux` exists

3. **"Collector not registered"**
   - Ensure collector has init() function
   - Check registry registration

4. **GitHub Actions timeout**
   - Check for infinite loops in tests
   - Reduce collection intervals
   - Add context timeouts

### Getting Help

1. Check test output for specific error messages
2. Review kernel version requirements in `config.yaml`
3. Consult the [kernel documentation](https://www.kernel.org/doc/html/latest/)
4. Open an issue with test logs attached

## Future Enhancements

Planned improvements:
- Distribution-specific kernel testing (RHEL, SUSE)
- Performance benchmarking across kernels
- Stress testing under load
- Custom kernel configuration testing
- Integration with kernel CI systems