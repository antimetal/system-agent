# CLAUDE.md

This file provides comprehensive guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

The Antimetal Agent is a sophisticated Kubernetes controller written in Go that connects infrastructure to the Antimetal platform for cloud resource management. It's designed as a cloud-native agent that:

- **Collects Kubernetes resources** via controller-runtime patterns
- **Monitors system performance** through /proc and /sys filesystem collectors
- **Uploads data to Antimetal** via gRPC streaming to the intake service
- **Stores resource state** using BadgerDB for efficient tracking
- **Supports multi-cloud environments** with provider abstractions (AWS/EKS, KIND)

### Key Technologies
- **Go 1.24** with controller-runtime framework
- **Kubernetes** custom controller patterns
- **gRPC** for streaming data to intake service
- **BadgerDB** for embedded resource storage
- **Docker** with multi-arch support (linux/amd64, linux/arm64)
- **KIND** for local development and testing

## Architecture Overview

### Core Components

1. **Kubernetes Controller** (`internal/kubernetes/agent/`)
   - Watches K8s resources using controller-runtime
   - Implements event-driven reconciliation
   - Handles resource indexing and storage
   - Supports leader election for HA

2. **Intake Worker** (`internal/intake/`)
   - gRPC streaming client for data upload
   - Batches deltas for efficient transmission
   - Handles retry logic and stream recovery
   - Implements heartbeat mechanism

3. **Performance Monitoring** (`pkg/performance/`)
   - Collector architecture for system metrics
   - Supports both one-shot and continuous collection
   - Reads from /proc and /sys filesystems
   - Provides LoadStats, MemoryStats, CPUStats, etc.

4. **Resource Store** (`pkg/resource/store/`)
   - BadgerDB-backed storage for resource state
   - Supports resources and relationships (RDF triplets)
   - Event-driven subscription model
   - Efficient indexing and querying

5. **Cloud Provider Abstractions** (`internal/kubernetes/cluster/`)
   - Interface for different cloud providers
   - EKS implementation with auto-discovery
   - KIND support for local development
   - Extensible for GKE, AKS future support

### Directory Structure
```
├── cmd/main.go                    # Application entry point
├── internal/                      # Private application code
│   ├── intake/                    # gRPC intake worker
│   └── kubernetes/
│       ├── agent/                 # K8s controller logic
│       ├── cluster/               # Cloud provider abstractions
│       └── scheme/                # K8s scheme setup
├── pkg/                           # Public/reusable packages
│   ├── aws/                       # AWS client utilities
│   ├── performance/               # Performance monitoring system
│   │   └── collectors/            # System metric collectors
│   └── resource/                  # Resource management
│       └── store/                 # BadgerDB storage layer
└── config/                        # K8s manifests and Kustomize
```

## Development Workflow

### Prerequisites
- **Docker** (rootless, containerd snapshotter enabled)
- **kubectl** for K8s operations
- **Go 1.24+** as specified in go.mod

### Common Commands

#### Core Development
```bash
make build                    # Build binary for current platform
make test                     # Run tests with coverage
make lint                     # Run golangci-lint
make fmt                      # Format Go code
make vendor                   # Update vendored dependencies
make generate                 # Generate K8s manifests (after annotation changes)
make gen-license-headers      # ALWAYS run before committing
```

#### Local Testing with KIND
```bash
make cluster                  # Create antimetal-agent-dev KIND cluster
make docker-build             # Build Docker image
make load-image               # Load image into KIND cluster
make deploy                   # Deploy agent to current context
make undeploy                 # Remove agent from cluster
make destroy-cluster          # Delete KIND cluster
```

#### Quick Development Iteration
```bash
make build-and-load-image     # Rebuild and redeploy in one command
```

#### Multi-Architecture Support
```bash
make build-all               # Build for all platforms
make docker-build-all        # Build multi-arch Docker images
```

### Key Development Patterns

#### Code Generation
Always run `make generate` after:
- Modifying kubebuilder annotations (`+kubebuilder:rbac`)
- Changing CRD definitions
- Updating webhook configurations

