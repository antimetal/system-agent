# GitHub Actions Workflows

This directory contains GitHub Actions workflows for the Antimetal System Agent project.

## Workflows

### 1. Linear Sync (`linear-sync.yml`)
Synchronizes GitHub issues with Linear tasks.
- **Triggers**: When issues are opened, closed, or reopened
- **Actions**:
  - Creates Linear issues when GitHub issues are opened
  - Updates Linear issue status when GitHub issues are closed/reopened

### 2. Claude Code (`claude.yml`)
Responds to mentions of Claude in issue comments.
- **Triggers**: Issue comments containing "@claude"
- **Actions**: Processes requests and provides assistance

### 3. eBPF VM Verifier Tests (`ebpf-vm-verifier-tests.yml`)
Tests eBPF programs across multiple kernel versions using VMs to ensure kernel verifier compatibility.
- **Triggers**:
  - Pull requests modifying eBPF code
  - Pushes to main branch with eBPF changes
  - Manual workflow dispatch
- **Actions**:
  - Builds eBPF programs with CO-RE support
  - Tests loading on kernels 5.10, 5.15, 6.1, and 6.6
  - Uses Cilium's LVH (Little VM Helper) for VM management

### 4. Hardware Collector Real Hardware Tests (`hardware-collector-tests.yml`)
Tests hardware collectors on real hardware configurations.
- **Triggers**: Changes to hardware-related collectors
- **Actions**: Validates hardware detection and data collection

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

## Resources

- [act Documentation](https://github.com/nektos/act)
- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Linear API Documentation](https://developers.linear.app/docs/graphql/working-with-the-graphql-api)
- [Anthropic API Documentation](https://docs.anthropic.com/claude/reference/getting-started-with-the-api)
