# Multi-Detector Memory Growth Monitor Architecture

> **Note**: Detailed technical documentation is available in the [System Agent Wiki](https://github.com/antimetal/system-agent/wiki/Memory-Monitoring/Overview).
>
> ⚠️ **DRAFT**: This feature is currently in development on the `mem_monitor` branch.

## Overview

The Antimetal Agent's Memory Growth Monitor implements a three-detector approach to memory leak detection using eBPF technology. All detectors monitor the Linux kernel's `kmem:rss_stat` tracepoint to analyze process memory behavior with <0.1% CPU overhead.

## The Three Detectors

1. **Linear Regression Detector** (`memgrowth.bpf.c`)
   - Statistical trend analysis for steady growth patterns
   - 16-element circular buffer with MB-resolution storage
   - Event coalescing to extend history coverage

2. **RSS Component Ratio Detector** (`memgrowth_rss_ratio.bpf.c`)
   - Analyzes memory composition (anonymous vs file-backed)
   - Distinguishes heap leaks from cache growth
   - Tracks growth rates by memory type

3. **Multi-Factor Threshold Detector** (`memgrowth_thresholds.bpf.c`)
   - VSZ/RSS divergence detection (Microsoft SWAT research)
   - Monotonic growth duration tracking (Google TCMalloc)
   - Anonymous memory ratio thresholds (Facebook OOMD)

## Architecture Diagram

```
Linux Kernel (5.8+)
    │
    ├─► kmem:rss_stat tracepoint
    │       │
    │       ├─► Linear Regression Detector
    │       ├─► RSS Ratio Detector
    │       └─► Threshold Detector
    │
    └─► Ring Buffer → Go Collector → Intake Service
```

## Key Features

- **Multiple validation signals** from independent detectors
- **<0.1% CPU overhead** in production environments
- **2-10 minute advance warning** before OOM events
- **In-kernel analysis** with zero network overhead
- **High confidence** through cross-validation

## Detection Capabilities

| Leak Type | Linear | Ratio | Threshold | Combined |
|-----------|--------|-------|-----------|----------|
| Slow steady leak | ✅ Excellent | ✅ Good | ✅ Good | ✅ Excellent |
| Fast leak | ✅ Good | ✅ Good | ✅ Excellent | ✅ Excellent |
| Heap fragmentation | ❌ Poor | ✅ Good | ✅ Excellent | ✅ Excellent |
| Cache growth | ✅ Filters | ✅ Excellent | ✅ Good | ✅ Excellent |

## Implementation Status

- ✅ **Complete**: eBPF detector implementations, test simulators
- 🚧 **In Progress**: Go collector integration
- 📋 **Planned**: Container awareness, ML enhancements

## Documentation

For detailed technical documentation, see the [System Agent Wiki](https://github.com/antimetal/system-agent/wiki/Memory-Monitoring/Overview):

- [Implementation Guide](https://github.com/antimetal/system-agent/wiki/Memory-Monitoring/Implementation-Guide)
- [Testing Methodology](https://github.com/antimetal/system-agent/wiki/Memory-Monitoring/Testing-Methodology)
- [Linear Regression Detector](https://github.com/antimetal/system-agent/wiki/Memory-Monitoring/Detectors/Linear-Regression-Detector)
- [RSS Ratio Detector](https://github.com/antimetal/system-agent/wiki/Memory-Monitoring/Detectors/RSS-Ratio-Detector)
- [Threshold Detector](https://github.com/antimetal/system-agent/wiki/Memory-Monitoring/Detectors/Threshold-Detector)

## Files

- `ebpf/src/memgrowth.bpf.c` - Linear regression detector
- `ebpf/src/memgrowth_rss_ratio.bpf.c` - RSS ratio detector
- `ebpf/src/memgrowth_thresholds.bpf.c` - Threshold detector
- `ebpf/include/memgrowth_types.h` - Shared data structures
- `test/memory-leak-simulators/` - Test programs

---
*Branch: `mem_monitor` | Status: In Development*