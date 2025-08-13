# Perf Event Enumeration and Portability Guide

This document describes the perf event enumeration system implemented in the Antimetal Agent's eBPF profiler and provides guidance for understanding Linux perf event portability across different hardware platforms.

## Overview

The Antimetal Agent includes comprehensive perf event enumeration functionality that discovers available performance monitoring events on the host system. This enables better error reporting, dynamic configuration, and debugging of eBPF profiler issues across different hardware platforms.

## Implementation

### Core Functionality

The enumeration system is implemented in [`pkg/performance/collectors/profiler_perf_events.go`](../pkg/performance/collectors/profiler_perf_events.go) and provides:

- **Real-time event discovery** using `perf_event_open()` validation
- **Hardware event enumeration** from standardized `PERF_COUNT_HW_*` constants  
- **Software event enumeration** for VM-compatible profiling
- **Raw PMU event discovery** from `/sys/devices/cpu/events/`
- **Enhanced error messages** with available alternatives

### API Usage

#### Event Discovery
```go
// Get all available events with details
events, err := collectors.EnumerateAvailablePerfEvents()
for _, event := range events {
    fmt.Printf("Event: %s (type=%d, config=0x%x, available=%v, source=%s)\n",
        event.Name, event.Type, event.Config, event.Available, event.Source)
}

// Get event summary statistics
summary, err := collectors.GetPerfEventSummary()
fmt.Printf("Found %d/%d available events\n", summary.AvailableEvents, summary.TotalEvents)

// Look up specific events by name
event, err := collectors.FindPerfEventByName("cache-misses")
```

#### Unified ProfilerCollector Setup
With the new unified API, all events (predefined and discovered) use the same `PerfEventConfig` structure:

```go
// Method 1: Using predefined events
profiler, _ := collectors.NewProfiler(logger, config)
err := profiler.Setup(collectors.NewProfilerConfig(collectors.CPUClockEvent))

// Method 2: Using discovered events
events, _ := collectors.EnumerateAvailablePerfEvents()
customEvent := collectors.PerfEventConfig{
    Name:         events[0].Name,
    Type:         events[0].Type,
    Config:       events[0].Config,
    SamplePeriod: 1000000,
}
err := profiler.Setup(collectors.ProfilerConfig{Event: customEvent})

// Method 3: Direct event creation
err := profiler.Setup(collectors.ProfilerConfig{
    Event: collectors.PerfEventConfig{
        Name:         "branch-instructions",
        Type:         0,  // PERF_TYPE_HARDWARE
        Config:       4,  // PERF_COUNT_HW_BRANCH_INSTRUCTIONS
        SamplePeriod: 500000,
    },
})
```

### Example Output

On a typical bare metal server, the enumeration discovers:

**Hardware Events (PERF_TYPE_HARDWARE):**
- `cpu-cycles` (config=0) ✅ Available
- `instructions` (config=1) ✅ Available 
- `cache-references` (config=2) ✅ Available
- `cache-misses` (config=3) ✅ Available
- `branch-instructions` (config=4) ✅ Available
- `branch-misses` (config=5) ✅ Available
- `bus-cycles` (config=6) ❌ Not available (hardware dependent)

**Software Events (PERF_TYPE_SOFTWARE):**
- `cpu-clock` (config=0) ✅ Available everywhere
- `task-clock` (config=1) ✅ Available everywhere
- `page-faults` (config=2) ✅ Available everywhere
- `context-switches` (config=3) ✅ Available everywhere

**Raw PMU Events (PERF_TYPE_RAW):**
- Vendor-specific events from `/sys/devices/cpu/events/`
- Example: `stalled-cycles-frontend` (config=0xa9)

## Linux Perf Event Architecture

### Standardized vs Vendor-Specific

Linux perf events use a **two-layer architecture**:

#### Layer 1: Generic Hardware Events
- **Standardized constants** defined in `linux/perf_event.h`
- **Portable across architectures** (x86, ARM, RISC-V, etc.)
- **Limited to ~10 common events** (cycles, instructions, cache events, etc.)