#### License Headers
- **ALWAYS** run `make gen-license-headers` before committing
- All Go files must have the PolyForm Shield license header
- Uses `tools/license_check/license_check.py` for enforcement

#### Testing Philosophy
- Use standard Go testing framework
- Tests located alongside implementation files
- Table-driven tests for comprehensive coverage
- Mock external dependencies (gRPC, AWS, K8s)

## Performance Collector Architecture

### Collector Interface Design
The performance monitoring system follows a dual-interface pattern:

```go
// PointCollector - one-shot data collection
type PointCollector interface {
    Collect(ctx context.Context) (any, error)
}

// ContinuousCollector - streaming data collection
type ContinuousCollector interface {
    Start(ctx context.Context) (<-chan any, error)
    Stop() error
}
```

### Collector Implementation Patterns

#### Base Collector Pattern
```go
type BaseCollector struct {
    metricType   MetricType
    name         string
    logger       logr.Logger
    config       CollectionConfig
    capabilities CollectorCapabilities
}
```

#### Capabilities System
```go
type CollectorCapabilities struct {
    SupportsOneShot    bool
    SupportsContinuous bool
    RequiresRoot       bool
    RequiresEBPF       bool
    MinKernelVersion   string
}
```

### Performance Collector Testing Methodology

#### Standardized Testing Approach
Performance collectors follow a comprehensive testing pattern:

1. **Test Structure**
   - Table-driven tests for multiple scenarios
   - Temporary file system isolation using `t.TempDir()`
   - Mock /proc and /sys filesystem files
   - Reusable helper functions for common operations

2. **Core Testing Areas**
   - **Constructor validation**: Path handling, configuration validation
   - **Data parsing**: Valid scenarios, malformed input, edge cases
   - **Error handling**: Missing files, invalid data, graceful degradation
   - **File system operations**: Different proc paths, access errors
   - **Whitespace tolerance**: Leading/trailing whitespace handling

3. **Key Testing Patterns**
   ```go
   // Helper function pattern
   func createTestCollector(t *testing.T, content string) *Collector {
       tmpDir := t.TempDir()
       // Create mock files
       return collector
   }
   
   // Validation helper pattern
   func validateResults(t *testing.T, actual, expected *Stats) {
       assert.Equal(t, expected.Field, actual.Field)
   }
   ```

4. **Testing Requirements**
   - **Path validation**: Test absolute vs relative paths
   - **File interdependency**: Test behavior when related files unavailable
   - **Return type validation**: Explicit type assertions
   - **Graceful degradation**: Document critical vs optional files

5. **Testing Principles**
   - Don't test static properties (Name, RequiresRoot)
   - Focus on parsing logic and error handling
   - Use realistic test data from actual /proc files
   - Test collectors in `collectors_test` package

## Resource Store Architecture

### BadgerDB Integration
- **In-memory storage** for development/testing
- **Event-driven subscriptions** for real-time updates
- **RDF triplet relationships** (subject, predicate, object)
- **Efficient indexing** for complex queries

### Storage Patterns
```go
// Resource storage
AddResource(rsrc *Resource) error
UpdateResource(rsrc *Resource) error
DeleteResource(ref *ResourceRef) error

// Relationship storage (RDF triplets)
AddRelationships(rels ...*Relationship) error
GetRelationships(subject, object *ResourceRef, predicate proto.Message) error

// Event subscriptions
Subscribe(typeDef *TypeDescriptor) <-chan Event
```

## gRPC Integration

### Intake Service Communication
- **Streaming gRPC** for efficient data upload
- **Batched deltas** with configurable batch sizes
- **Exponential backoff** for connection failures
- **Stream recovery** with automatic reconnection
- **Heartbeat mechanism** for connection health

### Data Flow
1. K8s events → Controller → Resource Store
2. Resource Store → Event Router → Intake Worker
3. Intake Worker → Batching → gRPC Stream → Antimetal

## Multi-Cloud Provider Support

### Provider Interface
```go
type Provider interface {
    Name() string
    ClusterName(ctx context.Context) (string, error)
    Region(ctx context.Context) (string, error)
}
```

