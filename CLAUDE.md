# CLAUDE.md

This file provides comprehensive guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Code Review Guidelines

When performing code reviews on pull requests:

### Feedback Structure
- **IMPORTANT**: Use collapsible sections (`<details>` tags) for non-actionable feedback, explanations, or background information
- Keep actionable items (bugs, required changes) visible by default
- Use this format for non-critical suggestions:

```markdown
<details>
<summary>💡 Suggestion: [Brief description]</summary>

[Detailed explanation or rationale]

</details>
```

### Example Review Format
```markdown
## Review Summary
✅ **Required Changes** (visible by default)
- Fix memory leak in line 42
- Add error handling for null case

<details>
<summary>📚 Code Quality Observations</summary>

- Consider using early returns to reduce nesting
- The function could be split into smaller units
- Variable naming could be more descriptive

</details>

<details>
<summary>🔍 Performance Considerations</summary>

While not critical, you might consider:
- Using a map instead of repeated array lookups
- Caching the compiled regex pattern

</details>
```

### Review Priorities
1. **Always visible**: Security issues, bugs, breaking changes
2. **Collapsible**: Style suggestions, minor optimizations, educational content
3. **Focus on**: Constructive, actionable feedback over nitpicking

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

Use `make help` to see the full list of available commands.
Below are commands for common workflows.

