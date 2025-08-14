# Memory Growth Monitor Testing Methodology

## Overview

This document outlines the testing methodology for validating the RSS-stat eBPF-based memory growth monitor's functionality, performance, and accuracy using the kmem:rss_stat tracepoint. It includes test scenarios, metrics collection, and validation criteria.

## Test Environment Setup

### Hardware Requirements

- **Linux System**: Kernel 5.8+ with BTF support
- **Memory**: Minimum 8GB RAM for meaningful test scenarios
- **CPU**: Multi-core processor for concurrent process testing

### Software Requirements

```bash
# Verify eBPF support
ls /sys/kernel/btf/vmlinux

# Check BPF permissions
cat /proc/sys/kernel/unprivileged_bpf_disabled

# Install required tools
sudo apt-get install linux-tools-generic bpftool
```

## Test Scenarios

### RSS-stat Tracepoint Validation

Verify that the kmem:rss_stat tracepoint is available and functioning:

```bash
# Check tracepoint availability
sudo ls /sys/kernel/debug/tracing/events/kmem/rss_stat/

# Verify tracepoint is firing
sudo perf stat -e kmem:rss_stat -a sleep 1

# Monitor RSS changes in real-time
sudo perf trace -e kmem:rss_stat
```

### 1. Synthetic Memory Leak Generator

Create controlled memory growth patterns for validation:

```go
// test/memory_grower.go
package main

import (
    "fmt"
    "time"
)

func main() {
    var data [][]byte
    growthRateMB := 1  // 1MB per second
    
    for i := 0; i < 60; i++ {
        // Allocate memory
        mb := make([]byte, growthRateMB * 1024 * 1024)
        
        // Touch memory to ensure allocation
        for j := range mb {
            mb[j] = byte(i % 256)
        }
        
        data = append(data, mb)
        fmt.Printf("Allocated %d MB\n", (i+1) * growthRateMB)
        time.Sleep(1 * time.Second)
    }
}
```

### 2. Pattern Variations

Test different memory growth patterns:

#### Linear Growth
```go
// Steady 1MB/second growth
for i := 0; i < duration; i++ {
    allocate(1 * MB)
    sleep(1 * second)
}
```

#### Accelerating Growth
```go
// Exponentially increasing allocation
for i := 0; i < duration; i++ {
    allocate(i * MB)
    sleep(1 * second)
}
```

#### Oscillating Pattern
```go
// Alternating growth and stability
for i := 0; i < duration; i++ {
    if i % 10 < 5 {
        allocate(1 * MB)
    }
    sleep(1 * second)
}
```

#### Burst Allocation
```go
// Sudden large allocations
for i := 0; i < duration; i++ {
    if i % 20 == 0 {
        allocate(50 * MB)
    }
    sleep(1 * second)
}
```

### 3. Multi-Process Scenarios

Test concurrent process monitoring:

```bash
#!/bin/bash
# Launch multiple memory growers simultaneously

for i in {1..5}; do
    ./memory_grower --rate=$i --duration=60 &
done

# Monitor with eBPF collector
sudo ./test_memgrowth_collector --duration=70
```

### 4. Container Testing

Validate within containerized environments:

```dockerfile
FROM ubuntu:22.04
COPY memory_grower /usr/local/bin/
RUN apt-get update && apt-get install -y stress-ng

# Test with memory limits
CMD ["sh", "-c", "memory_grower & stress-ng --vm 2 --vm-bytes 128M --timeout 60s"]
```

```bash
# Run with memory limit
docker run --memory=512m --memory-swap=512m test-memory-growth
```

## Metrics Collection

### Primary Metrics

1. **Detection Latency**
   - Time from leak start to first detection
   - Measured from process start_time to first EVENT_GROWTH_DETECTED

2. **False Positive Rate**
   - Legitimate growth patterns incorrectly flagged
   - Measured against known-good workloads

3. **True Positive Rate**
   - Actual leaks correctly identified
   - Validated against synthetic leaks

4. **Confidence Score Accuracy**
   - Correlation between confidence and actual leak severity
   - Measured across different pattern types

### Performance Metrics

