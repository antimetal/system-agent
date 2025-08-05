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
        SupportsOneShot:      true,
        SupportsContinuous:   false,
        RequiredCapabilities: nil, // No special capabilities required
        MinKernelVersion:     "2.6.0",
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
The collector capabilities system provides granular Linux kernel capability requirements for collectors:

```go
type CollectorCapabilities struct {
    SupportsOneShot      bool
    SupportsContinuous   bool
    RequiredCapabilities []capabilities.Capability
    MinKernelVersion     string
}

// Check if collector can run with current process capabilities
func (c CollectorCapabilities) CanRun() (bool, []capabilities.Capability, error)
```

**Available Linux Capabilities:**
- `CAP_SYS_ADMIN`: Required for eBPF on older kernels, tracepoints
- `CAP_SYSLOG`: Required for /dev/kmsg access (kernel messages)
- `CAP_PERFMON`: Required for eBPF performance monitoring (kernel 5.8+)
- `CAP_BPF`: Required for eBPF programs (kernel 5.8+)

**Collector Capability Examples:**
```go
// No capabilities required (most collectors)
RequiredCapabilities: nil

// Kernel message collector  
RequiredCapabilities: []capabilities.Capability{capabilities.CAP_SYSLOG}

// eBPF collector
RequiredCapabilities: append(capabilities.GetEBPFCapabilities(), capabilities.CAP_SYSLOG)
```

**Runtime Capability Checking:**
```go
capabilities := collector.Capabilities()
canRun, missing, err := capabilities.CanRun()
if !canRun {
    log.Warn("Collector cannot run, missing capabilities", "missing", missing)
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

### Continuous Collector Pattern

When implementing collectors that support continuous collection (ContinuousCollector interface), follow this standardized pattern for proper lifecycle management:

#### Key Components
1. **Two control mechanisms**:
   - **Context** (passed to Start): External lifecycle management from parent/app
   - **Stopped channel** (internal): Direct control via Stop() method

2. **Channel management**:
   - Data channel (`ch`): Created in Start(), closed in Stop()
   - Stopped channel (`stopped`): Created in Start(), closed in Stop()

#### Implementation Pattern
```go
type MyCollector struct {
    performance.BaseContinuousCollector
    // ... collector-specific fields ...
    
    // Channel management
    ch      chan any
    stopped chan struct{}
}

func (c *MyCollector) Start(ctx context.Context) (<-chan any, error) {
    if c.Status() != performance.CollectorStatusDisabled {
        return nil, fmt.Errorf("collector already running")
    }
    
    c.SetStatus(performance.CollectorStatusActive)
    
    // Initialize state if needed
    // ...
    
    c.ch = make(chan any)
    c.stopped = make(chan struct{})
    go c.runCollection(ctx)
    return c.ch, nil
}

func (c *MyCollector) Stop() error {
    if c.Status() == performance.CollectorStatusDisabled {
        return nil
    }
    
    if c.stopped != nil {
        close(c.stopped)
        c.stopped = nil
    }
    
    // Give goroutine time to exit cleanly
    time.Sleep(10 * time.Millisecond)
    
    if c.ch != nil {
        close(c.ch)
        c.ch = nil
    }
    
    c.SetStatus(performance.CollectorStatusDisabled)
    return nil
}

func (c *MyCollector) runCollection(ctx context.Context) {
    ticker := time.NewTicker(c.interval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            // External shutdown (app closing)
            return
        case <-c.stopped:
            // Stop() was called
            return
        case <-ticker.C:
            data, err := c.collect(ctx)
            if err != nil {
                c.Logger().Error(err, "Failed to collect")
                c.SetError(err)
                continue
            }
            
            select {
            case c.ch <- data:
            case <-ctx.Done():
                return
            case <-c.stopped:
                return
            }
        }
    }
}
```

#### Why This Pattern?
1. **Dual control**: Responds to both external shutdown (context) and explicit Stop()
2. **Clean shutdown**: Stop() actually stops the goroutine and cleans up resources
3. **No orphaned goroutines**: Either mechanism ensures proper cleanup
4. **Consistent behavior**: All continuous collectors work the same way
5. **Prevents resource leaks**: Channels are properly closed

#### Usage Scenarios
- **App shutdown**: Context cancellation stops all collectors automatically
- **Individual control**: Stop() specific collectors while others continue
- **Reconfiguration**: Stop(), reconfigure, Start() again
- **Debugging**: Temporarily disable specific collectors

This pattern ensures collectors integrate well with Kubernetes controllers and other lifecycle management systems while providing fine-grained control when needed.

## Testing Methodology

### Overview

The system agent follows a comprehensive testing strategy that distinguishes between unit tests (portable, mock-based) and integration tests (Linux-specific, real system interaction). This separation ensures developers can work effectively on any platform while maintaining thorough test coverage on target Linux systems.

### Test Categories

#### Unit Tests
- **Purpose**: Test business logic, algorithms, and data structures in isolation
- **Characteristics**:
  - Run on any platform (macOS, Windows, Linux)
  - Use mocked filesystems and dependencies
  - Fast execution with no external dependencies
  - Default test type (no build tags required)
- **File naming**: `*_test.go`
- **Example**: Testing parsing logic with mock /proc files

#### Integration Tests
- **Purpose**: Verify interaction with real Linux kernel features and filesystems
- **Characteristics**:
  - Require Linux system with actual /proc, /sys filesystems
  - May require specific kernel versions or capabilities
  - Test actual system behavior and edge cases
  - Use `//go:build integration` build tag
