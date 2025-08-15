# Cgroup Version & Container Runtime Compatibility

## Overview

The Antimetal Agent automatically detects and supports both cgroup v1 and v2 hierarchies across multiple container runtimes. This document details the compatibility matrix, detection mechanisms, and implementation specifics for each configuration.

## Supported Configurations

| Container Runtime | Cgroup v1 | Cgroup v2 | Min Version | Notes |
|------------------|-----------|-----------|-------------|--------|
| Docker           | ✅ Full    | ✅ Full    | 19.03+     | Both systemd and cgroupfs drivers supported |
| containerd       | ✅ Full    | ✅ Full    | 1.4.0+     | Default runtime in Kubernetes 1.24+ |
| CRI-O            | ✅ Full    | ✅ Full    | 1.17+      | Systemd driver recommended for stability |
| Podman           | ⚠️ Partial | ✅ Full    | 2.0+       | Rootless mode not fully tested |

### Operating System Support

| Distribution | Default Cgroup | Switch Available | Notes |
|-------------|---------------|------------------|--------|
| Ubuntu 22.04+ | v2 | Yes | v2 by default, v1 available via kernel parameter |
| Ubuntu 20.04 | v1 | Yes | v1 by default, v2 via systemd.unified_cgroup_hierarchy |
| RHEL/CentOS 9+ | v2 | Yes | v2 default, full v1 compatibility |
| RHEL/CentOS 8 | v1 | Yes | v1 default, v2 available |
| RHEL/CentOS 7 | v1 | No | v1 only, v2 not available |
| Debian 11+ | v2 | Yes | v2 default |
| Amazon Linux 2 | v1 | Limited | v1 default, partial v2 support |
| Alpine 3.15+ | v2 | Yes | v2 with OpenRC or systemd |

## Cgroup Version Detection

The agent automatically detects the cgroup version using a hierarchical approach:

### Detection Algorithm

```go
// From container_discovery.go:DetectCgroupVersion()
func (d *ContainerDiscovery) DetectCgroupVersion() (int, error) {
    // Step 1: Check for cgroup v2 unified hierarchy marker
    v2Marker := filepath.Join(d.cgroupPath, "cgroup.controllers")
    if _, err := os.Stat(v2Marker); err == nil {
        return 2, nil  // Definitively v2
    }

    // Step 2: Check for cgroup v1 controller directories
    v1Controllers := []string{"cpu", "memory", "cpuacct", "blkio", "devices"}
    for _, controller := range v1Controllers {
        controllerPath := filepath.Join(d.cgroupPath, controller)
        if _, err := os.Stat(controllerPath); err == nil {
            return 1, nil  // Found v1 controller
        }
    }

    return 0, fmt.Errorf("unable to detect cgroup version")
}
```

### Key Detection Files

| Version | Detection File | Location | Purpose |
|---------|---------------|----------|---------|
| v2 | `cgroup.controllers` | `/sys/fs/cgroup/` | Lists available v2 controllers |
| v1 | Controller directories | `/sys/fs/cgroup/{cpu,memory,...}` | Separate controller hierarchies |

## Filesystem Layouts

### Cgroup v1 Structure

```
/sys/fs/cgroup/
├── cpu/                          # CPU controller
│   ├── docker/
│   │   └── <container_id>/       # Docker with cgroupfs driver
│   │       ├── cpu.stat          # Throttling statistics
│   │       ├── cpu.shares        # CPU shares (relative weight)
│   │       ├── cpu.cfs_quota_us  # CPU quota in microseconds
│   │       └── cpu.cfs_period_us # CPU period in microseconds
│   ├── system.slice/
│   │   └── docker-<container_id>.scope/  # Docker with systemd driver
│   └── kubepods/                 # Kubernetes pods (older versions)
│       └── pod<pod_uid>/
│           └── <container_id>/
├── cpuacct/                      # CPU accounting (separate in v1)
│   └── docker/
│       └── <container_id>/
│           └── cpuacct.usage     # CPU usage in nanoseconds
├── memory/                       # Memory controller
│   └── docker/
│       └── <container_id>/
│           ├── memory.stat       # Detailed memory statistics
│           ├── memory.usage_in_bytes
│           ├── memory.limit_in_bytes
│           └── memory.failcnt    # Number of limit hits
└── blkio/                        # Block I/O controller
    └── docker/
        └── <container_id>/
```

