# Hardware Graph Development Guide

## Quick Start

### Prerequisites
1. Fork and clone the `antimetal/apis` repository for protobuf definitions
2. Update `buf.gen.yaml` to point to your local fork:
```yaml
inputs:
  - directory: ../jra3-apis/api
```

### Build and Test
```bash
# Generate protobuf code
make proto

# Run tests
go test ./internal/hardware/... -v

# Format code
make fmt

# Add license headers
make gen-license-headers
```

## Development Workflow

### 1. Adding New Hardware Node Types

**Step 1:** Define the protobuf message in `jra3-apis/api/hardware/v1/hardware.proto`:
```protobuf
message GPUDeviceNode {
  string device = 1;
  string vendor = 2;
  string model = 3;
  uint64 memory_bytes = 4;
  uint32 compute_capability = 5;
}
```

**Step 2:** Regenerate protobuf code:
```bash
make proto
```

**Step 3:** Add builder method in `internal/hardware/graph/builder.go`:
```go
func (b *Builder) createGPUDeviceNode(gpu *performance.GPUInfo) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
    gpuSpec := &hardwarev1.GPUDeviceNode{
        Device:            gpu.Device,
        Vendor:            gpu.Vendor,
        Model:             gpu.Model,
        MemoryBytes:       gpu.MemoryBytes,
        ComputeCapability: gpu.ComputeCapability,
    }
    
    specAny, err := anypb.New(gpuSpec)
    if err != nil {
        return nil, nil, fmt.Errorf("failed to marshal GPU spec: %w", err)
    }
    
    name := fmt.Sprintf("gpu-%s", gpu.Device)
    resource := &resourcev1.Resource{
        Type: &resourcev1.TypeDescriptor{
            Kind: "GPUDeviceNode",
            Type: string(gpuSpec.ProtoReflect().Descriptor().FullName()),
        },
        Metadata: &resourcev1.ResourceMeta{
            Provider:   resourcev1.Provider_PROVIDER_HARDWARE,
            ProviderId: name,
            Name:       name,
        },
        Spec: specAny,
    }
    
    ref := &resourcev1.ResourceRef{
        TypeUrl: string(gpuSpec.ProtoReflect().Descriptor().FullName()),
        Name:    name,
    }
    
    return resource, ref, nil
}
```

**Step 4:** Integrate into topology building:
```go
func (b *Builder) buildGPUTopology(ctx context.Context, gpuInfo []performance.GPUInfo, systemRef *resourcev1.ResourceRef) error {
    for _, gpu := range gpuInfo {
        gpuNode, gpuRef, err := b.createGPUDeviceNode(&gpu)
        if err != nil {
            return fmt.Errorf("failed to create GPU %s: %w", gpu.Device, err)
        }
        
        if err := b.store.AddResource(gpuNode); err != nil {
            return fmt.Errorf("failed to add GPU device: %w", err)
        }
        
        if err := b.createContainsRelationship(systemRef, gpuRef, "physical"); err != nil {
            return fmt.Errorf("failed to create system->gpu relationship: %w", err)
        }
    }
    return nil
}
```

### 2. Adding New Relationship Types

**Step 1:** Define predicate in `hardware.proto`:
```protobuf
message PCIeConnectionPredicate {
  string bus_address = 1;
  uint32 link_speed = 2;  // GT/s
  uint32 link_width = 3;  // x1, x4, x8, x16
}
```

**Step 2:** Create relationship builder:
```go
func (b *Builder) createPCIeConnectionRelationship(device, controller *resourcev1.ResourceRef, busAddr string, speed, width uint32) error {
    predicate := &hardwarev1.PCIeConnectionPredicate{
        BusAddress: busAddr,
        LinkSpeed:  speed,
        LinkWidth:  width,
    }
    
    predicateAny, err := anypb.New(predicate)
    if err != nil {
        return fmt.Errorf("failed to marshal PCIe predicate: %w", err)
    }
    
    relationship := &resourcev1.Relationship{
        Type: &resourcev1.TypeDescriptor{
            Kind: "PCIeConnection",
            Type: string(predicate.ProtoReflect().Descriptor().FullName()),
        },
        Subject:   device,
        Object:    controller,
        Predicate: predicateAny,
    }
    
    return b.store.AddRelationships(relationship)
}
```