- **File naming**: `*_integration_test.go`
- **Example**: Testing eBPF program loading on real kernel

### Build Tags Convention

#### Integration Test Files
```go
//go:build integration

package collectors_test

import (
    "testing"
    "github.com/antimetal/agent/pkg/testutil"
)

func TestRealProcFilesystem(t *testing.T) {
    testutil.RequireLinux(t)
    testutil.RequireLinuxFilesystem(t)
    // Test with real /proc filesystem
}
```

#### Unit Test Files (Optional Explicit Tag)
```go
//go:build !integration

package collectors_test

// Regular unit tests that work anywhere
```

### File Organization

Tests should live alongside the code they test:
```
pkg/
├── kernel/
│   ├── version.go
│   ├── version_test.go              # Unit tests
│   └── version_integration_test.go   # Integration tests
├── performance/collectors/
│   ├── cpu_info.go
│   ├── cpu_info_test.go             # Unit tests with mocked /proc
│   └── cpu_info_integration_test.go  # Tests with real /proc
└── ebpf/core/
    ├── core.go
    ├── core_test.go                  # Unit tests
    └── core_integration_test.go      # Tests requiring BTF/BPF
```

### Running Tests

#### Development Commands
```bash
# Run unit tests only (default, works on any platform)
make test

# Run unit tests with specific package
go test ./pkg/kernel/...

# Run integration tests (Linux only)
make test-integration

# Run both unit and integration tests
make test-all

# Run integration tests for specific package
go test -tags integration ./pkg/performance/collectors/...
```

#### CI/CD Commands
```bash
# GitHub Actions for unit tests (all platforms)
go test -v -race -coverprofile=coverage.out ./...

# GitHub Actions for integration tests (Linux runners only)
go test -v -tags integration -race ./...
```

### Test Helpers

The `pkg/testutil` package provides helpers for integration tests:

```go
// Skip test if not on Linux
testutil.RequireLinux(t)

// Verify Linux filesystem availability
testutil.RequireLinuxFilesystem(t)

// Check for specific kernel capabilities
testutil.RequireCapability(t, capabilities.CAP_BPF)

// Check for minimum kernel version
testutil.RequireKernelVersion(t, 5, 8, 0)

// Check for BTF support
testutil.RequireBTF(t)
```

### Migration Strategy

For existing tests in `test/integration/`:
1. Identify tests that can be converted to unit tests with mocking
2. Move true integration tests to live alongside their code
3. Add appropriate build tags
4. Update import paths and test helpers
5. Gradually phase out the `test/integration/` directory

### Best Practices

1. **Prefer Unit Tests**: Write unit tests whenever possible using mocks
2. **Integration Tests for Verification**: Use integration tests to verify assumptions about system behavior
3. **Clear Test Names**: Use descriptive names that explain what system feature is being tested
4. **Document Requirements**: Clearly state what system features/kernel versions are required
5. **Graceful Skipping**: Use test helpers to skip tests when requirements aren't met
6. **Platform-Specific Behavior**: Document when behavior differs across kernel versions

### Examples

#### Unit Test Example
```go
// cpu_info_test.go
//go:build !integration

func TestCPUInfoCollector_ParsesCPUInfo(t *testing.T) {
    // Create mock /proc/cpuinfo
    tmpDir := t.TempDir()
    procPath := filepath.Join(tmpDir, "proc")
    require.NoError(t, os.MkdirAll(procPath, 0755))
    require.NoError(t, os.WriteFile(
        filepath.Join(procPath, "cpuinfo"),
        []byte(mockCPUInfo),
        0644,
    ))
    
    // Test with mocked filesystem
    collector, err := NewCPUInfoCollector(logr.Discard(), CollectionConfig{
        HostProcPath: procPath,
    })
    require.NoError(t, err)
    
    result, err := collector.Collect(context.Background())
    require.NoError(t, err)
    // Assert on parsed result
}
```

