# Test Configurations and Workloads

This directory contains test configurations and Kubernetes workloads used for benchmarking and testing the Antimetal System Agent.

## Directory Structure

```
config/test/
├── kind-topology-cluster.yaml          # Complete KIND cluster configuration
├── workloads/                          # Test workload definitions
│   ├── cgroup-test-workloads.yaml     # Comprehensive cgroup testing suite
│   ├── cpu-pinned-workload.yaml       # CPU pinning and NUMA affinity testing
│   └── init-container-workload.yaml   # Init containers and sidecar patterns
├── scenarios/                          # Test scenario configurations (future)
└── kustomization.yaml                  # Kustomize configuration (optional)
```

## Test Workloads

### kind-topology-cluster.yaml
Complete KIND cluster configuration for testing container-process-hardware topology discovery:
- Multi-node cluster with different node types (CPU-intensive, memory-intensive)
- Full host filesystem access for topology discovery
- Container runtime access for all major runtimes
- CPU manager and topology manager enabled for affinity testing
- Cgroup v2 support with proper mounts

**Quick Start:**
```bash
# Create KIND cluster
kind create cluster --config config/test/kind-topology-cluster.yaml

# Deploy the agent (from config/default)
kubectl apply -k config/default

# Deploy test workloads
kubectl apply -f config/test/workloads/
```

### Test Workload Files

#### cpu-stress-workload.yaml
CPU-intensive workloads for testing CPU monitoring and cpuset discovery:
- Standard CPU stress with configurable limits
- CPU-pinned workload for testing Guaranteed QoS and CPU affinity
- Validates cgroup CPU metrics and throttling detection

#### memory-stress-workload.yaml  
Memory-intensive workloads for testing memory monitoring:
- Memory stress with configurable consumption patterns
- Memory growth workload that gradually increases usage
- Tests memory limits, pressure, and OOM behavior

#### multi-container-workload.yaml
Multi-container pods for testing container relationship discovery:
- Pods with multiple containers sharing volumes
- Init containers for testing process hierarchy
- Sidecar patterns for relationship mapping

#### cgroup-test-workloads.yaml
Comprehensive cgroup testing workloads:
- CPU-intensive workloads with different resource limits
- Memory-intensive workloads with varying consumption patterns
- Mixed workloads to test concurrent collection
- Pods with different QoS classes (Guaranteed, Burstable, BestEffort)

**Usage:**
```bash
# Deploy all test workloads
kubectl apply -f config/test/workloads/

# Or deploy specific workloads
kubectl apply -f config/test/workloads/cpu-stress-workload.yaml
kubectl apply -f config/test/workloads/memory-stress-workload.yaml
```

## Test Scenarios

### Container Topology Testing
Tests the agent's ability to discover and map container-process-hardware relationships:
```bash
# Create cluster and deploy agent
kind create cluster --config config/test/kind-topology-cluster.yaml
kubectl apply -k config/default

# Deploy test workloads
kubectl apply -f config/test/workloads/
```

This creates a KIND cluster with:
- Multiple container runtimes (Docker, containerd)
- Various workload types (CPU-bound, memory-bound, I/O-bound)
- Different cgroup configurations (v1 and v2)
- Privileged and unprivileged containers

### Cgroup Collector Testing
Tests the agent's cgroup-based resource collection:
```bash
./scripts/test-cgroup-collectors.sh
```

Features tested:
- CPU usage and throttling metrics
- Memory usage, limits, and pressure
- Container discovery across runtimes
- Cgroup v1/v2 compatibility

## Performance Benchmarking

### Load Testing
Deploy high-load workloads to test agent performance:
```yaml
# Example: Deploy 100 replicas for load testing
kubectl scale deployment test-cpu-workload --replicas=100 -n antimetal-test
```

### Resource Consumption
Monitor agent resource usage during testing:
```bash
kubectl top pod -n antimetal-system -l app=antimetal-agent
```

## Adding New Test Workloads

When adding new test workloads:

1. **Naming Convention**: Use descriptive names that indicate the test purpose
   - Format: `<feature>-test-<type>.yaml`
   - Example: `profiler-test-workload.yaml`

2. **Documentation**: Include comments in YAML files explaining:
   - Test purpose
   - Expected behavior
   - Resource requirements
   - Dependencies

3. **Labels**: Use consistent labels for test identification:
   ```yaml
   metadata:
     labels:
       test.antimetal.io/type: "performance"
       test.antimetal.io/component: "cgroup-collector"
   ```

4. **Namespaces**: Use dedicated test namespaces:
   - `antimetal-test`: General testing
   - `antimetal-bench`: Performance benchmarking
   - `antimetal-chaos`: Chaos testing (future)

## Test Utilities

### Running the Agent with Topology Discovery
The main agent now includes full topology discovery capabilities with both hardware and runtime managers:
```bash
# Build the agent
make docker-build

# Load into KIND cluster
kind load docker-image antimetal/agent:latest --name antimetal-topology-test

# Deploy the agent with topology discovery enabled
kubectl apply -k config/default

# Watch the agent logs to see topology discovery in action
kubectl logs -f -n antimetal-system deployment/antimetal-agent
```

The agent will automatically:
- Discover all containers via cgroup inspection (every 30s by default)
- Map processes to containers using /proc/<pid>/cgroup
- Build hardware topology including CPUs, NUMA nodes, memory (every 5m by default)
- Create relationships between containers and hardware based on cpuset constraints

### Validation Scripts
Common validation commands:
```bash
# Check container discovery
kubectl exec -it <agent-pod> -- ls -la /host/sys/fs/cgroup/

# Verify cgroup version
kubectl exec -it <agent-pod> -- cat /host/sys/fs/cgroup/cgroup.controllers

# Monitor resource collection
kubectl logs -f <agent-pod> | grep -E "cgroup|container"
```

## CI/CD Integration

These test configurations are used in CI/CD pipelines:
- PR validation: Runs basic topology and cgroup tests
- Nightly builds: Comprehensive testing with all workload types
- Release testing: Full performance benchmarking suite

## Troubleshooting

### Common Issues

1. **KIND cluster creation fails**
   - Ensure Docker has sufficient resources (8GB RAM, 20GB disk)
   - Check Docker daemon is running with proper permissions

2. **Workloads not scheduling**
   - Verify node resources: `kubectl describe nodes`
   - Check for taints: `kubectl get nodes -o json | jq '.items[].spec.taints'`

3. **Agent can't access cgroups**
   - Ensure privileged mode is enabled
   - Verify volume mounts are correct
   - Check SELinux/AppArmor policies

## Future Enhancements

- [ ] eBPF profiler test workloads
- [ ] Multi-cluster topology testing
- [ ] Chaos engineering scenarios
- [ ] Performance regression detection
- [ ] Automated test result analysis