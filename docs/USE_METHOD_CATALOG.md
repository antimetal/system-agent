# Comprehensive USE Method Tools and Techniques Catalog

## Table of Contents
1. [Overview](#overview)
2. [Brendan Gregg's Official USE Method Resources](#brendan-greggs-official-use-method-resources)
3. [Traditional Linux Tools](#traditional-linux-tools)
4. [Modern BPF/eBPF Tools](#modern-bpfebpf-tools)
5. [Performance Monitoring Frameworks](#performance-monitoring-frameworks)
6. [Container and Kubernetes Tools](#container-and-kubernetes-tools)
7. [Dashboard and Visualization Tools](#dashboard-and-visualization-tools)
8. [Automation and Scripts](#automation-and-scripts)
9. [Cloud-Native and Observability Platforms](#cloud-native-and-observability-platforms)

## Overview

The USE Method (Utilization, Saturation, and Errors) is a methodology for analyzing system performance developed by Brendan Gregg. It provides a systematic approach to identify resource bottlenecks by examining:

- **Utilization**: The average time the resource was busy servicing work
- **Saturation**: The degree to which the resource has extra work it can't service
- **Errors**: The count of error events

## Brendan Gregg's Official USE Method Resources

### Primary Resources
- **Linux Performance Checklist**: https://www.brendangregg.com/USEmethod/use-linux.html
- **Main USE Method Page**: https://www.brendangregg.com/usemethod.html
- **USE Method Rosetta Stone**: https://www.brendangregg.com/USEmethod/use-rosetta.html
- **Linux Performance Page**: https://www.brendangregg.com/linuxperf.html

## Traditional Linux Tools

### CPU
**Utilization:**
- `vmstat 1` - si and us columns
- `sar -u` - %user + %system
- `dstat -c` - usr and sys columns
- `mpstat -P ALL 1` - per-CPU utilization
- `top/htop` - CPU usage percentage
- `pidstat 1` - per-process CPU usage

**Saturation:**
- `vmstat 1` - r column (run queue length)
- `sar -q` - runq-sz
- `dstat -p` - run, blk, new columns
- `cat /proc/loadavg` - load averages

**Errors:**
- `perf stat -e cpu-clock` - CPU specific error events
- `dmesg | grep -i "machine check"` - hardware errors

### Memory
**Utilization:**
- `free -m` - used memory percentage
- `vmstat 1` - free, buff, cache columns
- `sar -r` - %memused
- `dstat -m` - used, buff, cach, free
- `slabtop` - kernel slab allocator usage
- `cat /proc/meminfo` - detailed memory statistics

**Saturation:**
- `vmstat 1` - si/so columns (swap in/out)
- `sar -B` - pgscank/s (page scanning)
- `sar -W` - pswpin/s, pswpout/s
- `dmesg | grep "Out of memory"` - OOM killer events

**Errors:**
- `dmesg | grep -E "memory|ECC"` - memory hardware errors
- `edac-util -v` - ECC memory errors

### Network Interfaces
**Utilization:**
- `sar -n DEV 1` - rxkB/s, txkB/s
- `ip -s link` - RX/TX bytes
- `cat /proc/net/dev` - interface statistics
- `nicstat` - network interface statistics
- `ethtool -S <interface>` - detailed NIC statistics

**Saturation:**
- `ifconfig` - overruns, dropped
- `netstat -s` - retransmits
- `sar -n EDEV 1` - rxdrop/s, txdrop/s
- `tc -s qdisc` - queue statistics

**Errors:**
- `ifconfig` - errors, dropped
- `netstat -i` - RX-ERR, TX-ERR
- `ip -s link` - errors, dropped
- `sar -n EDEV 1` - rxerr/s, txerr/s

### Storage Device I/O
**Utilization:**
- `iostat -xz 1` - %util column
- `sar -d` - %util
- `iotop` - disk I/O by process
- `pidstat -d 1` - disk I/O per process

**Saturation:**
- `iostat -xz 1` - avgqu-sz (average queue size)
- `sar -d` - await (average wait time)
- `cat /sys/block/*/queue/nr_requests` - queue depth

**Errors:**
- `/sys/devices/.../ioerr_cnt` - I/O error counts
- `smartctl -a /dev/sdX` - SMART errors
- `dmesg | grep -E "I/O error|hard resetting link"`

## Modern BPF/eBPF Tools

### BCC (BPF Compiler Collection)
Repository: https://github.com/iovisor/bcc

**CPU Tools:**
- `runqlat` - Run queue latency histogram
- `cpudist` - CPU usage distribution
- `cpuunclaimed` - Sample CPU run queues
- `profile` - CPU profiler
- `offcputime` - Off-CPU time analysis

**Memory Tools:**
- `memleak` - Memory leak detector
- `oomkill` - OOM kill events
- `slabratetop` - Kernel slab allocator usage
- `drsnoop` - Direct reclaim events

**Network Tools:**
- `tcplife` - TCP connection lifespan
- `tcpretrans` - TCP retransmission details
- `tcpdrop` - TCP packet drops
- `tcpconnect` - TCP active connections
- `tcpaccept` - TCP passive connections

**Storage Tools:**
- `biolatency` - Block I/O latency histogram
- `biosnoop` - Block I/O events
- `biotop` - Top for block I/O
- `bitesize` - I/O size histogram
- `ext4slower` - Trace slow ext4 operations

### bpftrace
High-level tracing language for eBPF

**Example USE Method Scripts:**
```bash
# CPU saturation - run queue length histogram
bpftrace -e 'profile:hz:99 { @[cpu] = lhist(curtask->se.nr_running, 0, 100, 1); }'

# Memory page faults
bpftrace -e 'software:page-fault:1 { @[comm] = count(); }'

# TCP retransmissions by process
bpftrace -e 'kprobe:tcp_retransmit_skb { @[comm] = count(); }'

# Block I/O latency
bpftrace -e 'kprobe:blk_account_io_start { @start[arg0] = nsecs; }
    kprobe:blk_account_io_done /@start[arg0]/ { 
        @latency = hist((nsecs - @start[arg0]) / 1000); delete(@start[arg0]); }'
```

## Performance Monitoring Frameworks

### Performance Co-Pilot (PCP)
**Installation:** Available in most Linux distributions

**USE Method Tools:**
- `pcp-ss` - Report USE method metrics
- `pmstat` - High-level system performance
- `pmiostat` - I/O statistics
- `pcp-atop` - Advanced system monitor

**Configuration:**
```bash
# Enable PCP
systemctl enable pmcd pmlogger
systemctl start pmcd pmlogger

# USE Method specific metrics
pminfo -t | grep -E "kernel.all.cpu|mem.util|disk.all|network"
```

### Prometheus + Node Exporter
**Node Exporter USE Metrics:**

**CPU:**
- Utilization: `1 - avg(irate(node_cpu_seconds_total{mode="idle"}[5m]))`
- Saturation: `node_load1 > count(node_cpu_seconds_total{mode="idle"})`

**Memory:**
- Utilization: `1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes`
- Saturation: `rate(node_vmstat_pswpin[5m]) + rate(node_vmstat_pswpout[5m])`

**Disk:**
- Utilization: `rate(node_disk_io_time_seconds_total[5m])`
- Saturation: `rate(node_disk_io_time_weighted_seconds_total[5m])`

**Network:**
- Utilization: `rate(node_network_receive_bytes_total[5m])`
- Errors: `rate(node_network_receive_errs_total[5m])`

### collectd
**Plugins for USE Method:**
- `cpu` - CPU utilization and states
- `memory` - Memory utilization
- `interface` - Network interface statistics
- `disk` - Disk I/O statistics
- `load` - System load (saturation indicator)

## Container and Kubernetes Tools

### cAdvisor
**Metrics Exposed:**
- Container CPU usage and throttling
- Memory usage and limits
- Network I/O statistics
- Filesystem usage
- Container restarts (errors)

**Access Methods:**
```bash
# Standalone
docker run -d --name=cadvisor \
  --volume=/var/run:/var/run:ro \
  --volume=/sys:/sys:ro \
  --volume=/var/lib/docker/:/var/lib/docker:ro \
  --publish=8080:8080 \
  gcr.io/cadvisor/cadvisor:latest

# Kubernetes (built into kubelet)
kubectl proxy
curl http://localhost:8001/api/v1/nodes/<node-name>/proxy/metrics/cadvisor
```

### Kubernetes Metrics
**kubectl top:**
```bash
kubectl top nodes
kubectl top pods --all-namespaces
kubectl top pods --containers
```

**Metrics Server:**
```bash
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
```

### Container-specific eBPF Tools
- `kubectl-trace` - Run bpftrace in Kubernetes
- `inspektor-gadget` - Collection of eBPF tools for Kubernetes

## Dashboard and Visualization Tools

### Grafana Dashboards

**USE Method Dashboard IDs:**
- Node Exporter Full: 1860
- Kubernetes Cluster Monitoring: 7249
- cAdvisor Dashboard: 14282

**Creating Custom USE Dashboards:**
```json
{
  "dashboard": {
    "title": "USE Method Dashboard",
    "panels": [
      {
        "title": "CPU Utilization",
        "targets": [{
          "expr": "100 - (avg(irate(node_cpu_seconds_total{mode=\"idle\"}[5m])) * 100)"
        }]
      },
      {
        "title": "CPU Saturation (Load Average)",
        "targets": [{
          "expr": "node_load1"
        }]
      },
      {
        "title": "Memory Utilization",
        "targets": [{
          "expr": "(1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) * 100"
        }]
      }
    ]
  }
}
```

### Observability Platforms

**DataDog:**
- USE Method dashboard templates
- APM integration with infrastructure metrics
- Custom USE Method monitors

**New Relic:**
- Infrastructure monitoring with USE metrics
- Custom dashboards and alerts
- Integration with cloud providers

**Elastic Stack:**
- Metricbeat for system metrics
- Kibana dashboards for USE visualization
- Machine learning for anomaly detection

## Automation and Scripts

### USE Method Check Script
```bash
#!/bin/bash
# use_check.sh - Basic USE Method health check

echo "=== CPU ==="
echo "Utilization:"
mpstat 1 1 | tail -1 | awk '{print 100-$NF"%"}'
echo "Saturation (load average):"
uptime
echo "Errors:"
dmesg | tail -20 | grep -i "cpu\|processor" || echo "No recent CPU errors"

echo -e "\n=== Memory ==="
echo "Utilization:"
free -h | grep Mem | awk '{print $3" / "$2}'
echo "Saturation (swap activity):"
vmstat 1 2 | tail -1 | awk '{print "si: "$7" so: "$8}'
echo "Errors:"
dmesg | tail -20 | grep -i "memory\|oom" || echo "No recent memory errors"

echo -e "\n=== Disk ==="
echo "Utilization:"
iostat -x 1 2 | grep -v "^$" | tail -n +7 | awk '{print $1": "$NF"%"}'
echo "Saturation (queue size):"
iostat -x 1 2 | grep -v "^$" | tail -n +7 | awk '{print $1": "$9}'
echo "Errors:"
dmesg | tail -20 | grep -i "i/o error" || echo "No recent I/O errors"

echo -e "\n=== Network ==="
echo "Utilization:"
ip -s link | grep -A1 "^[0-9]"
echo "Errors:"
netstat -i | column -t
```

### Prometheus Recording Rules for USE
```yaml
groups:
  - name: use_method
    interval: 30s
    rules:
      # CPU
      - record: instance:node_cpu_utilization:rate5m
        expr: 100 - (avg by (instance) (irate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)
      
      - record: instance:node_cpu_saturation:ratio
        expr: node_load1 / count by (instance) (node_cpu_seconds_total{mode="idle"})
      
      # Memory
      - record: instance:node_memory_utilization:ratio
        expr: 1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)
      
      - record: instance:node_memory_saturation:rate5m
        expr: rate(node_vmstat_pswpin[5m]) + rate(node_vmstat_pswpout[5m])
      
      # Disk
      - record: instance:node_disk_utilization:rate5m
        expr: irate(node_disk_io_time_seconds_total[5m])
      
      - record: instance:node_disk_saturation:rate5m
        expr: irate(node_disk_io_time_weighted_seconds_total[5m])
```

### GitHub Actions Workflow for USE Monitoring
```yaml
name: USE Method Health Check
on:
  schedule:
    - cron: '*/15 * * * *'
  workflow_dispatch:

jobs:
  use-check:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v3
      
      - name: Run USE Method Checks
        run: |
          # CPU Check
          CPU_UTIL=$(mpstat 1 1 | tail -1 | awk '{print 100-$NF}')
          echo "CPU Utilization: ${CPU_UTIL}%"
          
          # Memory Check
          MEM_UTIL=$(free | grep Mem | awk '{print ($3/$2) * 100}')
          echo "Memory Utilization: ${MEM_UTIL}%"
          
          # Create metrics file
          echo "cpu_utilization ${CPU_UTIL}" > metrics.txt
          echo "memory_utilization ${MEM_UTIL}" >> metrics.txt
      
      - name: Upload Metrics
        uses: actions/upload-artifact@v3
        with:
          name: use-metrics
          path: metrics.txt
```

## Cloud-Native and Observability Platforms

### AWS CloudWatch
**USE Metrics:**
- EC2: CPUUtilization, NetworkIn/Out, DiskReadBytes/WriteBytes
- EBS: VolumeReadBytes/WriteBytes, VolumeThroughputPercentage
- RDS: CPUUtilization, DatabaseConnections, ReadLatency/WriteLatency

### Google Cloud Monitoring
**USE Metrics:**
- Compute Engine: CPU utilization, memory utilization, disk I/O
- GKE: Container CPU/memory usage and limits
- Cloud SQL: CPU utilization, memory usage, disk utilization

### Azure Monitor
**USE Metrics:**
- Virtual Machines: Percentage CPU, Available Memory, Disk Read/Write
- AKS: Node and pod metrics
- Azure Database: CPU percent, memory percent, IO percent

### OpenTelemetry
**USE Method Implementation:**
```go
// Example Go instrumentation
meter := otel.Meter("use-method")

cpuUtilization, _ := meter.Float64ObservableGauge(
    "system.cpu.utilization",
    metric.WithDescription("CPU utilization percentage"),
)

memoryUtilization, _ := meter.Float64ObservableGauge(
    "system.memory.utilization",
    metric.WithDescription("Memory utilization percentage"),
)

diskIO, _ := meter.Int64Counter(
    "system.disk.io",
    metric.WithDescription("Disk I/O operations"),
)
```

## Best Practices and Tips

1. **Start with USE**: Apply USE Method first for system-level analysis, then use RED Method for service-level monitoring

2. **Automate Collection**: Use tools like Prometheus, PCP, or custom scripts to continuously collect USE metrics

3. **Set Baselines**: Establish normal ranges for utilization and saturation metrics

4. **Context Matters**: Consider workload patterns when interpreting metrics

5. **Combine Methods**: Use USE with other methodologies (RED, Four Golden Signals) for comprehensive monitoring

6. **Tool Selection**:
   - For ad-hoc analysis: Traditional Linux tools + bpftrace
   - For continuous monitoring: Prometheus + Grafana
   - For deep dive: BCC tools + perf
   - For containers: cAdvisor + Kubernetes metrics

7. **Error Tracking**: Don't neglect errors - they often indicate immediate problems

8. **Saturation Indicators**: Pay special attention to saturation as it often precedes performance degradation

This catalog provides a comprehensive overview of tools and techniques for implementing the USE Method across different environments and use cases.