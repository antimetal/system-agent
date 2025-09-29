# Test Configurations and Workloads

This directory contains Kustomize configurations to deploy and test the System Agent.

## Directory Structure

```
config/
├── cluster.yaml                         # KIND cluster configuration
├── test/
│   |── workloads/                       # Test workload definitions
├── agent/                               # System agent deployment
├── rbac/                                # RBAC configurations
└── default/                             # Full deployment (agent, test workloads, etc.)
```

**Usage:**
```bash
# Create test cluster
make cluster

# Deploy all test workloads
make deploy
```

## Test Scenarios

### Cgroup Collector Testing
Tests the agent's cgroup-based resource collection:
```bash
./scripts/test-cgroup-collectors.sh
```

## Performance Benchmarking

### Load Testing
Deploy high-load workloads to test agent performance:
```yaml
# Example: Deploy 100 replicas for load testing
kubectl scale deployment test-cpu-workload --replicas=100 -n antimetal-test
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

### Common Issues

1. **KIND cluster creation fails**
   - Ensure Docker has sufficient resources (8GB RAM, 20GB disk)
   - Check Docker daemon is running with proper permissions

2. **Agent can't access cgroups**
   - Ensure privileged mode is enabled
   - Verify volume mounts are correct
   - Check SELinux/AppArmor policies
