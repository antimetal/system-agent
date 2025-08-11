# Kernel Integration Testing Guide

This guide explains how to use and extend the kernel integration testing framework for the Antimetal Agent.

## Overview

The kernel integration testing framework uses [LVH (Little VM Helper)](https://github.com/cilium/little-vm-helper) to test the agent across different Linux kernel versions. This ensures compatibility with various kernel features, particularly for eBPF programs and system interfaces.

## Quick Start

### Running Tests Locally

While the full kernel integration tests require GitHub Actions, you can run the test suite on your current kernel:

```bash
# Run unit tests only (excluding integration tests)
make test-unit

# Run integration tests only
make test-integration

# Run all tests (unit and integration)
make test
```

**Note**: Integration tests are Linux-specific and require root privileges for eBPF tests. To run the full test suite across multiple kernel versions, use the GitHub Actions workflow or the local VM testing scripts.

### Triggering GitHub Actions

The kernel integration tests run automatically on:
- Pull requests that modify code in `pkg/**`, `internal/**`, `cmd/**`, `ebpf/**`, or workflows
- Pushes to main branch
- Manual workflow dispatch

To manually trigger:
```bash
gh workflow run integration-tests.yml
```

## Workflow Structure

### GitHub Actions Workflow

The `integration-tests.yml` workflow consists of these main jobs:

1. **unit-tests**: Runs unit tests on Ubuntu latest using `make test-unit`
2. **build-artifacts**: Builds eBPF programs and packages test artifacts
3. **integration-tests-vm**: Matrix job that runs tests in parallel VMs with different kernel versions (displays as "Integration Tests - Kernel X.Y")
4. **test-summary**: Aggregates results from all test jobs

#### Architecture Decision: Multiple Jobs vs Single Job

The workflow uses **multiple parallel jobs** instead of a single sequential job. Here's why:

**Pros of Multiple Jobs (Current Approach):**
- **Parallelization**: Tests run simultaneously, reducing total CI time from ~30 minutes to ~10 minutes
- **Fail-fast**: If one kernel version fails, others continue, providing complete test results
- **Resource isolation**: Each job gets fresh resources, preventing cross-contamination
- **Better debugging**: Failures are isolated to specific kernel versions with dedicated logs
- **Scalability**: Easy to add/remove kernel versions without affecting others
- **GitHub UI**: Clear visual separation of test results per kernel in the UI

**Cons of Multiple Jobs:**
- **Setup overhead**: Each job needs to download artifacts and set up environment (~30s per job)
- **Artifact management**: Requires uploading/downloading artifacts between jobs
- **Complexity**: More moving parts in the workflow configuration

**Alternative: Single Job Approach**
- Would run all kernel tests sequentially in one job
- Pros: Simpler workflow, no artifact passing, single log file
- Cons: 3x slower (sequential), harder to debug failures, one failure could block all tests

**Decision**: We use multiple jobs for the significant performance benefit (3x faster) and better debugging experience, accepting the minor complexity overhead

### Workflow Scripts

The workflow uses external scripts located in `.github/workflows/scripts/`:

```
.github/workflows/scripts/
├── build-ebpf.sh              # Builds eBPF programs with CO-RE support
├── package-artifacts.sh       # Packages eBPF programs and test scripts
├── run-tests-in-vm.sh        # Runs tests in VM environment (generic)
├── run-lvh-tests.sh          # Runs tests in LVH environment
└── generate-test-summary.sh  # Generates GitHub Actions summary from results
```

## Test Structure

### Integration Test Files

Integration tests use the `//go:build integration` build tag to separate them from unit tests:

```
pkg/
├── ebpf/
│   └── core/
│       ├── core_test.go              # Unit tests
│       └── ebpf_integration_test.go  # Integration tests
├── kernel/
│   ├── version_test.go               # Unit tests
│   ├── version_integration_test.go   # Integration tests
│   └── kernel_compat_integration_test.go
└── performance/
    └── collectors/
        ├── *_test.go                  # Unit tests
        └── *_integration_test.go      # Integration tests
```

### Test Categories

1. **Kernel Feature Detection** (`pkg/kernel/kernel_compat_integration_test.go`)
   - BTF support verification
   - CO-RE compatibility checks
   - BPF helper availability
   - Ring buffer vs perf buffer support

2. **eBPF Tests** (`pkg/ebpf/core/ebpf_integration_test.go`)
   - Program loading on different kernels
   - Map type support
   - CO-RE relocation testing
   - Verifier compatibility

3. **Collector Tests** (`pkg/performance/collectors/*_integration_test.go`)
   - Collector initialization with real filesystems
   - Data collection integrity
   - Error handling
   - Kernel version-specific features

## Supported Kernel Versions

The current test matrix includes:
- **5.4**: Ubuntu 20.04 LTS
- **5.10**: Stable kernel with good eBPF support
- **5.15**: Ubuntu 22.04 LTS, excellent eBPF support
- **6.1**: LTS kernel with advanced eBPF features
- **6.6**: Current LTS kernel with cutting-edge eBPF features
- **6.12**: Next LTS kernel with latest eBPF and performance features

## Adding New Tests

### 1. Adding a New Kernel Version

Edit `.github/workflows/integration-tests.yml`:
```yaml
matrix:
  include:
    - kernel: "5.4-20250616.013250"   # Ubuntu 20.04 LTS
    - kernel: "5.10-20250616.013250"
    - kernel: "5.15-20250616.013250"  # Ubuntu 22.04 LTS
    - kernel: "6.1-20250616.013250"   # LTS kernel
    - kernel: "6.6-20250616.013250"   # Current LTS kernel
    - kernel: "6.12-20250805.082402"  # Next LTS kernel (6.12)
```

### 2. Adding Integration Tests

Create a new integration test file with the build tag:
```go
//go:build integration

package collectors_test

import (
    "testing"
    "runtime"
)

func TestNewCollectorIntegration(t *testing.T) {
    if runtime.GOOS != "linux" {
        t.Skip("Linux-specific test")
    }
    
    // Test implementation using real filesystem
}
```

### 3. Adding eBPF Tests

For new eBPF programs, add tests to `pkg/ebpf/core/ebpf_integration_test.go`:
```go
func TestNewEBPFProgram(t *testing.T) {
    // Check kernel requirements
    currentKernel, _ := kernel.GetCurrentVersion()
    if currentKernel.Compare(kernel.KernelVersion{5, 8, 0}) < 0 {
        t.Skip("Requires kernel 5.8+")
    }
    
    // Test eBPF program loading
}
```

## Configuration

### Environment Variables

The test scripts respect these environment variables:
- `GO_VERSION`: Go version to install (default: matches go.mod)
- `KERNEL_VERSION`: Expected kernel version for validation
- `EBPF_BUILD_DIR`: Directory containing eBPF programs (default: `ebpf/build`)
- `HOST_PROC`: Override `/proc` path (default: `/proc`)
- `HOST_SYS`: Override `/sys` path (default: `/sys`)

### Script Configuration

The workflow scripts include built-in configuration:
- **build-ebpf.sh**: Uses `make build-ebpf` to compile eBPF programs with CO-RE support
- **run-tests-in-vm.sh**: Validates kernel version, installs dependencies, runs tests
- **run-lvh-tests.sh**: Optimized for LVH environment with proper mounts
- **package-artifacts.sh**: Packages eBPF programs from `ebpf/build`, filtering out test programs
- **generate-test-summary.sh**: Creates GitHub Actions summary with test results

## Debugging Failed Tests

### 1. Check Test Artifacts

GitHub Actions uploads test results with 7-day retention:
```bash
# Download artifacts
gh run download <run-id>

# View test results
cat test-results-5.15-20250616.013250/integration-test-results.txt
```

### 2. Run Tests Locally

Build and test eBPF programs locally:
```bash
# Build eBPF programs
make build-ebpf

# Run integration tests on current kernel
sudo make test-integration
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
- All pull requests (except documentation-only changes)
- All pushes to main (except documentation-only changes)
- Manual dispatch

Workflow is skipped only when changes are exclusively to:
- Markdown files (`**.md`)
- Documentation directory (`docs/**`)

### Job Dependencies

```
unit-tests ──────────┐
                     ├──> test-summary
build-artifacts ──┐  │
                  ↓  │
integration-tests-vm ┘
```

### Artifact Flow

1. **build-artifacts** job:
   - Builds eBPF programs using `build-ebpf.sh`
   - Packages artifacts using `package-artifacts.sh`
   - Uploads as `test-artifacts`

2. **integration-tests-vm** job:
   - Downloads `test-artifacts`
   - Runs tests in VMs using `run-lvh-tests.sh`
   - Uploads test results per kernel version

3. **test-summary** job:
   - Downloads all test results
   - Generates summary report

## Best Practices

### 1. Use Build Tags

Always use the `//go:build integration` tag for integration tests:
```go
//go:build integration

package mypackage_test
```

### 2. Check Kernel Version

Check kernel requirements before running tests:
```go
currentKernel, err := kernel.GetCurrentVersion()
require.NoError(t, err)

if currentKernel.Compare(kernel.KernelVersion{5, 8, 0}) < 0 {
    t.Skip("Feature requires kernel 5.8+")
}
```

### 3. Root Privilege Handling

Check for root when needed:
```go
if os.Geteuid() != 0 {
    t.Skip("Test requires root privileges")
}
```

### 4. Resource Cleanup

Always clean up resources:
```go
collector, err := NewCollector()
require.NoError(t, err)
defer collector.Stop()
```

### 5. Proper Error Handling

The workflow scripts use `set -euo pipefail` for strict error handling. Follow this pattern in custom scripts.

## Troubleshooting

### Common Issues

1. **"eBPF tests require root"**
   - Run tests with sudo: `sudo go test -tags integration -v`
   - Or use the VM-based testing scripts

2. **"BTF not available"**
   - Normal on kernels < 5.2
   - Check `/sys/kernel/btf/vmlinux` exists
   - Some tests may skip on older kernels

3. **"No eBPF programs found"**
   - Run `make build-ebpf` first
   - Check `ebpf/build/` for `.bpf.o` files
   - Verify clang/llvm installation

4. **GitHub Actions timeout**
   - Check for infinite loops in tests
   - Add context timeouts
   - Consider splitting large test suites

### Getting Help

1. Check test output for specific error messages
2. Review the workflow scripts in `.github/workflows/scripts/`
3. Consult the [kernel documentation](https://www.kernel.org/doc/html/latest/)
4. Open an issue with test logs attached

