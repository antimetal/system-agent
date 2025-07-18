# Hardware Collector Benchmark Tool

A comprehensive testing and benchmarking tool for all point collectors in the Antimetal agent performance monitoring system.

## Features

- **Functional Testing**: Validates all hardware collectors work correctly
- **Performance Benchmarking**: Measures execution time and performance characteristics
- **Data Inspection**: Shows collected data with detailed formatting
- **Error Reporting**: Comprehensive error handling and reporting
- **Filtering**: Test specific collector types only

## Usage

### Basic Testing

```bash
# Run all collectors once
go run tools/collector-bench/main.go

# Show collected data
go run tools/collector-bench/main.go -show-data

# Enable verbose output
go run tools/collector-bench/main.go -verbose
```

### Performance Benchmarking

```bash
# Run performance benchmarks (10 iterations by default)
go run tools/collector-bench/main.go -bench

# Custom number of iterations
go run tools/collector-bench/main.go -bench -iterations=50

# Benchmark with verbose iteration details
go run tools/collector-bench/main.go -bench -verbose
```

### Filtering

```bash
# Test only CPU and memory collectors
go run tools/collector-bench/main.go -filter=cpu_info,memory_info

# Test only disk collector with benchmarks
go run tools/collector-bench/main.go -filter=disk_info -bench -show-data
```

### Advanced Options

```bash
# Set custom timeout for slow systems
go run tools/collector-bench/main.go -timeout=60s

# Full comprehensive test with all options
go run tools/collector-bench/main.go -bench -show-data -verbose -iterations=20
```

## Command Line Options

| Flag | Default | Description |
|------|---------|-------------|
| `-verbose` | false | Enable verbose output with detailed information |
| `-bench` | false | Run performance benchmarks |
| `-iterations` | 10 | Number of benchmark iterations |
| `-show-data` | false | Show collected data (can be large) |
| `-filter` | "" | Comma-separated list of collector types to run |
| `-timeout` | 30s | Timeout for individual collectors |

## Available Collector Types

- `cpu_info` - CPU hardware information
- `memory_info` - Memory configuration and NUMA topology
- `disk_info` - Disk hardware and partition information
- `network_info` - Network interface configuration

## Output Format

### Basic Test Results
```
📊 Collector Test Results
=========================
✅ PASS CPU Hardware Info Collector (cpu_info)
   Duration: 2.5ms
   Data Size: ~450 bytes

✅ PASS Memory Hardware Info Collector (memory_info)
   Duration: 1.8ms
   Data Size: ~120 bytes
```

### Benchmark Results
```
🏃 Benchmarking CPU Hardware Info Collector
   Running 10 iterations...
   Results:
     Success Rate: 10/10 (100.0%)
     Average: 2.3ms
     Median:  2.1ms
     Min:     1.9ms
     Max:     3.2ms
```

### Data Inspection
```
   Data:
     Model: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
     Vendor: GenuineIntel
     Physical Cores: 6
     Logical Cores: 12
     CPU MHz: 2600.00
     Family: 6, Model: 158, Stepping: 10
     Features: 48 flags
```

## Linux Systems Only

This tool is designed exclusively for Linux systems and requires access to:
- `/proc` filesystem for CPU and memory information
- `/sys` filesystem for hardware device information

### Testing on Lima VMs

For development on non-Linux systems, use a Lima VM:

```bash
# Build for Linux from project root
GOOS=linux GOARCH=arm64 go build -o collector-bench tools/collector-bench/main.go

# Copy to Lima VM (replace 'your-lima-instance' with your instance name)
limactl copy collector-bench your-lima-instance:/tmp/collector-bench

# Run in Lima VM
limactl shell your-lima-instance
/tmp/collector-bench -bench -show-data
```

## Troubleshooting

### Common Issues

1. **Permission Denied**: Some collectors may need read access to `/proc` and `/sys`
2. **Timeout Errors**: Increase timeout with `-timeout=60s` on slow systems
3. **Missing Data**: Virtual machines may not expose all hardware information

### Debug Mode

Use verbose mode to see detailed execution:
```bash
go run tools/collector-bench/main.go -verbose -show-data
```

## Integration with CI/CD

The tool exits with code 1 if any collector fails, making it suitable for CI/CD pipelines:

```bash
# In CI pipeline
go run tools/collector-bench/main.go -bench -iterations=5
```

## Development

To add support for new collectors:
1. Add the collector to the `collectors` slice in `main()`
2. Add data size estimation in `estimateDataSize()`
3. Add data printing logic in `printCollectorData()`
4. Update the filter types documentation