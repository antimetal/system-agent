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

The Antimetal Agent is a Kubernetes controller that connects infrastructure to the Antimetal platform for cloud resource management. It collects K8s resources, monitors system performance, and streams data via gRPC.

### Key Technologies
- **Go 1.24** with controller-runtime framework
- **Kubernetes** custom controller patterns
- **gRPC** for streaming data to intake service
- **BadgerDB** for embedded resource storage
- **Docker** with multi-arch support (linux/amd64, linux/arm64)
- **KIND** for local development and testing

## Architecture Overview

### Core Components

| Component | Path | Purpose |
|-----------|------|---------|
| **Kubernetes Controller** | `internal/kubernetes/agent/` | Watches resources, reconciliation, leader election |
| **Intake Worker** | `internal/intake/` | gRPC streaming, batching, retry logic |
| **Performance Monitoring** | `pkg/performance/` | System metrics from /proc and /sys |
| **Resource Store** | `pkg/resource/store/` | BadgerDB storage, RDF triplets, event subscriptions |
| **Cloud Providers** | `internal/kubernetes/cluster/` | EKS, KIND, extensible provider interface |

### Directory Structure
- `cmd/` - Application entry points
- `internal/` - Private application code (intake, kubernetes controller)
- `pkg/` - Public packages (aws, performance, resource store)
- `config/` - K8s manifests and Kustomize
- `ebpf/` - eBPF programs and build system

## Development Workflow

### Prerequisites
- **Docker** (rootless, containerd snapshotter enabled)
- **kubectl** for K8s operations
- **Go 1.24+** as specified in go.mod

### Common Commands

Run `make help` for the full list. Key commands:

| Category | Command | Purpose |
|----------|---------|---------|
| **Build** | `make build` | Build binary for current platform |
| | `make build-all` | Build for all platforms |
| | `make docker-build-all` | Build multi-arch Docker images |
| **Test** | `make test` | Run tests with coverage |
| | `make lint` | Run golangci-lint |
| | `make fmt` | Format Go code |
| | `make fmt.clang` | Format C/C++/eBPF code |
| **Generate** | `make generate` | Generate K8s manifests |
| | `make gen-license-headers` | **ALWAYS run before committing** |
| **KIND** | `make cluster` | Create local KIND cluster |
| | `make build-and-load-image` | Quick rebuild and deploy |
| | `make destroy-cluster` | Delete KIND cluster |

### Key Development Patterns

#### Code Generation
Always run `make generate` after:
- Modifying kubebuilder annotations (`+kubebuilder:rbac`)
- Changing CRD definitions
- Updating webhook configurations

#### License Headers
- **ALWAYS** run `make gen-license-headers` before committing
- **ALL** .go files must have the PolyForm Shield license header located in @tools/license_check/license_header.txt

#### Testing Philosophy
- Use standard Go testing framework
- Tests located alongside implementation files
- Table-driven tests for comprehensive coverage
- Mock external dependencies (gRPC, AWS, K8s)

#### Git Commits and PRs
- **ALWAYS** run `make lint-fix` before creating a commit
- **ALWAYS** use the `commit-author` agent for creating commit messages, reviewing commits, or generating PR descriptions
- The agent ensures compliance with project commit conventions and formatting standards

#### Linux Systems and eBPF Development
- **ALWAYS** use the `linux-systems-expert` agent when developing or debugging Linux system collectors, eBPF programs, or /proc or /sys parsers
- The agent has deep expertise in kernel interfaces, CO-RE/BTF, performance monitoring, and cross-kernel compatibility

## Performance Collectors

The Antimetal Agent includes a comprehensive performance monitoring system that collects system metrics from /proc and /sys filesystems. Collectors follow a dual-interface pattern (PointCollector for one-shot, ContinuousCollector for streaming) with standardized error handling and testing methodologies.

**Key concepts:**
- Constructor pattern with path validation and capabilities
- Registry system for collector management
- Graceful degradation for optional data
- Comprehensive testing with mock filesystems

For detailed performance collector development including implementation patterns, testing methodology, continuous collectors, and examples, see **[docs/performance-collectors.md](docs/performance-collectors.md)**.

### Container Metrics Collection

The agent supports container-level metrics via cgroup collectors:

- **Automatic version detection**: Supports both cgroup v1 and v2
- **Multi-runtime support**: Docker, containerd, CRI-O, Podman
- **Zero-configuration**: Auto-discovers containers without runtime APIs
- **Graceful degradation**: Handles missing files and permissions

For implementation details, see the system-agent wiki documentation on cgroup collectors.

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