```bash
# CPU overhead measurement
perf stat -e cycles,instructions -p $(pidof collector) -- sleep 60

# Memory usage
cat /proc/$(pidof collector)/status | grep -E "VmRSS|VmSize"

# eBPF program statistics
bpftool prog show
bpftool map show
```

### Overhead Analysis Script

```bash
#!/bin/bash
# test/measure_overhead.sh

echo "=== eBPF Memory Growth Monitor Overhead Analysis ==="
echo "Date: $(date)"
echo "Kernel: $(uname -r)"
echo ""

# Start baseline measurement (no collector)
echo "1. Baseline CPU usage (60 seconds)..."
sar -u 1 60 > baseline_cpu.txt &
SAR_PID=$!

# Wait for baseline
wait $SAR_PID

# Start collector
echo "2. Starting eBPF collector..."
sudo ./memgrowth_collector &
COLLECTOR_PID=$!
sleep 5  # Let it initialize

# Measure with collector running
echo "3. CPU usage with collector (60 seconds)..."
sar -u 1 60 > collector_cpu.txt &
SAR_PID=$!

# Track memory usage
for i in {1..60}; do
    ps -p $COLLECTOR_PID -o rss= >> collector_memory.txt
    sleep 1
done

wait $SAR_PID

# Generate load (launch memory growers)
echo "4. CPU usage under load (60 seconds)..."
for i in {1..10}; do
    ./memory_grower --rate=1 &
done

sar -u 1 60 > load_cpu.txt &
SAR_PID=$!
wait $SAR_PID

# Stop everything
kill $COLLECTOR_PID
killall memory_grower

# Analysis
echo ""
echo "=== Results ==="
echo "Baseline CPU: $(awk '{sum+=$3} END {print sum/NR}' baseline_cpu.txt)%"
echo "With Collector: $(awk '{sum+=$3} END {print sum/NR}' collector_cpu.txt)%"
echo "Under Load: $(awk '{sum+=$3} END {print sum/NR}' load_cpu.txt)%"
echo "Memory Usage: $(awk '{sum+=$1} END {print sum/NR/1024 "MB"}' collector_memory.txt)"
echo ""
echo "BPF Program Stats:"
sudo bpftool prog show | grep memgrowth
sudo bpftool map show | grep -A2 process_states
```

### Data Collection Script

```bash
#!/bin/bash
# test/collect_metrics.sh

OUTPUT_DIR="test_results_$(date +%Y%m%d_%H%M%S)"
mkdir -p $OUTPUT_DIR

# Start collector with debug output
sudo ./memgrowth_collector --debug > $OUTPUT_DIR/collector.log 2>&1 &
COLLECTOR_PID=$!

# Launch test scenarios
./run_test_scenarios.sh > $OUTPUT_DIR/scenarios.log 2>&1 &
SCENARIO_PID=$!

# Collect system metrics every second
while kill -0 $SCENARIO_PID 2>/dev/null; do
    echo "$(date +%s)" >> $OUTPUT_DIR/metrics.log
    
    # CPU usage
    ps -p $COLLECTOR_PID -o %cpu= >> $OUTPUT_DIR/cpu.log
    
    # Memory usage
    ps -p $COLLECTOR_PID -o rss= >> $OUTPUT_DIR/memory.log
    
    # BPF stats
    bpftool map dump name process_states >> $OUTPUT_DIR/bpf_states.log
    
    sleep 1
done

# Generate report
./generate_report.py $OUTPUT_DIR > $OUTPUT_DIR/report.md
```

## Validation Criteria

### Functional Requirements

| Requirement | Test Method | Pass Criteria |
|-------------|------------|---------------|
| Process Detection | Launch 10 processes | All processes tracked within 1s |
| Growth Rate Calculation | Linear growth test | Rate accurate within ±5% |
| Pattern Classification | Pattern variation tests | Correct classification >90% |
| Memory Limit Tracking | Container tests | Accurate limit detection |
| Process Exit Cleanup | Kill test processes | State removed within 1s |

### Performance Requirements

| Metric | Target | Test Method |
|--------|--------|-------------|
| CPU Overhead | To be measured | 60-second measurement under load |
| Memory Usage | To be measured | RSS measurement |
| Detection Latency | <10 seconds | Time to first event with 5s coalescing |
| Event Rate | To be measured | Ring buffer throughput |
| Map Operations | To be measured | BPF map update rate |