### Supported Providers
- **EKS**: Full AWS integration with auto-discovery
- **KIND**: Local development support
- **GKE/AKS**: Interface defined, implementation pending

## Configuration Management

### Command Line Flags
Comprehensive flag system for:
- Intake service configuration
- Kubernetes provider settings
- Performance monitoring options
- Security and TLS settings

### Environment Variables
- `NODE_NAME`: Node identification
- `HOST_PROC`, `HOST_SYS`, `HOST_DEV`: Containerized filesystem paths

## Security Considerations

### License Management
- **PolyForm Shield License** for source code
- License header enforcement via Python script
- Automatic license header generation

### Runtime Security
- **Non-root container** execution (user 65532)
- **Minimal distroless base** image
- **TLS by default** for gRPC connections
- **RBAC permissions** via kubebuilder annotations

## Debugging and Monitoring

### Logging
- **Structured logging** with logr
- **Contextual logging** with component names
- **Configurable log levels** via zap

### Metrics and Health
- **Prometheus metrics** via controller-runtime
- **Health checks** (`/healthz`, `/readyz`)
- **Pprof support** for performance profiling

### Debugging Commands
```bash
kubectl logs -n antimetal-system <pod-name>
kubectl get pods -n antimetal-system
kubectl describe deployment -n antimetal-system agent
```

## Build and Release

### Docker Multi-Arch
- **linux/amd64** and **linux/arm64** support
- **GoReleaser** for automated releases
- **Distroless base** for minimal attack surface

### Deployment
- **Kustomize** for configuration management
- **Helm charts** published separately
- **antimetal-system** namespace by default

## Testing Strategy

### Unit Testing
- **Mock external dependencies** (gRPC, AWS, K8s)
- **Table-driven tests** for comprehensive coverage
- **Temporary file systems** for isolation
- **Testify** for assertions and mocking

### Integration Testing
- **KIND clusters** for K8s integration
- **Mock intake service** for gRPC testing
- **BadgerDB in-memory** for storage testing

### Performance Testing
- **Benchmarks** for critical paths
- **Load testing** with realistic data volumes
- **Memory profiling** for optimization

## Development Notes

### Code Style
- **Early returns** to reduce nesting
- **Functional patterns** where applicable
- **Concise implementations** without unnecessary comments
- **Error wrapping** with context

### Common Pitfalls
- Always run `make generate` after annotation changes
- Don't forget license headers before committing
- Test with both AMD64 and ARM64 architectures
- Validate /proc file parsing with realistic data

### Performance Optimization
- **Efficient BadgerDB usage** with proper indexing
- **Batch gRPC operations** for network efficiency
- **Context cancellation** for graceful shutdowns
- **Memory pooling** for high-frequency operations

## eBPF Development

### CO-RE (Compile Once - Run Everywhere) Support

The Antimetal Agent uses **CO-RE** technology for portable eBPF programs that work across different kernel versions without recompilation. This provides significant operational benefits:

#### Key Benefits
- **Single Binary Deployment**: Same eBPF program runs on kernels 4.18+
- **Automatic Field Relocation**: Kernel structure changes handled automatically
- **No Runtime Compilation**: Pre-compiled programs with BTF relocations
- **Improved Reliability**: Reduced kernel compatibility issues

#### Technical Implementation

1. **BTF (BPF Type Format) Support**
   - Native kernel BTF on kernels 5.2+ at `/sys/kernel/btf/vmlinux`
   - Pre-generated vmlinux.h from BTF hub for portability
   - BTF verification during build process

2. **Compilation Flags**
   ```makefile
   CFLAGS := -g -O2 -Wall -target bpf -D__TARGET_ARCH_$(ARCH) \
       -fdebug-types-section -fno-stack-protector
   ```
   - `-g`: Enable BTF generation
   - `-fdebug-types-section`: Improve BTF quality
   - `-fno-stack-protector`: Required for BPF

3. **CO-RE Macros**
   - Use `BPF_CORE_READ()` for field access
   - Automatic offset calculation at load time
   - Example: `BPF_CORE_READ(task, real_parent, tgid)`

4. **Runtime Support**
   - `pkg/ebpf/core` package for kernel feature detection
   - Automatic BTF discovery and loading
   - cilium/ebpf v0.19.0 handles relocations

