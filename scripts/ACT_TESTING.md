# Testing PRs with Act in Lima VMs

## Overview

Instead of reimplementing the CI workflow, we can use `act` to run GitHub Actions locally. This approach:
- Uses the exact same workflow defined in `.github/workflows/integration-tests.yml`
- Leverages LVH (Little VM Helper) that's already configured in the workflow
- Maintains consistency between local and CI testing

## Architecture

```
Host Machine (macOS)
    └── Lima VM (Ubuntu 22.04)
         ├── Docker (for act containers)
         ├── act (GitHub Actions runner)
         └── GitHub Workflow
              └── LVH (runs tests in nested VMs)
                   └── Different kernel versions
```

## Quick Start

### 1. Manual Setup in Existing Lima VM

```bash
# Connect to your Lima VM
limactl shell pr106-test

# Install Docker (if not already installed)
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER
newgrp docker

# Install act
curl https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash

# Navigate to repository
cd ~/system-agent

# Run the integration tests workflow
act -W .github/workflows/integration-tests.yml \
    --container-architecture linux/amd64 \
    -P ubuntu-latest=catthehacker/ubuntu:act-latest
```

### 2. Automated Setup with Script

```bash
# Test PR 106
./scripts/test-pr-with-act.sh 106

# Test with preserved VM for debugging
./scripts/test-pr-with-act.sh 106 --keep-vm

# Test current branch
./scripts/test-pr-with-act.sh --keep-vm
```

## How It Works

1. **Lima VM Creation**: Creates an Ubuntu VM with Docker and act pre-installed
2. **Repository Setup**: Clones the system-agent repo and checks out the PR
3. **Act Execution**: Runs the GitHub workflow using act
4. **LVH Integration**: The workflow uses LVH to test across different kernels
5. **Nested Virtualization**: LVH creates nested VMs inside the Docker containers

## Advantages Over Manual Testing

- **Exact CI Replication**: Uses the same workflow file as GitHub Actions
- **No Maintenance**: Workflow changes automatically apply to local testing
- **Kernel Matrix Testing**: Tests across multiple kernel versions via LVH
- **Container Isolation**: Each job runs in isolated containers

## Workflow Jobs Executed

The `integration-tests.yml` workflow runs:

1. **unit-tests**: Basic Go unit tests
2. **build-artifacts**: Builds eBPF programs and packages artifacts
3. **integration-tests-vm**: Runs tests in LVH VMs with different kernels:
   - Kernel 5.4 (Ubuntu 20.04 LTS)
   - Kernel 5.10
   - Kernel 5.15 (Ubuntu 22.04 LTS)
   - Kernel 6.1 (LTS)
   - Kernel 6.6 (Latest LTS)

## Act Configuration

### Custom Events

To test specific workflow triggers:

```bash
# Simulate a pull request event
echo '{"pull_request": {"number": 106}}' > /tmp/event.json
act pull_request -e /tmp/event.json

# Simulate a push to main
act push --ref refs/heads/main
```

### Running Specific Jobs

```bash
# Run only unit tests
act -j unit-tests

# Run only integration tests
act -j integration-tests-vm

# Run with specific kernel matrix
act -j integration-tests-vm --matrix kernel:6.1-20250616.013250
```

### Debugging

```bash
# Run with verbose output
act --verbose

# Keep containers after failure
act --reuse

# Shell into failed container
act --container-shell
```

## Troubleshooting

### Docker Permission Issues
```bash
# Fix Docker permissions in Lima VM
sudo usermod -aG docker $USER
newgrp docker
```

### Nested Virtualization
LVH requires nested virtualization. Ensure your Lima VM is configured with:
```yaml
vmType: "qemu"  # or "vz" on macOS 13+
```

### Resource Constraints
The workflow requires significant resources:
- Minimum 4 CPUs
- Minimum 8GB RAM
- 50GB disk space

### Act Image Issues
If act images are outdated:
```bash
# Pull latest act images
docker pull catthehacker/ubuntu:act-latest
docker pull catthehacker/ubuntu:act-22.04
```

## Comparison with Direct Testing

| Aspect | Act Approach | Direct Script Approach |
|--------|--------------|------------------------|
| Maintenance | Low (uses existing workflow) | High (separate scripts) |
| CI Parity | Exact match | Approximate |
| Complexity | Moderate (act + Docker) | High (custom orchestration) |
| Debugging | Container-based | Direct VM access |
| Speed | Slower (container overhead) | Faster (direct execution) |
| Kernel Testing | Via LVH (automated) | Manual VM setup |

## Best Practices

1. **Use `--keep-vm`** during development to avoid VM recreation
2. **Monitor resources** - act + LVH can be resource-intensive
3. **Cache Docker images** to speed up subsequent runs
4. **Use specific job targeting** when iterating on specific tests
5. **Enable verbose mode** for troubleshooting workflow issues

## Example Workflows

### Development Iteration
```bash
# Initial test with preserved VM
./scripts/test-pr-with-act.sh 106 --keep-vm

# Make changes locally
# ...

# Re-run tests in same VM
limactl shell act-test-106
cd ~/system-agent
git pull  # or sync changes
act -W .github/workflows/integration-tests.yml --reuse
```

### CI Validation
```bash
# Full CI simulation
./scripts/test-pr-with-act.sh 106

# Check results
echo "Tests passed! PR is ready for CI"
```

### Debugging Failed Tests
```bash
# Run with debugging enabled
limactl shell act-test-106
cd ~/system-agent
act -W .github/workflows/integration-tests.yml \
    --verbose \
    --container-shell \
    -j integration-tests-vm
```

## Conclusion

Using act provides the most accurate local representation of the CI environment while leveraging the existing workflow definitions. This approach eliminates the need to maintain separate testing scripts and ensures consistency between local and CI testing.