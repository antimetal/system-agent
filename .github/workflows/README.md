# GitHub Actions Workflows

This directory contains GitHub Actions workflows for the Antimetal System Agent project.

## Current Workflows

### 1. Integration Tests (`integration-tests.yml`)
**Primary testing workflow** that runs both unit and integration tests across multiple Linux kernel versions using VMs.
- **Triggers**: 
  - Pull requests modifying code in `pkg/`, `internal/`, `cmd/`, `ebpf/`, or `test/`
  - Pushes to main branch with code changes
  - Manual workflow dispatch
- **Kernel versions tested**: 5.10, 5.15, 6.1, 6.6
- **Test coverage**:
  - Unit tests (with `//go:build !integration` tag)
  - Integration tests (with `//go:build integration` tag)
  - Hardware collector verification
  - eBPF program compilation and loading
  - Real /proc and /sys filesystem interaction
- **VM Management**: Uses Cilium's LVH (Little VM Helper) for VM orchestration

### 2. Go Tests (`test.yml`)
Basic Go test workflow for quick unit test validation.
- **Triggers**: Pull requests and pushes to main
- **Actions**: Runs standard Go tests without integration tests

### 3. Linear Sync (`linear-sync.yml`)
Synchronizes GitHub issues with Linear tasks.
- **Triggers**: When issues are opened, closed, or reopened
- **Actions**:
  - Creates Linear issues when GitHub issues are opened
  - Updates Linear issue status when GitHub issues are closed/reopened

### 4. Claude Code (`claude.yml`)
Responds to mentions of Claude in issue comments.
- **Triggers**: Issue comments containing "@claude"
- **Actions**: Processes requests and provides assistance

## Testing Strategy

### Build Tags
The codebase uses Go build tags to separate unit and integration tests:
- `//go:build !integration` - Unit tests that use mocked filesystems
- `//go:build integration` - Integration tests requiring real Linux filesystems

### Local Testing
```bash
# Run unit tests only
make test

# Run integration tests (requires Linux)
make test-integration

# Run all tests
make test-all
```

### CI/CD Testing
The `integration-tests.yml` workflow:
1. Builds test artifacts and eBPF programs with CO-RE support
2. Creates VMs with different kernel versions
3. Runs both unit and integration tests in each VM
4. Collects and reports results across all kernels
5. Generates a compatibility matrix showing test results per kernel

## Deprecated Workflows

The following workflows have been consolidated into `integration-tests.yml`:

- **hardware-collector-tests.yml** → Functionality moved to `integration-tests.yml`
  - Hardware collector tests now run in VMs across all kernel versions
  - Real hardware data collection integrated into VM test suite
  
- **ebpf-vm-verifier-tests.yml** → Functionality moved to `integration-tests.yml`
  - eBPF programs built with CO-RE support in build phase
  - BTF verification performed across all kernel versions
  - BPF program loading tested within VM environments

## Migration Guide

### From hardware-collector-tests.yml
Hardware collector tests are now part of the integration test suite:
- Tests run automatically in VMs across kernel versions 5.10-6.6
- Hardware detection and data collection validated in each VM
- Collector benchmark tool tested as part of integration suite

### From ebpf-vm-verifier-tests.yml
eBPF testing is now integrated into the main test workflow:
- eBPF programs compiled once with CO-RE support
- BTF sections verified during build
- Programs loaded and tested in each VM using bpftool
- Kernel compatibility automatically validated

## Adding New Tests

### Unit Tests
1. Create `*_test.go` files alongside implementation
2. Add `//go:build !integration` tag at the top
3. Use mocked filesystems and dependencies
4. Example:
```go
//go:build !integration

package collectors_test

func TestLoadCollector_MockedData(t *testing.T) {
    // Test with mocked /proc/loadavg
}
```

### Integration Tests
1. Create `*_integration_test.go` files
2. Add `//go:build integration` tag at the top
3. Use real Linux filesystems and kernel features
4. Example:
```go
//go:build integration

package collectors_test

func TestLoadCollector_RealKernel(t *testing.T) {
    // Test with actual /proc/loadavg
}
```

## Testing Workflows Locally

### Prerequisites

1. Install `act` (runs GitHub Actions locally):
   ```bash
   # macOS
   brew install act

   # Linux
   curl https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash
   ```

2. Install Docker (required by act)

### Setup

1. Create a `.secrets` file in the repository root:
   ```bash
   # Copy from example
   cp .secrets.example .secrets

   # Edit and add your tokens
   vim .secrets
   ```

   Required secrets:
   - `LINEAR_API_TOKEN`: Your Linear API token
   - `ANTHROPIC_API_KEY`: Your Anthropic API key for Claude
   - `GITHUB_TOKEN`: GitHub personal access token (if needed)

