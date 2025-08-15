# Hardware Graph Documentation

## Overview

The Hardware Graph feature adds hardware configuration discovery and graph representation to the Antimetal Agent, enabling physical and virtual hardware resources to be represented as nodes and relationships in "The Graph" alongside Kubernetes and cloud resources.

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────────┐
│                     Performance Collectors                  │
│  (CPUInfo, MemoryInfo, DiskInfo, NetworkInfo)               │
└──────────────────────┬──────────────────────────────────────┘
                       │ Collect hardware data
                       ▼
┌─────────────────────────────────────────────────────────────┐
│                    Hardware Manager                         │
│  - Periodic collection orchestration                        │
│  - Snapshot aggregation                                     │
└──────────────────────┬──────────────────────────────────────┘
                       │ Hardware snapshot
                       ▼
┌─────────────────────────────────────────────────────────────┐
│                Hardware Graph Builder                       │
│  - Converts collector data to graph nodes                   │
│  - Creates RDF triplet relationships                        │
└──────────────────────┬──────────────────────────────────────┘
                       │ Resources & Relationships
                       ▼
┌─────────────────────────────────────────────────────────────┐
│                    Resource Store                           │
│   (BadgerDB - stores nodes and relationships)               │
└─────────────────────────────────────────────────────────────┘
```

### Data Flow

1. **Performance collectors** read from `/proc` and `/sys` filesystems
2. **Hardware Manager** orchestrates periodic collection (default: 5 minutes)
3. **Graph Builder** transforms raw data into graph nodes and relationships
4. **Resource Store** persists the hardware graph using RDF triplets

## Hardware Ontology

### Node Types

#### SystemNode
Root node representing the physical or virtual machine.

**Properties:**
- `hostname`: System hostname
- `architecture`: CPU architecture (x86_64, arm64)
- `boot_time`: System boot timestamp
- `kernel_version`: Kernel version string
- `os_info`: Operating system information

#### CPUPackageNode
Represents a physical CPU socket/package.

**Properties:**
- `socket_id`: Physical package ID
- `vendor_id`: CPU vendor (GenuineIntel, AuthenticAMD)
- `model_name`: Full CPU model name
- `cpu_family`: CPU family number
- `model`: Model number
- `stepping`: Stepping revision
- `microcode`: Microcode version
- `cache_size`: Cache size string
- `physical_cores`: Number of physical cores
- `logical_cores`: Number of logical cores (with hyperthreading)

#### CPUCoreNode
Individual CPU core within a package.

**Properties:**
- `processor_id`: Logical CPU number
- `core_id`: Physical core ID
- `physical_id`: Parent package ID
- `frequency_mhz`: Current frequency
- `siblings`: Number of sibling threads

#### MemoryModuleNode
System memory configuration.

**Properties:**
- `total_bytes`: Total system memory
- `numa_enabled`: NUMA support status
- `numa_balancing_available`: NUMA balancing availability
- `numa_node_count`: Number of NUMA nodes

#### NUMANode
NUMA memory node for systems with non-uniform memory access.

**Properties:**
- `node_id`: NUMA node identifier
- `total_bytes`: Memory in this NUMA node
- `cpus`: CPU cores assigned to this node
- `distances`: Distance metrics to other nodes

#### DiskDeviceNode
Physical storage device.

**Properties:**
- `device`: Device name (sda, nvme0n1)
- `model`: Model identifier
- `vendor`: Manufacturer
- `size_bytes`: Total capacity
- `rotational`: HDD (true) or SSD (false)
- `block_size`: Logical block size
- `physical_block_size`: Physical block size
- `scheduler`: I/O scheduler
- `queue_depth`: Queue depth

#### DiskPartitionNode
Disk partition on a storage device.

**Properties:**
- `name`: Partition name (sda1, nvme0n1p1)
- `parent_device`: Parent disk device
- `size_bytes`: Partition size
- `start_sector`: Starting sector

#### NetworkInterfaceNode
Network adapter/interface.

**Properties:**
- `interface`: Interface name (eth0, wlan0)
- `mac_address`: Hardware MAC address
- `speed`: Link speed in Mbps
- `duplex`: Duplex mode (full/half)
- `mtu`: Maximum transmission unit
- `driver`: Driver name
- `type`: Interface type (ethernet, wireless, loopback)
- `oper_state`: Operational state
- `carrier`: Carrier detection status

### Relationship Types

#### ContainsPredicate
Hierarchical containment relationship.

**Properties:**
- `type`: Containment type (physical, logical, partition)

**Usage:**
- System → CPU Package (physical)
- CPU Package → CPU Core (physical)
- System → Memory Module (physical)
- System → Disk Device (physical)
- Disk Device → Partition (partition)
- System → Network Interface (physical)

#### NUMAAffinityPredicate
NUMA node affinity relationships.

**Properties:**
- `node_id`: NUMA node identifier
- `distance`: Distance metric (optional)

**Usage:**
- Memory Module → NUMA Node
- CPU Core → NUMA Node

#### SocketSharingPredicate
CPU cores sharing a physical socket.

**Properties:**
- `physical_id`: Physical package ID
- `socket_id`: Socket identifier

**Usage:**
- CPU Core ↔ CPU Core (same socket)

#### BusConnectionPredicate
Hardware bus connections (future use).

**Properties:**
- `bus_type`: Bus type (pci, usb, sata, nvme)
- `bus_address`: Bus address (optional)

## Example Graph Structure

```
SystemNode (node-01.example.com)
├── [Contains:physical] → CPUPackageNode (socket-0)
│   ├── [Contains:physical] → CPUCoreNode (core-0)
│   ├── [Contains:physical] → CPUCoreNode (core-1)
│   ├── [Contains:physical] → CPUCoreNode (core-2)
│   └── [Contains:physical] → CPUCoreNode (core-3)
├── [Contains:physical] → CPUPackageNode (socket-1)
│   ├── [Contains:physical] → CPUCoreNode (core-4)
│   ├── [Contains:physical] → CPUCoreNode (core-5)
│   ├── [Contains:physical] → CPUCoreNode (core-6)
│   └── [Contains:physical] → CPUCoreNode (core-7)
├── [Contains:physical] → MemoryModuleNode (64GB)
│   ├── [NUMAAffinity:node-0] → NUMANode (node-0, 32GB)
│   └── [NUMAAffinity:node-1] → NUMANode (node-1, 32GB)
├── [Contains:logical] → NUMANode (node-0)
├── [Contains:logical] → NUMANode (node-1)
├── [Contains:physical] → DiskDeviceNode (nvme0n1, 1TB)
│   ├── [Contains:partition] → DiskPartitionNode (nvme0n1p1, 100GB)
│   └── [Contains:partition] → DiskPartitionNode (nvme0n1p2, 900GB)
├── [Contains:physical] → DiskDeviceNode (sda, 4TB)
│   └── [Contains:partition] → DiskPartitionNode (sda1, 4TB)
├── [Contains:physical] → NetworkInterfaceNode (eth0, 10Gbps)
└── [Contains:physical] → NetworkInterfaceNode (eth1, 10Gbps)
```

## Performance Considerations

### Collection Overhead
- Hardware discovery reads from `/proc` and `/sys` filesystems
- Typical collection time: <100ms on modern systems
- Update interval configurable (default: 5 minutes)

### Storage Impact
- Each hardware node: ~200-500 bytes
- Typical system: 50-200 nodes total
- Total storage: <100KB per system

## Scalable Storage Architecture for Million-Host Deployments

### The Deduplication Opportunity

At scale, hardware configurations are highly repetitive. Analysis of large fleets shows:
- ~100 unique CPU models across millions of servers
- ~20 common memory configurations (16GB, 32GB, 64GB, 128GB, etc.)
- ~50 unique disk models
- **Result: ~1,000 unique hardware profiles serve 99% of hosts**

### Proposed Approach

Instead of storing complete hardware graphs for each host, we use a **profile catalog pattern**:

1. **Hardware profiles are deduplicated** - Each unique hardware configuration is stored once
2. **Hosts reference profiles** - Each host points to its hardware profile ID
3. **Profile hashing** - Agents compute a hash of their hardware locally for fast deduplication
4. **Differential storage** - Only host-specific data (hostname, serial numbers) stored per-host

### Storage Impact

For 1 million hosts:
- **Without deduplication**: 100KB × 1M = 100GB
- **With profile catalog**: 
  - Unique profiles: 1,000 × 100KB = 100MB
  - Host mappings: 1M × 100 bytes = 100MB
  - **Total: 200MB (500x reduction)**

The approach scales with the number of unique hardware configurations, not the number of hosts, making it ideal for large standardized fleets.

### Memory Usage
- Snapshot data held temporarily during collection
- Graph builder processes incrementally
- No persistent memory cache required

## Future Enhancements

### Cross-Linking with Kubernetes
Link hardware nodes to Kubernetes nodes:
```
K8s Node → [RunsOn] → SystemNode
K8s Pod → [ScheduledOn] → CPUCoreNode
```

### Extended Hardware Support
- GPU devices and topology
- InfiniBand/RDMA adapters
- Hardware accelerators (TPU, FPGA)
- Power management states
- Thermal sensors

### Performance Metrics Integration
- Attach real-time metrics to hardware nodes
- CPU utilization per core
- Memory bandwidth per NUMA node
- Disk I/O per device
- Network throughput per interface

### Advanced Relationships
- PCIe bus topology
- Memory channel configuration
- CPU cache hierarchy
- Interrupt affinity

## References

- [Linux /proc filesystem](https://www.kernel.org/doc/html/latest/filesystems/proc.html)
- [Linux /sys filesystem](https://www.kernel.org/doc/html/latest/admin-guide/sysfs-rules.html)
- [NUMA Architecture](https://www.kernel.org/doc/html/latest/vm/numa.html)
- [RDF Triplets](https://www.w3.org/TR/rdf-concepts/)
- [Protocol Buffers](https://developers.google.com/protocol-buffers)