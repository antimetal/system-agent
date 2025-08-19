# Memory Leak Simulator Test Suite

This directory contains test applications designed to trigger specific memory leak detection thresholds in our eBPF monitors.

## Test Applications

### 1. VSZ/RSS Divergence Test (`vsz_divergence.c`)
- Allocates memory but never touches it
- Expected: VSZ grows 2x+ faster than RSS
- Threshold: VSZ/RSS ratio > 2.0

### 2. Monotonic Growth Test (`monotonic_growth.c`)
- Steady memory leak without any decreases
- Expected: Continuous growth for >5 minutes
- Threshold: Monotonic growth > 300 seconds

### 3. Anonymous Memory Ratio Test (`anon_ratio.c`)
- Pure heap allocations (no file mappings)
- Expected: Anonymous memory >80% of RSS
- Threshold: anon_ratio > 800 (80%)

### 4. Combined Leak Test (`combined_leak.c`)
- Triggers all three thresholds simultaneously
- Expected: High confidence (>90)

### 5. False Positive Tests
- `cache_growth.c` - File cache growth (should NOT trigger)
- `startup_spike.c` - Initial memory spike then stable (should NOT trigger)
- `gc_pattern.c` - Sawtooth GC pattern (should NOT trigger)

## Test Execution

### Local Testing
```bash
# Compile all tests
make build-tests

# Run individual test
./bin/vsz_divergence

# Run with parameters
./bin/monotonic_growth --leak-rate=1MB --duration=600
```

### Hetzner Deployment
```bash
# Deploy to Hetzner server
./scripts/deploy-to-hetzner.sh

# Run test suite remotely
ssh hetzner-test ./run-leak-tests.sh

# Monitor eBPF output
ssh hetzner-test ./monitor-ebpf.sh
```

## Expected Results

| Test | VSZ/RSS | Monotonic | Anon% | Confidence | Detection Time |
|------|---------|-----------|-------|------------|----------------|
| vsz_divergence | >2.5 | No | 60% | 60-70 | <1 min |
| monotonic_growth | 1.2 | Yes | 75% | 70-80 | 5-6 min |
| anon_ratio | 1.1 | No | 90% | 65-75 | <1 min |
| combined_leak | >2.0 | Yes | 85% | 95-100 | 5 min |
| cache_growth | 1.5 | No | 40% | <20 | Never |
| startup_spike | 1.3 | No | 60% | <30 | Never |
| gc_pattern | 1.2 | No | 70% | <40 | Never |

## Validation Criteria

### Success Criteria
- Each threshold test triggers its specific detection
- Combined test achieves >90 confidence
- False positive tests stay below 50 confidence
- Detection occurs within expected timeframe

### Debugging
- Use `bpftool prog tracelog` to see eBPF debug output
- Check `/sys/kernel/debug/tracing/trace_pipe` for events
- Monitor confidence scores in real-time