### Cgroup v2 Structure

```
/sys/fs/cgroup/
├── cgroup.controllers            # Available controllers (cpu io memory pids)
├── cgroup.subtree_control        # Enabled controllers for children
├── system.slice/                 # Systemd-managed containers
│   └── docker-<container_id>.scope/
│       ├── cgroup.controllers    # Inherited controllers
│       ├── cpu.stat              # CPU usage and throttling
│       ├── cpu.max               # CPU quota and period
│       ├── cpu.weight            # CPU weight (1-10000)
│       ├── memory.current        # Current memory usage
│       ├── memory.max            # Memory limit
│       ├── memory.stat           # Detailed memory statistics
│       └── memory.events         # OOM and threshold events
├── kubepods.slice/               # Kubernetes pods
│   ├── kubepods-burstable.slice/
│   │   └── kubepods-pod<pod_uid>.slice/
│   │       └── <runtime>-<container_id>.scope/
│   └── kubepods-besteffort.slice/
└── user.slice/                   # User session containers (Podman)
    └── user-1000.slice/
        └── podman-<container_id>.scope/
```

## Container ID Extraction Patterns

### Docker

| Driver | Cgroup Path Pattern | ID Extraction | Example |
|--------|-------------------|---------------|---------|
| cgroupfs | `/docker/<container_id>` | Direct from path | `/docker/abc123def456789` |
| systemd | `/system.slice/docker-<container_id>.scope` | Between "docker-" and ".scope" | `/system.slice/docker-abc123def456.scope` |

### Containerd

| Context | Cgroup Path Pattern | ID Format | Example |
|---------|-------------------|-----------|---------|
| Standalone | `/containerd/<container_id>` | 64-char hex | `/containerd/0123456789abcdef...` |
| Kubernetes | `/cri-containerd-<container_id>.scope` | 64-char hex | `/cri-containerd-0123456789abcdef.scope` |

### CRI-O

| Driver | Cgroup Path Pattern | ID Extraction | Example |
|--------|-------------------|---------------|---------|
| systemd | `/crio-<container_id>.scope` | Between "crio-" and ".scope" | `/crio-abc123def456.scope` |
| cgroupfs | `/crio/<container_id>` | Direct from path | `/crio/abc123def456` |

### Podman

| Mode | Cgroup Path Pattern | Notes |
|------|-------------------|--------|
| Root | `/machine.slice/libpod-<container_id>.scope` | System containers |
| Rootless | `/user.slice/user-<uid>.slice/podman-<container_id>.scope` | User containers |

## Metrics Mapping

### CPU Metrics

| Metric | Cgroup v1 Path | Cgroup v2 Path | Unit Conversion |
|--------|----------------|----------------|-----------------|
| **Usage** | `cpuacct/cpuacct.usage` | `cpu.stat` → `usage_usec` | v2: µs→ns (*1000) |
| **Throttling** | `cpu/cpu.stat` → `nr_throttled` | `cpu.stat` → `nr_throttled` | Same format |
| **Throttle Time** | `cpu/cpu.stat` → `throttled_time` | `cpu.stat` → `throttled_usec` | v2: µs→ns (*1000) |
| **Shares/Weight** | `cpu/cpu.shares` (2-262144) | `cpu.weight` (1-10000) | Convert: shares=(weight*1024)/100 |
| **Quota** | `cpu/cpu.cfs_quota_us` | `cpu.max` (first field) | Same unit (µs) |
| **Period** | `cpu/cpu.cfs_period_us` | `cpu.max` (second field) | Same unit (µs) |

### Memory Metrics

| Metric | Cgroup v1 Path | Cgroup v2 Path | Notes |
|--------|----------------|----------------|--------|
| **Current Usage** | `memory/memory.usage_in_bytes` | `memory.current` | Same unit (bytes) |
| **Limit** | `memory/memory.limit_in_bytes` | `memory.max` | v2: "max" = unlimited |
| **Peak Usage** | `memory/memory.max_usage_in_bytes` | Not available | v1 only |
| **RSS** | `memory/memory.stat` → `rss` | `memory.stat` → `anon` | Different names |
| **Cache** | `memory/memory.stat` → `cache` | `memory.stat` → `file` | Different names |
| **Fail Count** | `memory/memory.failcnt` | `memory.events` → `max` | Different location |
| **OOM Kill** | `memory/memory.oom_control` | `memory.events` → `oom_kill` | Different format |