#### Core Development
```bash
make build                    # Build binary for current platform
make test                     # Run tests with coverage
make lint                     # Run golangci-lint
make fmt                      # Format Go code
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

#### Constructor Pattern
All collectors must follow a consistent constructor pattern:
```go
func NewXCollector(logger logr.Logger, config performance.CollectionConfig) (*XCollector, error) {
    // 1. Validate paths are absolute
    if !filepath.IsAbs(config.HostProcPath) {
        return nil, fmt.Errorf("HostProcPath must be an absolute path, got: %q", config.HostProcPath)
    }
    if !filepath.IsAbs(config.HostSysPath) {  // If collector uses sysfs
        return nil, fmt.Errorf("HostSysPath must be an absolute path, got: %q", config.HostSysPath)
    }
    
    // 2. Define capabilities
    capabilities := performance.CollectorCapabilities{
        SupportsOneShot:    true,
        SupportsContinuous: false,
        RequiresRoot:       false,
        RequiresEBPF:       false,
        MinKernelVersion:   "2.6.0",
    }
    
    // 3. Return collector with pre-computed paths
    return &XCollector{
        BaseCollector: performance.NewBaseCollector(...),
        specificPath: filepath.Join(config.HostProcPath, "specific/file"),
    }, nil
}
```

#### Compile-Time Interface Checks
Every collector must include a compile-time interface check:
```go
// Compile-time interface check
var _ performance.Collector = (*NetworkCollector)(nil)
```

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

#### Error Handling Strategy
Collectors must clearly distinguish between critical and optional data and should never panic:

- **Critical files**: Return error immediately if unavailable (e.g., /proc/loadavg for LoadCollector)
- **Optional files**: Log warning and continue with graceful degradation (e.g., /sys/class/net/* metadata)
- **Parse errors**: Handle based on field importance - critical fields cause errors, optional fields are logged
- **Panic prevention**: Collectors must use proper error handling instead of panicking. All errors should be returned to the caller

**Graceful Degradation**: When optional data is unavailable, collectors should:
1. Log the issue at appropriate verbosity level (V(2) for debug info)
2. Continue processing with available data
3. Return partial results rather than failing entirely
4. Document which fields may be missing in degraded mode

Document the error handling strategy in method comments:
```go
// collectNetworkStats reads network statistics
//
// Error handling strategy:
// - /proc/net/dev is critical - returns error if unavailable
// - /sys/class/net/* files are optional - logs warnings but continues
// - Malformed lines in /proc/net/dev are skipped with logging
// - Never panics - all errors are returned to caller
```

#### Adding Collector to Registry
In order to use a collector, it has to be added to the registry so that the manager knows about it.
This is typically done in the init() function in the source file where the collector is implemented: 

For PointCollectors, you need to transform them into ContinuousCollectors in order to add them to the registry.
There are two wrappers to do that depending on use case:
1. `ContinuousPointCollector`: wraps a PointCollector to call `Collect()` on an interval
2. `OnceContinuousCollector`: wraps a PointCollector where it calls `Collect()` once and caches the result

Use `PartialNewContinuousPointCollector` if the collector is collecting runtime statistics.

```go
func init() {
	performance.Register(performance.MetricTypeXXX, performance.PartialNewContinuousPointCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewXCollector(logger, config)
		},
	))
}
```

Use `PartialOnceContinuousCollector` when collecting hardware information or if the collector needs to run just once.

```go
func init() {
	performance.Register(performance.MetricTypeXXX, performance.PartialNewOnceContinuousCollector(
		func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
			return NewXCollector(logger, config)
		},
	))
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
   // Helper function pattern - consistent naming and structure
   func createTestXCollector(t *testing.T, procContent string, sysFiles map[string]string) (*XCollector, string, string) {
       tmpDir := t.TempDir()
       procPath := filepath.Join(tmpDir, "proc")
       sysPath := filepath.Join(tmpDir, "sys")
       
       // Setup mock files...
       
       config := performance.CollectionConfig{
           HostProcPath: procPath,
           HostSysPath:  sysPath,
       }
       collector, err := collectors.NewXCollector(logr.Discard(), config)
       require.NoError(t, err)
       
       return collector, procPath, sysPath
   }
   
   // Collection and validation helper
   func collectAndValidateX(t *testing.T, collector *XCollector, expectError bool, validate func(t *testing.T, result TypedResult)) {
       result, err := collector.Collect(context.Background())
       
       if expectError {
           assert.Error(t, err)
           return
       }
       
       require.NoError(t, err)
       typedResult, ok := result.(TypedResult)
       require.True(t, ok, "result should be TypedResult")
       
       if validate != nil {
           validate(t, typedResult)
       }
   }
   
   // Test data as constants
   const validProcFile = `actual /proc file content here`
   const malformedProcFile = `malformed content`
   ```

4. **Testing Requirements**
   - **Constructor tests**: Separate test function for constructor validation
   - **Path validation**: Test absolute vs relative paths, empty paths
   - **File interdependency**: Test behavior when related files unavailable
   - **Return type validation**: Explicit type assertions with proper error messages
   - **Graceful degradation**: Document and test critical vs optional files
   - **Boundary conditions**: Test zero values, maximum values (e.g., max uint64)
   - **Malformed input**: Test partial data, missing fields, corrupt formats
   - **Special cases**: Test virtual interfaces, disabled devices, etc.

5. **Testing Principles**
   - Don't test static properties (Name, RequiresRoot)
   - Don't test compile-time interface checks (e.g., `var _ performance.Collector = (*XCollector)(nil)`)
   - Focus on parsing logic and error handling
   - Use realistic test data from actual /proc files
   - Test collectors in `collectors_test` package (external testing)
   - Name test functions consistently: `TestXCollector_Constructor`, `TestXCollector_Collect`
   - Use descriptive test names that explain the scenario
   - Group related test scenarios in the same test function

6. **Documentation Standards**
   - **Type-level documentation**: Explain purpose, data sources, references
   - **Method documentation**: Include format examples and error handling strategy
   - **Inline documentation**: Explain complex parsing logic or non-obvious decisions
   - **Reference links**: Include kernel documentation links where applicable

### Collector Development Workflow

When adding a new performance collector:

1. **Define the data structure** in `pkg/performance/types.go`
2. **Create the collector** in `pkg/performance/collectors/your_collector.go`
   - Include compile-time interface check
   - Follow constructor pattern with path validation
   - Document error handling strategy
3. **Create comprehensive tests** in `pkg/performance/collectors/your_collector_test.go`
   - Use `collectors_test` package
   - Include constructor tests
   - Add helper functions following naming patterns
   - Test edge cases and error scenarios
4. **Update collector registry**
   - Write an init() function in `your_collector.go` to add the collector to the registry
5. **Run validation**:
   ```bash
   make test                    # Run tests
   make fmt                     # Run go fmt
   make lint                    # Check code style
   make gen-license-headers     # Ensure license headers
   ```

### Common Collector Patterns

#### Reading Single Value Files
```go
data, err := os.ReadFile(c.somePath)
if err != nil {
    return nil, fmt.Errorf("failed to read %s: %w", c.somePath, err)
}
value := strings.TrimSpace(string(data))
```

#### Parsing Multi-Line Files
```go
scanner := bufio.NewScanner(file)
for scanner.Scan() {
    line := scanner.Text()
    // Parse line
}
if err := scanner.Err(); err != nil {
    return nil, fmt.Errorf("error reading %s: %w", c.somePath, err)
}
```

#### Handling Optional Metadata
```go
// Try to read optional file, but don't fail if missing
if data, err := os.ReadFile(optionalPath); err == nil {
    // Process optional data
} else {
    c.Logger().V(2).Info("Optional file not available", "path", optionalPath, "error", err)
}
```

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

### Adding New eBPF Programs
For new `.bpf.c` files:
1. Create `ebpf/src/your_program.bpf.c`
2. Add `//go:generate` directive to the relevant userspace package that will use the eBPF program:
   ```go
   //go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang YourProgram ../../ebpf/src/your_program.bpf.c
   ```
3. Run `make generate-ebpf-bindings`

For new struct definitions:
1. Create `ebpf/include/your_collector_types.h` with C structs
2. Run `make generate-ebpf-types` to generate Go types
3. Generated files appear in `pkg/performance/collectors/`

### eBPF Commands
- `make generate-ebpf-bindings` - Generate Go bindings from eBPF C code
- `make generate-ebpf-types` - Generate Go types from eBPF header files
- `make build-ebpf` - Build eBPF programs (uses Docker on non-Linux)
- `make build-ebpf-builder` - Build eBPF Docker image

### Generation Pattern
Place `//go:generate` directives in the userspace packages that will use the eBPF programs, rather than in a centralized location. This keeps the generation logic co-located with the code that uses it.

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