#### Kernel Compatibility Matrix

| Kernel Version | BTF Support | CO-RE Support | Notes |
|----------------|-------------|---------------|-------|
| 5.2+           | Native      | Full          | Best performance, native BTF |
| 4.18-5.1       | External    | Partial       | Requires BTF from btfhub |
| <4.18          | None        | None          | Traditional compilation only |

#### CO-RE Development Workflow

1. **Write CO-RE Compatible Code**
   ```c
   #include "vmlinux.h"
   #include <bpf/bpf_core_read.h>
   
   // Use CO-RE macros for kernel struct access
   pid_t ppid = BPF_CORE_READ(task, real_parent, tgid);
   ```

2. **Build with CO-RE Support**
   ```bash
   make build-ebpf  # Automatically uses CO-RE flags
   ```

3. **Verify BTF Generation**
   - Build process includes BTF verification step
   - Check with: `bpftool btf dump file <program>.bpf.o`

4. **Test Compatibility**
   ```bash
   ./ebpf/scripts/check_core_support.sh  # Check system CO-RE support
   ```

### Adding New eBPF Programs
For new `.bpf.c` files:
1. Create `ebpf/src/your_program.bpf.c`
2. Include CO-RE headers:
   ```c
   #include "vmlinux.h"
   #include <bpf/bpf_core_read.h>
   ```
3. Use CO-RE macros for kernel struct access
4. Add to `BPF_PROGS` in `ebpf/Makefile`
5. Run `make build-ebpf`

For new struct definitions:
1. Create `ebpf/include/your_collector_types.h` with C structs
2. Run `make generate-ebpf-types` to generate Go types
3. Generated files appear in `pkg/performance/collectors/`

### eBPF Commands
- `make build-ebpf` - Build eBPF programs with CO-RE support
- `make generate-ebpf-bindings` - Generate Go bindings from eBPF C code (runs go:generate in collectors)
- `make generate-ebpf-types` - Generate Go types from eBPF header files
- `make build-ebpf-builder` - Build/rebuild eBPF Docker image
- `./ebpf/scripts/check_core_support.sh` - Check system CO-RE capabilities

### CO-RE Best Practices

1. **Always Use CO-RE Macros**
   - Prefer `BPF_CORE_READ()` over direct field access
   - Use `BPF_CORE_READ_STR()` for string fields
   - Check field existence with `BPF_CORE_FIELD_EXISTS()`

2. **Test Across Kernels**
   - Test on minimum supported kernel (4.18)
   - Verify on latest stable kernel
   - Use KIND clusters with different kernel versions

3. **Handle Missing Fields Gracefully**
   - Not all kernel versions have all fields
   - Use conditional compilation or runtime checks
   - Provide fallback behavior

4. **Monitor BTF Size**
   - BTF adds ~100KB to each eBPF object
   - Worth it for portability benefits
   - Strip BTF for size-critical deployments if needed

### Troubleshooting CO-RE

1. **BTF Verification Failures**
   - Ensure clang has `-g` flag
   - Check clang version (10+ recommended)
   - Verify vmlinux.h is accessible

2. **Runtime Loading Errors**
   - Check kernel has BTF: `ls /sys/kernel/btf/vmlinux`
   - Verify CO-RE support: `./ebpf/scripts/check_core_support.sh`
   - Check dmesg for BPF verifier errors

3. **Field Access Errors**
   - Ensure using CO-RE macros not direct access
   - Verify field exists in target kernel version
   - Check struct definitions in vmlinux.h

## Future Extensibility

### Planned Features
- **eBPF collectors** for deep system monitoring
- **GKE/AKS provider** implementations
- **Additional performance metrics** (memory bandwidth, etc.)
- **Persistent storage** options beyond in-memory

### Extension Points
- **Collector registry** for new metric types
- **Provider interface** for additional cloud platforms
- **gRPC interceptors** for custom processing
- **Event filters** for selective data collection

This comprehensive guide should enable effective development and maintenance of the Antimetal Agent codebase while maintaining consistency with established patterns and practices.