## Implementation Details

### Graceful Degradation Strategy

The collectors implement a three-tier degradation strategy:

1. **Full Data**: All cgroup files available and readable
2. **Partial Data**: Some files missing (e.g., no PSI support)
3. **Minimal Data**: Only basic metrics available

```go
// From cgroup_cpu.go:readCgroupV1Stats()
// All files treated as optional - we read what's available
if data, err := os.ReadFile(cpuStatPath); err == nil {
    c.parseCPUStat(string(data), stats)
}
// Missing files silently ignored for graceful degradation
```

### Error Handling Philosophy

| Scenario | v1 Behavior | v2 Behavior | Rationale |
|----------|------------|-------------|-----------|
| Missing optional file | Continue silently | Continue silently | Support partial mounts |
| Missing critical file | Log and skip container | Return error | v2 has fewer files, all important |
| Permission denied | Log warning, skip | Log warning, skip | Non-fatal in monitoring |
| Malformed data | Use zero value | Use zero value | Better than failing entirely |

### Container Discovery Optimization

For large-scale deployments (100+ containers), the discovery process implements several optimizations:

1. **Parallel scanning**: Multiple runtime paths checked concurrently
2. **Early termination**: Stop scanning once container ID validated
3. **Caching**: Discovery results cached for collection interval
4. **Selective scanning**: Only scan active runtime directories

## Known Limitations and Workarounds

### Limitation 1: Podman Rootless Containers
**Issue**: Rootless Podman uses user-specific cgroup paths not covered by standard scanning.
**Workaround**: Manually configure `HostCgroupPath` to include user slice:
```yaml
HostCgroupPath: /sys/fs/cgroup/user.slice/user-1000.slice
```

### Limitation 2: Nested Containers
**Issue**: Containers running inside containers have nested cgroup paths.
**Status**: Not supported. Metrics collected only for top-level containers.

### Limitation 3: Custom Cgroup Drivers
**Issue**: Non-standard cgroup drivers may use unexpected paths.
**Workaround**: Extend `ContainerDiscovery.scanCgroupV*Directory()` methods.

### Limitation 4: Docker Desktop on macOS/Windows
**Issue**: Uses virtualization layer with modified cgroup paths.
**Status**: Not tested. May require custom discovery logic.

## Testing and Validation

### Verify Cgroup Version

```bash
# Check cgroup version on host
if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
    echo "Cgroup v2 detected"
    cat /sys/fs/cgroup/cgroup.controllers
else
    echo "Cgroup v1 detected"
    ls /sys/fs/cgroup/
fi
```

### Verify Container Detection

```bash
# Inside agent pod
kubectl exec -n antimetal-system deployment/agent -- sh -c '
    find /host/cgroup -type d -name "*docker*" -o -name "*containerd*" | head -10
'
```

### Test Metrics Collection

```bash
# Enable verbose logging
kubectl set env deployment/agent -n antimetal-system LOG_LEVEL=debug

# Check for successful collection
kubectl logs -n antimetal-system deployment/agent | grep -E "Detected cgroup version|Discovered containers"
```

## Performance Considerations

| Deployment Size | Scan Time | Memory Usage | Recommendations |
|----------------|-----------|--------------|-----------------|
| <10 containers | <10ms | <1MB | Default settings |
| 10-100 containers | 10-100ms | 1-5MB | Default settings |
| 100-500 containers | 100-500ms | 5-20MB | Increase collection interval to 30s |
| 500+ containers | >500ms | >20MB | Consider sampling or sharding |

## Migration Guide: v1 to v2

For systems transitioning from cgroup v1 to v2:

1. **No code changes required** - Agent auto-detects version
2. **Metrics remain consistent** - Same data structure returned
3. **Performance improves** - v2 has unified hierarchy, fewer file operations
4. **Some metrics unavailable** - v2 lacks `memory.max_usage_in_bytes`

## References

- [Kernel Cgroup v2 Documentation](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html)
- [Docker Cgroup Driver Documentation](https://docs.docker.com/config/containers/runmetrics/)
- [Kubernetes Cgroup Management](https://kubernetes.io/docs/concepts/architecture/cgroups/)
- [Systemd Cgroup Delegation](https://systemd.io/CGROUP_DELEGATION/)