#### Integration Test Example
```go
// cpu_info_integration_test.go
//go:build integration

func TestCPUInfoCollector_RealProcFS(t *testing.T) {
    testutil.RequireLinux(t)
    testutil.RequireLinuxFilesystem(t)
    
    // Test with real /proc filesystem
    collector, err := NewCPUInfoCollector(logr.Discard(), CollectionConfig{
        HostProcPath: "/proc",
    })
    require.NoError(t, err)
    
    result, err := collector.Collect(context.Background())
    require.NoError(t, err)
    
    cpuInfo := result.(*CPUInfo)
    // Verify actual system CPU information
    assert.Greater(t, len(cpuInfo.Processors), 0, "Should detect at least one CPU")
    assert.NotEmpty(t, cpuInfo.Processors[0].VendorID, "Should have vendor ID")
}
```

## Container Resource Monitoring

### Overview

The system agent includes collectors for monitoring container resource usage via cgroups, providing visibility into CPU throttling, memory pressure, and resource contention between containers.

### Cgroup Collectors

#### Cgroup CPU Collector (`MetricTypeCgroupCPU`)
- Monitors CPU usage and throttling per container
- Reads from cgroup cpu and cpuacct controllers
- Detects CPU resource contention between containers
- Tracks throttling events to identify CPU limits being hit
- Supports both cgroup v1 and v2

#### Cgroup Memory Collector (`MetricTypeCgroupMemory`)
- Tracks memory usage, limits, and pressure
- Monitors for OOM events and memory failures
- Provides detailed memory breakdown (RSS, cache, swap)
- Identifies containers approaching memory limits
- Supports both cgroup v1 and v2

### Container Discovery

The agent automatically discovers containers through:
- Docker containers in `/sys/fs/cgroup/*/docker/`
- containerd containers in `/sys/fs/cgroup/*/containerd/`
- CRI-O containers in `/sys/fs/cgroup/*/crio/`
- Kubernetes pods in `/sys/fs/cgroup/kubepods.slice/`
- systemd-managed containers in `system.slice`

### Configuration

```go
// Set cgroup path in CollectionConfig
config := performance.CollectionConfig{
    HostCgroupPath: "/sys/fs/cgroup", // Default
}

// Or use environment variable
export HOST_CGROUP=/host/sys/fs/cgroup
```

### Deployment Requirements

For container monitoring in Kubernetes:

```yaml
spec:
  containers:
  - name: antimetal-agent
    securityContext:
      privileged: false  # Not required for cgroup monitoring
      readOnlyRootFilesystem: true
    volumeMounts:
    - name: cgroup
      mountPath: /host/sys/fs/cgroup
      readOnly: true
    env:
    - name: HOST_CGROUP
      value: /host/sys/fs/cgroup
  volumes:
  - name: cgroup
    hostPath:
      path: /sys/fs/cgroup
      type: Directory
```

### Metrics Collected

**CPU Metrics:**
- `UsageNanos`: Total CPU time consumed
- `NrPeriods`: Number of enforcement periods
- `NrThrottled`: Number of times throttled
- `ThrottledTime`: Total time throttled
- `CpuShares`: Relative CPU weight
- `CpuQuotaUs`: CPU quota per period
- `ThrottlePercent`: Percentage of periods throttled

**Memory Metrics:**
- `RSS`: Resident set size (active memory)
- `Cache`: Page cache memory
- `ActiveAnon`/`InactiveAnon`: Anonymous memory breakdown
- `ActiveFile`/`InactiveFile`: File cache breakdown
- `LimitBytes`: Memory limit
- `UsageBytes`: Current usage
- `FailCount`: Times limit was hit
- `OOMKillCount`: Number of OOM kills
- `UsagePercent`: Usage as percentage of limit

### Use Cases

1. **CPU Throttling Detection**: Identify containers hitting CPU limits
2. **Memory Pressure Analysis**: Detect containers approaching OOM
3. **Resource Contention**: Find "noisy neighbor" containers
4. **Right-sizing**: Optimize resource requests and limits
5. **Cost Optimization**: Identify over-provisioned containers

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