```c
// From linux/perf_event.h - these are standardized
enum perf_hw_id {
    PERF_COUNT_HW_CPU_CYCLES           = 0,
    PERF_COUNT_HW_INSTRUCTIONS         = 1,
    PERF_COUNT_HW_CACHE_REFERENCES     = 2,
    PERF_COUNT_HW_CACHE_MISSES         = 3,
    PERF_COUNT_HW_BRANCH_INSTRUCTIONS  = 4,
    PERF_COUNT_HW_BRANCH_MISSES        = 5,
    PERF_COUNT_HW_BUS_CYCLES           = 6,
    // ...
};
```
Source: [`include/uapi/linux/perf_event.h`](https://github.com/torvalds/linux/blob/master/include/uapi/linux/perf_event.h)

#### Layer 2: Raw PMU Events
- **Vendor-specific event codes** mapped by kernel drivers
- **Thousands of available events** per CPU vendor
- **Non-portable** between different hardware

**Example mapping for `PERF_COUNT_HW_CACHE_MISSES`:**
- Intel x86: Raw PMU code `0x412e`
- AMD Zen1: Raw PMU code `0x0964`  
- AMD K7-16h: Raw PMU code `0x077e`
- ARM: Different encoding entirely

### Architecture Differences

The kernel automatically translates generic events to vendor-specific codes:

**Intel ([`arch/x86/events/intel/core.c`](https://github.com/torvalds/linux/blob/master/arch/x86/events/intel/core.c)):**
```c
static u64 intel_perfmon_event_map[PERF_COUNT_HW_MAX] = {
    [PERF_COUNT_HW_CPU_CYCLES]        = 0x003c,
    [PERF_COUNT_HW_CACHE_MISSES]      = 0x412e,
    // ...
};
```

**AMD ([`arch/x86/events/amd/core.c`](https://github.com/torvalds/linux/blob/master/arch/x86/events/amd/core.c)):**
```c
static const u64 amd_zen1_perfmon_event_map[PERF_COUNT_HW_MAX] = {
    [PERF_COUNT_HW_CPU_CYCLES]        = 0x0076,
    [PERF_COUNT_HW_CACHE_MISSES]      = 0x0964,  // Different from Intel!
    // ...
};
```

## Best Practices for eBPF Profilers

### 1. Use Generic Events First
Always prefer `PERF_TYPE_HARDWARE` with standardized `PERF_COUNT_HW_*` constants:

```go
// Portable across all architectures
attr := unix.PerfEventAttr{
    Type:   unix.PERF_TYPE_HARDWARE,
    Config: unix.PERF_COUNT_HW_CPU_CYCLES,
}
```

### 2. Implement Graceful Degradation
```go
profiler, _ := collectors.NewProfiler(logger, config)

// Try hardware events first
err := profiler.Setup(collectors.ProfilerConfig{
    Event: collectors.PerfEventConfig{
        Name:         "cpu-cycles",
        Type:         unix.PERF_TYPE_HARDWARE,
        Config:       unix.PERF_COUNT_HW_CPU_CYCLES,
        SamplePeriod: 1000000,
    },
})
if err != nil {
    // Fallback to software events
    err = profiler.Setup(collectors.NewProfilerConfig(collectors.CPUClockEvent))
    if err != nil {
        // Disable hardware profiling features
        return disableHardwareProfiling()
    }
}
```

### 3. Validate Events at Runtime
Never assume events are available - always test with `perf_event_open()`:

```go
func isPerfEventAvailable(eventType uint32, config uint64) bool {
    attr := unix.PerfEventAttr{
        Type:   eventType,
        Config: config,
        Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
    }
    
    fd, err := unix.PerfEventOpen(&attr, 0, -1, -1, unix.PERF_FLAG_FD_CLOEXEC)
    if err != nil {
        return false
    }
    
    unix.Close(fd)
    return true
}
```

### 4. Handle Common Error Conditions
- `ENOENT`: Event not supported → Try alternatives
- `EACCES`: Permission denied → Check `perf_event_paranoid`
- `EMFILE`: Too many open files → Resource management

### 5. Provide Helpful Error Messages
Use enumeration to suggest available alternatives:

```go
func (c *ProfilerCollector) validateEventSupport(eventConfig *PerfEventConfig) error {
    if !isPerfEventAvailable(eventConfig.Type, eventConfig.Config) {
        // Get available events for better error messages
        events, _ := collectors.EnumerateAvailablePerfEvents()
        var available []string
        for _, event := range events {
            if event.Available {
                available = append(available, event.Name)
            }
        }
        return fmt.Errorf("perf event %q (type=%d, config=0x%x) not available on this system. Available events: %v", 
            eventConfig.Name, eventConfig.Type, eventConfig.Config, available)
    }
    return nil
}
```

## Troubleshooting

### Event Not Available
If a specific perf event fails:

1. **Check enumeration output** to see what events are actually available
2. **Verify event constants** match kernel definitions
3. **Test on bare metal** - VMs often lack PMU access
4. **Check permissions** - `/proc/sys/kernel/perf_event_paranoid`

### Cross-Architecture Testing
Test profilers on:
- **Intel x86_64** - Most common server architecture
- **AMD x86_64** - Different PMU event mappings than Intel  
- **ARM64** - Growing server adoption, different event model
- **Virtualized environments** - Limited to software events

### Debugging Commands
```bash
# List available events
perf list hw
perf list sw

# Check PMU access
cat /proc/sys/kernel/perf_event_paranoid

# Discover raw events
ls /sys/devices/cpu/events/
ls /sys/bus/event_source/devices/
```

## Bug Discovery Example

During implementation, we discovered a bug in our constant definitions:

**Issue**: `PERF_COUNT_HW_CACHE_MISSES` was incorrectly defined as `6` instead of the kernel standard `3`.

**Detection**: The enumeration system detected that cache-misses was available at config=3, but our profiler expected config=6.

**Root cause**: Confusion between standardized API constants and raw PMU event codes.

**Fix**: Updated constants in [`pkg/performance/collectors/profiler.go`](../pkg/performance/collectors/profiler.go) to match official Linux kernel definitions.

**Lesson**: Event enumeration not only improves user experience but helps detect implementation bugs.

## Related Documentation

- [eBPF Development Guide](ebpf-development.md) - For general eBPF development workflows
- [Performance Collectors Guide](performance-collectors.md) - For implementing performance collectors

## References

### Linux Kernel Sources
- [Linux Kernel `perf_event.h`](https://github.com/torvalds/linux/blob/master/include/uapi/linux/perf_event.h) - Standardized event definitions
- [Intel x86 PMU Implementation](https://github.com/torvalds/linux/blob/master/arch/x86/events/intel/core.c)
- [AMD x86 PMU Implementation](https://github.com/torvalds/linux/blob/master/arch/x86/events/amd/core.c)
- [ARM PMU Implementation](https://github.com/torvalds/linux/blob/master/drivers/perf/arm_pmuv3.c)

### Official Documentation
- [`perf_event_open(2)` man page](https://man7.org/linux/man-pages/man2/perf_event_open.2.html) - System call documentation
- [Linux Perf Wiki](https://perf.wiki.kernel.org/) - Community documentation and examples
- [Linux kernel tools/perf documentation](https://github.com/torvalds/linux/tree/master/tools/perf/Documentation)

### Vendor Resources
- [Intel PMU Event Database](https://perfmon-events.intel.com/) - Comprehensive Intel event listings
- AMD Processor Programming Reference (PPR) - Available per CPU family
- [ARM Performance Monitoring Unit Guide](https://developer.arm.com/documentation/100095/0002/performance-monitoring-unit)