#### Integration Test Environment Assumptions
Integration tests (files with `//go:build integration` tag) should always assume:
- **Linux environment**: Never check `runtime.GOOS` - integration tests only run on Linux
- **Proper permissions**: Never check `os.Geteuid()` - assume tests have required permissions (root, CAP_BPF, etc.)
- **Required capabilities**: Use `require.NoError()` for permission-related operations rather than skipping
- **Kernel features**: Check kernel version for feature availability, but don't skip for OS type

Example of correct integration test setup:
```go
//go:build integration

func TestEBPFFeature(t *testing.T) {
    // NO: if runtime.GOOS != "linux" { t.Skip() }
    // NO: if os.Geteuid() != 0 { t.Skip() }
    
    // YES: Assume Linux and proper permissions
    err := rlimit.RemoveMemlock()
    require.NoError(t, err, "Failed to remove memlock - integration tests require proper permissions")
    
    // YES: Check kernel version for feature support
    kernel, _ := GetCurrentVersion()
    if !kernel.IsAtLeast(5, 8) {
        t.Skip("Feature requires kernel 5.8+")
    }
}
```

### Performance Testing
- **Benchmarks** for critical paths
- **Load testing** with realistic data volumes
- **Memory profiling** for optimization

## Development Notes

### Code Style

#### Golang
- **Early returns** to reduce nesting
- **Functional patterns** where applicable
- **Concise implementations** without unnecessary comments
- **Error wrapping** with context
- **ALWAYS use `any` instead of `interface{}`**
- **ALWAYS format with `make fmt`** before committing

#### C/C++ and eBPF Code
- **Google C++ Style** as base (via `.clang-format`)
- **Pointer alignment**: `Type* variable` (pointer attached to type)
- **Indent width**: 2 spaces
- **Column limit**: 80 characters
- **Format with `make fmt.clang`** before committing
- **Include ordering**: vmlinux.h first, then system headers, then local headers

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

The Antimetal Agent supports eBPF-based collectors for deep kernel observability using CO-RE (Compile Once - Run Everywhere) technology for portability across kernel versions 4.18+.

**Key commands:**
- `make build-ebpf` - Build eBPF programs with CO-RE support
- `make generate-ebpf-bindings` - Generate Go bindings from eBPF C code

For detailed eBPF development guidance including CO-RE support, adding new programs, troubleshooting, and best practices, see **[docs/ebpf-development.md](docs/ebpf-development.md)**.

## Wiki Documentation Integration

The project maintains comprehensive documentation in a GitHub Wiki, available as a git submodule in the `.wiki/` directory.

### Wiki-Keeper Agent

**IMPORTANT**: Always use the `wiki-keeper` agent for ALL wiki operations via the Task tool. This specialized agent:
- Dynamically reads `.wiki/CLAUDE.md` for current conventions
- Searches, analyzes, updates, and creates documentation  
- Returns concise summaries without loading full content
- Reduces context usage significantly

#### When to Use Wiki-Keeper

Use the wiki-keeper agent for:
- **Searching documentation** before implementing features
- **Checking if features are documented**
- **Updating documentation** after code changes
- **Creating new documentation** for new features
- **Analyzing documentation completeness**
- **Finding architecture decisions and design patterns**

#### Example Usage

```python
Task(
    subagent_type="general-purpose",
    description="Search wiki for [topic]",
    prompt="""
    You are the wiki-keeper agent. Your instructions are in .claude/agents/wiki-keeper.md.
    Work from the .wiki/ directory.
    IMPORTANT: First read .wiki/CLAUDE.md for wiki-specific conventions.
    
    Task: [Your specific task here]
    Context: [Any relevant context about code changes]
    Return: [What you need back - summaries, locations, gaps, etc.]
    """
)
```

The wiki-keeper returns structured responses with file paths, brief summaries, and specific recommendations.

#### Best Practices

1. **Always use wiki-keeper** instead of reading wiki files directly
2. **Provide context** about code changes when requesting updates
3. **Trust the agent's summaries** - avoid requesting full content

### Git Operations

```bash
# ALWAYS pull latest wiki changes (not pinned to specific commit)
git submodule update --remote --merge .wiki

# Initialize wiki if cloning fresh
git submodule init && git submodule update --remote .wiki

# After wiki-keeper makes documentation changes
cd .wiki && git add -A && git commit -m "docs: [description]" && git push
cd ..

# IMPORTANT: Avoid committing the .wiki submodule reference
# If you see .wiki as modified in git status, DO NOT stage it
git add .  # Stage your changes
git reset .wiki  # Unstage the submodule reference
git commit -m "your changes"  # Commit without updating wiki reference
```

**Important**: The wiki tracks the `master` branch and should always use the latest documentation, not a pinned commit. Use `--remote` flag to get latest changes.

### Code References

When referencing wiki documentation in code comments:
```go
// See wiki: Cgroup/Memory-Collector.md for algorithm details
```
