# Testing Cgroup Collectors in KIND

This guide provides instructions for testing the cgroup CPU and memory collectors in a KIND cluster.

## Quick Start

Run the complete test suite:
```bash
./test/test-cgroup-collectors.sh all
```

## Test Components

### 1. Test Workloads (`cgroup-test-workloads.yaml`)

The test suite deploys several workloads to exercise different aspects of the collectors:

- **cpu-stress-test**: Generates CPU load exceeding limits to trigger throttling
- **memory-stress-test**: Uses significant memory approaching the limit
- **multi-container-app**: 5 replicas for testing container discovery
- **bursty-workload**: Alternates between busy and idle states
- **memory-leak-test**: Gradually increases memory usage until hitting limit

### 2. Test Script (`test-cgroup-collectors.sh`)

The script provides multiple test phases:

```bash
# Individual test phases
./test/test-cgroup-collectors.sh prereq    # Check prerequisites
./test/test-cgroup-collectors.sh deploy     # Build and deploy agent
./test/test-cgroup-collectors.sh workloads  # Deploy test workloads
./test/test-cgroup-collectors.sh verify     # Verify cgroup mounts
./test/test-cgroup-collectors.sh monitor    # Monitor collector logs
./test/test-cgroup-collectors.sh metrics    # Check container metrics
./test/test-cgroup-collectors.sh stress     # Run stress test
./test/test-cgroup-collectors.sh verbose    # Enable verbose logging
./test/test-cgroup-collectors.sh cleanup    # Remove test resources
```

## Manual Testing Steps

### 1. Build and Deploy

```bash
# Ensure KIND cluster exists
make cluster

# Build and deploy agent with cgroup support
make generate
make docker-build
make load-image
make deploy
```

### 2. Deploy Test Workloads

```bash
kubectl apply -f test/cgroup-test-workloads.yaml
```

### 3. Monitor Collectors

Watch agent logs for cgroup activity:
```bash
# Check cgroup version detection
kubectl logs -n antimetal-system deployment/agent -f | grep -i cgroup

# Monitor for throttling
kubectl logs -n antimetal-system deployment/agent -f | grep -i throttl

# Check memory statistics
kubectl logs -n antimetal-system deployment/agent -f | grep -i memory
```

### 4. Verify Inside Pod

```bash
# Get agent pod name
POD=$(kubectl get pods -n antimetal-system -l app=agent -o jsonpath='{.items[0].metadata.name}')

# Check cgroup mounts
kubectl exec -n antimetal-system $POD -- ls -la /host/cgroup/

# Check cgroup version
kubectl exec -n antimetal-system $POD -- cat /host/cgroup/cgroup.controllers 2>/dev/null || echo "Cgroup v1"

# Find container cgroups
kubectl exec -n antimetal-system $POD -- find /host/cgroup -name '*docker*' -type d | head -10
```

### 5. Verify Metrics Collection

Check if collectors are finding containers:
```bash
# Enable verbose logging
kubectl set env deployment/agent -n antimetal-system VERBOSITY=2

# Wait for restart
kubectl rollout status deployment/agent -n antimetal-system

# Check detailed logs
kubectl logs -n antimetal-system deployment/agent --tail=1000 | grep -E "discovered|container|cgroup"
```

## Expected Results

### Success Indicators

✅ **Cgroup Version Detection**
```
"Detected cgroup version" version=2
```

✅ **Container Discovery**
```
"Discovered containers" count=10 runtime=docker
```

✅ **CPU Throttling Detection**
```
ContainerID: abc123def456
NrThrottled: 50
ThrottlePercent: 5.0
```

✅ **Memory Usage Tracking**
```
ContainerID: abc123def456
UsageBytes: 450000000
LimitBytes: 536870912
UsagePercent: 83.8
```

### Common Issues

1. **No cgroup version detected**
   - Check HOST_CGROUP environment variable is set
   - Verify /sys/fs/cgroup is mounted in container

2. **No containers discovered**
   - Ensure test workloads are running
   - Check cgroup mount permissions

3. **No metrics collected**
   - Enable verbose logging to see detailed errors
   - Check if collector is registered properly

## Cleanup

Remove all test resources:
```bash
./test/test-cgroup-collectors.sh cleanup
```

Or manually:
```bash
kubectl delete -f test/cgroup-test-workloads.yaml
kubectl set env deployment/agent -n antimetal-system VERBOSITY-
```

## Performance Validation

Monitor the agent's own resource usage during collection:
```bash
kubectl top pod -n antimetal-system
```

Expected performance:
- Collection time: <100ms for 10 containers
- CPU usage: <5% increase during collection
- Memory usage: <10MB increase for caching

## Debugging Tips

1. **Check collector registration**:
   ```bash
   kubectl logs -n antimetal-system deployment/agent | grep "Registered.*cgroup"
   ```

2. **Verify environment variables**:
   ```bash
   kubectl describe deployment agent -n antimetal-system | grep HOST_CGROUP
   ```

3. **Test file access**:
   ```bash
   POD=$(kubectl get pods -n antimetal-system -l app=agent -o jsonpath='{.items[0].metadata.name}')
   kubectl exec -n antimetal-system $POD -- cat /host/cgroup/cpu/kubepods/cpu.stat
   ```

4. **Check for errors**:
   ```bash
   kubectl logs -n antimetal-system deployment/agent | grep -i error | grep -i cgroup
   ```