### Accuracy Targets

| Metric | Target | Current Status |
|--------|--------|----------------|
| True Positive Rate | >90% | **TO BE TESTED** |
| False Positive Rate | <5% | **TO BE TESTED** |
| Confidence Correlation | >0.8 | **TO BE TESTED** |
| Growth Rate Accuracy | ±10% | **TO BE TESTED** |

## Test Execution Plan

### Phase 1: Unit Testing (Completed)
- [x] eBPF program compilation
- [x] Go collector initialization
- [x] Ring buffer communication
- [x] Basic process tracking

### Phase 2: Integration Testing (In Progress)
- [x] Lima VM testing with simple processes
- [ ] Memory growth pattern detection
- [ ] Multi-process scenarios
- [ ] Container environment testing

### Phase 3: Performance Testing (Planned)
- [ ] CPU overhead measurement
- [ ] Memory usage profiling
- [ ] Scalability testing (100+ processes)
- [ ] Long-duration stability test (24 hours)

### Phase 4: Accuracy Validation (Planned)
- [ ] Synthetic leak detection rates
- [ ] Pattern classification accuracy
- [ ] Confidence score calibration
- [ ] False positive analysis

## Known Issues and Limitations

### Current Implementation Notes

1. **RSS Tracking**: Using kmem:rss_stat tracepoint for exact RSS values
   - Provides accurate resident memory tracking
   - Tracks per-memory-type changes (anon/file/swap/shmem)

2. **Process Filtering**: No container/cgroup awareness yet
   - Impact: Cannot correlate with container limits
   - Fix: Add cgroup path resolution

3. **Event Coalescing**: 5-second coalescing threshold
   - Reduces noise from rapid RSS fluctuations
   - Configurable intervals and threshold-based events planned

### Test Environment Limitations

1. **Kernel Variations**: Different kernel versions have different mm_struct layouts
2. **Virtualization**: Some VMs don't expose hardware PMU counters
3. **Permissions**: Requires CAP_BPF or CAP_SYS_ADMIN

## Reporting Template

### Test Report Format

```markdown
# Memory Growth Monitor Test Report

**Date**: [Date]
**Environment**: [Kernel version, architecture]
**Test Duration**: [Duration]

## Executive Summary
[Brief overview of test results]

## Test Scenarios Executed
- [ ] Linear growth pattern
- [ ] Accelerating pattern
- [ ] Oscillating pattern
- [ ] Multi-process test
- [ ] Container test

## Results

### Detection Accuracy
- Processes Monitored: X
- Leaks Detected: Y
- False Positives: Z
- Detection Latency: Avg Xs (Min: Xs, Max: Xs)

### Performance Impact
- CPU Usage: X%
- Memory Usage: X MB
- Events Generated: X/sec

### Issues Found
[List any bugs or unexpected behavior]

## Recommendations
[Suggestions for improvements]
```

## Continuous Testing

### Automated Test Pipeline

```yaml
# .github/workflows/ebpf-tests.yml
name: eBPF Memory Monitor Tests

on:
  push:
    paths:
      - 'ebpf/src/memgrowth.bpf.c'
      - 'pkg/performance/collectors/memgrowth_*.go'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      
      - name: Setup eBPF environment
        run: |
          sudo apt-get update
          sudo apt-get install -y linux-tools-generic
          
      - name: Build eBPF program
        run: make build-ebpf
        
      - name: Run unit tests
        run: go test ./pkg/performance/collectors
        
      - name: Run integration tests
        run: sudo ./test/run_integration_tests.sh
        
      - name: Generate report
        run: ./test/generate_report.sh > test_report.md
        
      - name: Upload results
        uses: actions/upload-artifact@v2
        with:
          name: test-results
          path: test_report.md
```

## Next Steps

1. **Immediate**: Fix RSS reading to get accurate memory values
2. **Short-term**: Implement full test suite with synthetic workloads
3. **Medium-term**: Add container awareness and cgroup integration
4. **Long-term**: Establish baseline metrics from production workloads

---

*This testing methodology will evolve as the implementation matures. All test results should be documented and tracked for regression prevention.*