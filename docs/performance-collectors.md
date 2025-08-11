# Performance Collectors Development Guide

This guide covers the development, testing, and implementation patterns for performance collectors in the Antimetal Agent.

## Collector Interface Design

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

## Collector Implementation Patterns

### Constructor Pattern
All collectors must follow a consistent constructor pattern with:
1. Absolute path validation for HostProcPath and HostSysPath
2. Capability definition (SupportsOneShot, SupportsContinuous, RequiresRoot, etc.)
3. Pre-computed paths for efficiency

See `pkg/performance/collectors/load.go` for a reference implementation.

### Compile-Time Interface Checks
Every collector must include a compile-time interface check:
```go
// Compile-time interface check
var _ performance.Collector = (*NetworkCollector)(nil)
```

### Base Collector Pattern
```go
type BaseCollector struct {
    metricType   MetricType
    name         string
    logger       logr.Logger
    config       CollectionConfig
    capabilities CollectorCapabilities
}
```

### Capabilities System
```go
type CollectorCapabilities struct {
    SupportsOneShot    bool
    SupportsContinuous bool
    RequiresRoot       bool
    RequiresEBPF       bool
    MinKernelVersion   string
}
```

## Error Handling Strategy

Collectors must clearly distinguish between critical and optional data and should never panic:

- **Critical files**: Return error immediately if unavailable (e.g., /proc/loadavg for LoadCollector)
- **Optional files**: Log warning and continue with graceful degradation (e.g., /sys/class/net/* metadata)
- **Parse errors**: Handle based on field importance - critical fields cause errors, optional fields are logged
- **Panic prevention**: Collectors must use proper error handling instead of panicking. All errors should be returned to the caller

### Graceful Degradation
When optional data is unavailable, collectors should:
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

## Adding Collector to Registry

In order to use a collector, it has to be added to the registry so that the manager knows about it.
This is typically done in the init() function in the source file where the collector is implemented.

For PointCollectors, you need to transform them into ContinuousCollectors in order to add them to the registry.
There are two wrappers to do that depending on use case:
1. `ContinuousPointCollector`: wraps a PointCollector to call `Collect()` on an interval
2. `OnceContinuousCollector`: wraps a PointCollector where it calls `Collect()` once and caches the result

### Runtime Statistics Collectors
Use `PartialNewContinuousPointCollector` if the collector is collecting runtime statistics:

```go
func init() {
    performance.Register(performance.MetricTypeXXX, performance.PartialNewContinuousPointCollector(
        func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
            return NewXCollector(logger, config)
        },
    ))
}
```

### Hardware Information Collectors
Use `PartialOnceContinuousCollector` when collecting hardware information or if the collector needs to run just once:

```go
func init() {
    performance.Register(performance.MetricTypeXXX, performance.PartialNewOnceContinuousCollector(
        func(logger logr.Logger, config performance.CollectionConfig) (performance.PointCollector, error) {
            return NewXCollector(logger, config)
        },
    ))
}
```

## Performance Collector Testing Methodology

### Standardized Testing Approach
Performance collectors follow a comprehensive testing pattern:

#### 1. Test Structure
- Table-driven tests for multiple scenarios
- Temporary file system isolation using `t.TempDir()`
- Mock /proc and /sys filesystem files
- Reusable helper functions for common operations

#### 2. Core Testing Areas
- **Constructor validation**: Path handling, configuration validation
- **Data parsing**: Valid scenarios, malformed input, edge cases
- **Error handling**: Missing files, invalid data, graceful degradation
- **File system operations**: Different proc paths, access errors
- **Whitespace tolerance**: Leading/trailing whitespace handling

#### 3. Key Testing Patterns
- Helper functions for collector creation and validation
- Mock filesystem setup using `t.TempDir()`
- Table-driven test structure
- See examples in `pkg/performance/collectors/*_test.go` files

#### 4. Testing Requirements
- **Constructor tests**: Separate test function for constructor validation
- **Path validation**: Test absolute vs relative paths, empty paths
- **File interdependency**: Test behavior when related files unavailable
- **Return type validation**: Explicit type assertions with proper error messages
- **Graceful degradation**: Document and test critical vs optional files
- **Boundary conditions**: Test zero values, maximum values (e.g., max uint64)
- **Malformed input**: Test partial data, missing fields, corrupt formats
- **Special cases**: Test virtual interfaces, disabled devices, etc.

#### 5. Testing Principles
- Don't test static properties (Name, RequiresRoot)
- Don't test compile-time interface checks (e.g., `var _ performance.Collector = (*XCollector)(nil)`)
- Focus on parsing logic and error handling
- Use realistic test data from actual /proc files
- Test collectors in `collectors_test` package (external testing)
- Name test functions consistently: `TestXCollector_Constructor`, `TestXCollector_Collect`
- Use descriptive test names that explain the scenario
- Group related test scenarios in the same test function

#### 6. Documentation Standards
- **Type-level documentation**: Explain purpose, data sources, references
- **Method documentation**: Include format examples and error handling strategy
- **Inline documentation**: Explain complex parsing logic or non-obvious decisions
- **Reference links**: Include kernel documentation links where applicable

## Collector Development Workflow

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

## Common Collector Patterns

- **Reading Single Value Files**: Use `os.ReadFile()` with `strings.TrimSpace()`
- **Parsing Multi-Line Files**: Use `bufio.Scanner` with error checking
- **Handling Optional Metadata**: Read without failing, log at V(2) if missing
- See working examples in `pkg/performance/collectors/` directory

## Continuous Collector Pattern

When implementing collectors that support continuous collection (ContinuousCollector interface), follow this standardized pattern for proper lifecycle management:

### Key Components
1. **Two control mechanisms**:
   - **Context** (passed to Start): External lifecycle management from parent/app
   - **Stopped channel** (internal): Direct control via Stop() method

2. **Channel management**:
   - Data channel (`ch`): Created in Start(), closed in Stop()
   - Stopped channel (`stopped`): Created in Start(), closed in Stop()

### Implementation Pattern
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

### Why This Pattern?
1. **Dual control**: Responds to both external shutdown (context) and explicit Stop()
2. **Clean shutdown**: Stop() actually stops the goroutine and cleans up resources
3. **No orphaned goroutines**: Either mechanism ensures proper cleanup
4. **Consistent behavior**: All continuous collectors work the same way
5. **Prevents resource leaks**: Channels are properly closed

### Usage Scenarios
- **App shutdown**: Context cancellation stops all collectors automatically
- **Individual control**: Stop() specific collectors while others continue
- **Reconfiguration**: Stop(), reconfigure, Start() again
- **Debugging**: Temporarily disable specific collectors

This pattern ensures collectors integrate well with Kubernetes controllers and other lifecycle management systems while providing fine-grained control when needed.

## Example Collectors

For complete implementation examples, see:
- `pkg/performance/collectors/load.go` - Simple /proc/loadavg collector
- `pkg/performance/collectors/network.go` - Complex multi-file collector
- `pkg/performance/collectors/process.go` - Continuous collector implementation
- `pkg/performance/collectors/cpu_info.go` - Hardware information collector

## Additional Resources

- [Linux /proc Filesystem Documentation](https://www.kernel.org/doc/html/latest/filesystems/proc.html)
- [sysfs Documentation](https://www.kernel.org/doc/html/latest/filesystems/sysfs.html)
- [Go Testing Best Practices](https://go.dev/doc/tutorial/add-a-test)