### 3. Testing Hardware Graph Components

**Unit Test Template:**
```go
func TestBuilder_BuildGPUTopology(t *testing.T) {
    ctx := context.Background()
    logger := logr.Discard()
    mockStore := newMockStore()
    
    builder := graph.NewBuilder(logger, mockStore)
    
    snapshot := &performance.Snapshot{
        Timestamp: time.Now(),
        Metrics: performance.Metrics{
            GPUInfo: []performance.GPUInfo{
                {
                    Device:            "nvidia0",
                    Vendor:            "NVIDIA",
                    Model:             "Tesla V100",
                    MemoryBytes:       17179869184, // 16GB
                    ComputeCapability: 70,
                },
            },
        },
    }
    
    err := builder.BuildFromSnapshot(ctx, snapshot)
    require.NoError(t, err)
    
    // Verify GPU node was created
    found := false
    for _, r := range mockStore.resources {
        if r.Type.Kind == "GPUDeviceNode" {
            found = true
            var spec hardwarev1.GPUDeviceNode
            err := anypb.UnmarshalTo(r.Spec, &spec, proto.UnmarshalOptions{})
            require.NoError(t, err)
            assert.Equal(t, "nvidia0", spec.Device)
            assert.Equal(t, "NVIDIA", spec.Vendor)
            assert.Equal(t, uint64(17179869184), spec.MemoryBytes)
        }
    }
    assert.True(t, found, "GPU node should have been created")
}
```

## Code Organization

### Package Structure
```
internal/hardware/
├── manager.go           # Orchestration and scheduling
├── graph/
│   ├── builder.go       # Graph construction
│   ├── builder_test.go  # Unit tests
│   ├── cpu.go          # CPU-specific builders (future)
│   ├── memory.go       # Memory-specific builders (future)
│   ├── storage.go      # Storage-specific builders (future)
│   └── network.go      # Network-specific builders (future)
```

### Separation of Concerns

**Manager (`manager.go`):**
- Scheduling and timing
- Collector orchestration
- Snapshot aggregation
- Error recovery

**Builder (`graph/builder.go`):**
- Node creation
- Relationship establishment
- Type conversions
- Graph structure logic

**Performance Collectors:**
- Raw data collection
- File system interaction
- Data parsing
- No graph knowledge

## Best Practices

### 1. Error Handling
```go
// Always wrap errors with context
if err := b.store.AddResource(node); err != nil {
    return fmt.Errorf("failed to add %s node: %w", nodeType, err)
}

// Log non-critical failures
if err := b.collectOptionalData(); err != nil {
    b.logger.V(1).Info("Failed to collect optional data", "error", err)
    // Continue with partial data
}
```

### 2. Type Safety
```go
// Use type assertions with checks
if cpuInfo, ok := data.(*performance.CPUInfo); ok {
    snapshot.Metrics.CPUInfo = cpuInfo
} else {
    return fmt.Errorf("unexpected type %T for CPU info", data)
}
```

### 3. Resource Naming
```go
// Consistent naming pattern: type-identifier
name := fmt.Sprintf("cpu-package-%d", packageID)
name := fmt.Sprintf("disk-%s", deviceName)
name := fmt.Sprintf("network-%s", interfaceName)
```

### 4. Relationship Creation
```go
// Always create bidirectional relationships when appropriate
if err := b.createContainsRelationship(parent, child, "physical"); err != nil {
    return err
}
// Consider: Should there be an inverse "ContainedBy" relationship?
```

### 5. Testing
```go
// Test edge cases
- Empty snapshots
- Partial data
- Maximum values (MaxUint64, etc.)
- Missing optional fields
- Malformed data

// Use table-driven tests for variations
tests := []struct {
    name     string
    snapshot *performance.Snapshot
    wantErr  bool
    validate func(t *testing.T, store *mockStore)
}{
    // Test cases...
}
```

## Debugging

### Enable Verbose Logging
```go
// In development
logger := logr.NewLogger(logr.WithVerbosity(2))

// Use log levels appropriately
logger.Info("Normal operation message")
logger.V(1).Info("Debug message")
logger.V(2).Info("Trace message")
```