2. Set repository variables:
   ```bash
   # Create .vars file for repository variables
   echo "LINEAR_TEAM_ID=your_team_id" > .vars
   ```

### Testing Individual Workflows

#### Test Integration Tests Workflow
```bash
# Test the build phase
act push -j build-artifacts \
  --secret-file .secrets

# Test with specific kernel
act push -j test-on-vms \
  --matrix kernel:6.1 \
  --secret-file .secrets
```

#### Test Linear Sync Workflow

```bash
# Test issue creation
act issues -j create-linear-issue \
  --secret-file .secrets \
  --var-file .vars \
  -e test-events/issue-opened.json

# Test with real issue data
act issues -j create-linear-issue \
  --secret-file .secrets \
  --var-file .vars \
  --eventpath - << 'EOF'
{
  "action": "opened",
  "issue": {
    "number": 123,
    "title": "Test Issue",
    "body": "This is a test issue body",
    "html_url": "https://github.com/antimetal/system-agent/issues/123"
  }
}
EOF
```

#### Test Claude Code (Issue Comments)

```bash
# Test Claude mention response
act issue_comment -j respond \
  --secret-file .secrets \
  -e test-events/issue-comment.json
```

### Using Makefile Targets

The project includes convenient Makefile targets for testing:

```bash
# List all workflows and jobs
make test-actions

# Test specific workflows
make test-linear-sync

# Dry run (see what would execute)
make test-actions-dry
```

### Creating Test Events

Create test event JSON files in `test-events/` directory:

```json
// test-events/issue-opened.json
{
  "action": "opened",
  "issue": {
    "number": 1,
    "title": "Sample Issue",
    "body": "Issue description",
    "html_url": "https://github.com/antimetal/system-agent/issues/1",
    "user": {
      "login": "testuser"
    }
  },
  "repository": {
    "name": "system-agent",
    "owner": {
      "login": "antimetal"
    }
  }
}
```

### Debugging Tips

1. **Verbose Output**: Add `-v` flag for detailed logs
   ```bash
   act issues -j create-linear-issue -v
   ```

2. **Container Shell**: Debug inside the action container
   ```bash
   act issues -j create-linear-issue --container-daemon-socket -
   ```

3. **List Available Events**: See what events act can simulate
   ```bash
   act -l
   ```

4. **Use Specific Docker Image**: Override default image
   ```bash
   act -P ubuntu-latest=ghcr.io/catthehacker/ubuntu:act-latest
   ```

## Testing Without `act`

### Direct API Testing

Test Linear API integration:
```bash
# Test Linear GraphQL API
curl -X POST \
  -H "Authorization: YOUR_LINEAR_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "mutation { issueCreate(input: { title: \"Test\", teamId: \"TEAM_ID\" }) { issue { identifier } } }"
  }' \
  https://api.linear.app/graphql | jq
```

### Branch Testing

1. Create a test branch:
   ```bash
   git checkout -b test/workflow-changes
   git push origin test/workflow-changes
   ```

2. Temporarily modify workflow triggers:
   ```yaml
   on:
     pull_request:
       branches: [main, test/**]
   ```

3. Create test issues/PRs against the test branch

## Common Issues and Solutions

### Issue: Secrets not loading
**Solution**: Ensure `.secrets` file is in repository root and formatted correctly:
```
LINEAR_API_TOKEN=lin_api_xxxxx
ANTHROPIC_API_KEY=sk-ant-xxxxx
```

### Issue: Docker not running
**Solution**: Start Docker Desktop or Docker daemon before running act

### Issue: Workflow syntax errors
**Solution**: Validate workflow syntax:
```bash
# Install workflow parser
npm install -g @actions/workflow-parser

# Validate workflow
workflow-parser .github/workflows/linear-sync.yml
```

### Issue: Rate limiting
**Solution**: Add delays between API calls or use test-specific rate limits

## Best Practices

1. **Always test locally first** using `act` before pushing changes
2. **Use test branches** for integration testing
3. **Keep test events updated** in `test-events/` directory
4. **Never commit secrets** - use `.secrets` file (gitignored)
5. **Document workflow changes** in commit messages
6. **Test error scenarios** not just happy paths
7. **Use build tags** to separate unit and integration tests
8. **Run integration tests in VMs** for kernel-specific features

## Resources

- [act Documentation](https://github.com/nektos/act)
- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Linear API Documentation](https://developers.linear.app/docs/graphql/working-with-the-graphql-api)
- [Anthropic API Documentation](https://docs.anthropic.com/claude/reference/getting-started-with-the-api)
- [LVH (Little VM Helper)](https://github.com/cilium/little-vm-helper)
- [Go Build Tags](https://pkg.go.dev/go/build#hdr-Build_Constraints)