# Memory Growth Monitor Testing Methodology

> **Note**: Comprehensive testing documentation has been moved to the [System Agent Wiki](https://github.com/antimetal/system-agent/wiki/Memory-Monitoring/Testing-Methodology).
>
> ⚠️ **DRAFT**: This feature is currently in development on the `mem_monitor` branch.

## Quick Overview

The memory growth monitoring system includes a comprehensive test suite with:

- **Detector-specific simulators** - Test each detection algorithm independently
- **Combined tests** - Validate multi-detector interaction
- **False positive tests** - Ensure legitimate patterns aren't flagged
- **Performance benchmarks** - Verify <0.1% overhead target

## Test Programs

Located in `test/memory-leak-simulators/`:

| Simulator | Purpose | Target Detector |
|-----------|---------|-----------------|
| `linear_growth.c` | Steady 1MB/s leak | Linear Regression |
| `anon_ratio.c` | 90%+ heap allocations | RSS Ratio |
| `vsz_divergence.c` | mmap without touch | Threshold (VSZ/RSS) |
| `monotonic_growth.c` | No decreases for 5+ min | Threshold (Monotonic) |
| `combined_leak.c` | Trigger all detectors | All three |
| `cache_growth.c` | File cache expansion | False positive test |

## Running Tests

```bash
cd test/memory-leak-simulators

# Build all tests
make build-tests

# Run individual test
./bin/vsz_divergence

# Run full test suite
./run-tests.sh
```

## Documentation

For detailed testing procedures, validation criteria, and debugging guides, see the [Testing Methodology](https://github.com/antimetal/system-agent/wiki/Memory-Monitoring/Testing-Methodology) in the wiki.

---
*Branch: `mem_monitor` | Status: In Development*