### Inspect Graph Contents
```go
// Dump all hardware nodes
resources, _ := store.GetResources(
    &resourcev1.TypeDescriptor{
        Type: "hardware.v1.*",
    },
)

for _, r := range resources {
    fmt.Printf("Node: %s (%s)\n", r.Metadata.Name, r.Type.Kind)
}
```

### Trace Relationship Paths
```go
// Find all relationships from a node
rels, _ := store.GetRelationships(
    &resourcev1.ResourceRef{
        TypeUrl: "hardware.v1.SystemNode",
        Name:    hostname,
    },
    nil, // any object
    nil, // any predicate
)

for _, rel := range rels {
    fmt.Printf("%s -[%s]-> %s\n", 
        rel.Subject.Name,
        rel.Type.Kind,
        rel.Object.Name,
    )
}
```

## Performance Optimization

### 1. Batch Operations
```go
// Good: Batch relationship creation
relationships := make([]*resourcev1.Relationship, 0, len(cores))
for _, core := range cores {
    rel := b.buildRelationship(packageRef, coreRef)
    relationships = append(relationships, rel)
}
err := b.store.AddRelationships(relationships...)

// Bad: Individual operations
for _, core := range cores {
    rel := b.buildRelationship(packageRef, coreRef)
    err := b.store.AddRelationships(rel)
}
```

### 2. Preallocate Slices
```go
// When size is known
nodes := make([]*resourcev1.Resource, 0, len(devices))
```

### 3. Reuse Builders
```go
// Cache commonly used predicates
var containsPhysical = &hardwarev1.ContainsPredicate{Type: "physical"}
```

### 4. Concurrent Collection
```go
// Collect independent data in parallel
var wg sync.WaitGroup
var mu sync.Mutex

wg.Add(3)
go func() {
    defer wg.Done()
    cpuData := collectCPU()
    mu.Lock()
    snapshot.CPUInfo = cpuData
    mu.Unlock()
}()
// ... similar for memory, disk
wg.Wait()
```

## Integration Points

### With Performance Manager
```go
// Get specific collector
collector, err := perfManager.GetCollector(performance.MetricTypeCPUInfo)

// Collect data
data, err := collector.Collect(ctx)

// Type assert to expected type
cpuInfo, ok := data.(*performance.CPUInfo)
```

### With Resource Store
```go
// Add resources
err := store.AddResource(resource)

// Add relationships
err := store.AddRelationships(relationships...)

// Query resources
resources, err := store.GetResources(filter)

// Subscribe to changes
events := store.Subscribe(typeDef)
```

### With Main Application
```go
// In cmd/main.go or similar
hwManager, err := hardware.NewManager(logger, config)
if err != nil {
    return err
}

// Start hardware discovery
if err := hwManager.Start(); err != nil {
    return err
}

// Register shutdown
defer hwManager.Stop()
```

## Common Pitfalls

### 1. Type Mismatches
```go
// Problem: Proto uses uint64, Go type uses int64
// Solution: Explicit conversion
spec.SizeBytes = uint64(goType.Size)
```

### 2. Missing Nil Checks
```go
// Always check optional data
if snapshot.Metrics.CPUInfo != nil {
    // Process CPU info
}
```

### 3. Resource Leaks
```go
// Always clean up
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()
```

### 4. Import Cycles
```go
// Avoid by keeping dependencies unidirectional
// hardware → performance ✓
// performance → hardware ✗
```

## Future Development Areas

### Enhanced Collectors
- GPU topology (NVIDIA, AMD, Intel)
- Hardware security modules (HSM/TPM)
- Battery and power supplies
- Cooling systems and thermal zones

### Advanced Relationships
- Memory hierarchy (L1/L2/L3 cache)
- PCIe device tree
- USB device hierarchy
- Network bonding/teaming

### Performance Metrics
- Attach real-time metrics to nodes
- Historical data aggregation
- Anomaly detection
- Capacity planning

### Cloud Integration
- Link to cloud provider metadata
- Virtual-to-physical mapping
- Nested virtualization awareness
- Container runtime integration