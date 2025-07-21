# Testing GitHub Actions Locally with Act

This guide explains how to test GitHub Actions workflows locally without pushing to GitHub using `act` by nektos.

## Installation

### macOS (recommended)
```bash
brew install act
```

### Linux
```bash
curl -sSf https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash
```

### Windows
```bash
scoop install act
# or
winget install nektos.act
```

## Prerequisites

- **Docker** must be installed and running
- You must be in a Git repository with `.github/workflows/` directory

## Basic Usage

### List available workflows and jobs
```bash
# List all actions for all events
act -l

# Example output for this project:
# Stage  Job ID          Job name                        Workflow name          Workflow file                      Events           
# 0      claude-review   claude-review                   Claude Code Review     claude-code-review.yml             pull_request     
# 0      claude          claude                          Claude Code            claude.yml                         issue_comment,pull_request_review_comment,issues,pull_request_review
# 0      create-linear-issue create-linear-issue         Linear Sync            linear-sync.yml                    issues           
# 0      update-linear-issue update-linear-issue         Linear Sync            linear-sync.yml                    issues           
```

### Run workflows

```bash
# Run the default (push) event
act

# Run a specific event (e.g., pull_request)
act pull_request

# Run a specific job
act -j claude-review

# Dry run (show what would be executed without running)
act -n

# Verbose output for debugging
act -v
```

## Testing Specific Workflows in This Project

### 1. Testing Claude Code Review Workflow
```bash
# Simulate a pull request event
act pull_request -j claude-review

# With secrets (create .secrets file first)
act pull_request -j claude-review --secret-file .secrets
```

### 2. Testing Claude Code Workflow
```bash
# Simulate an issue comment event
act issue_comment -j claude

# Simulate a pull request review comment
act pull_request_review_comment -j claude
```

### 3. Testing Linear Sync Workflow
```bash
# Simulate issue opened event
act issues -j create-linear-issue --eventpath test-events/issue-opened.json

# Simulate issue closed event
act issues -j update-linear-issue --eventpath test-events/issue-closed.json
```

## Configuration

### 1. Create `.actrc` file for default settings
```bash
# .actrc
-P ubuntu-latest=catthehacker/ubuntu:act-latest
-P ubuntu-22.04=catthehacker/ubuntu:act-22.04
-P ubuntu-20.04=catthehacker/ubuntu:act-20.04
--container-architecture linux/amd64
```

### 2. Create `.secrets` file for API keys
```bash
# .secrets (add to .gitignore!)
ANTHROPIC_API_KEY=your-api-key-here
LINEAR_API_TOKEN=your-linear-token-here
```

### 3. Create test event payloads
```bash
mkdir -p test-events

# Example: test-events/issue-opened.json
cat > test-events/issue-opened.json << 'EOF'
{
  "action": "opened",
  "issue": {
    "number": 1,
    "title": "Test Issue",
    "body": "This is a test issue body",
    "html_url": "https://github.com/antimetal/system-agent/issues/1"
  }
}
EOF
```

## Tips for Testing

### 1. Use Medium-sized Docker Images
When first running `act`, choose the medium-sized image (~500MB) for faster downloads and sufficient functionality.

### 2. Test Individual Jobs
Instead of running entire workflows, test specific jobs:
```bash
act -j job-name
```

### 3. Use Environment Variables
```bash
# Pass environment variables
act -e MY_VAR=value

# Use .env file
act --env-file .env.test
```

### 4. Debug with Verbose Output
```bash
act -v pull_request
```

### 5. Skip Specific Steps
Use conditional expressions in your workflow:
```yaml
- name: Deploy
  if: ${{ !env.ACT }}  # Skip when running locally with act
  run: ./deploy.sh
```

## Limitations

1. **Services**: Docker services defined in workflows are not fully supported
2. **Workflow dispatch inputs**: Limited support for workflow_dispatch events
3. **GitHub-specific features**: Some GitHub-specific APIs and contexts may not work
4. **Performance**: Local runs may be slower than GitHub's hosted runners

## Example Commands for This Project

```bash
# Test Claude code review on a simulated PR
act pull_request -j claude-review --secret-file .secrets

# Test Linear sync when an issue is opened
act issues -j create-linear-issue --secret LINEAR_API_TOKEN=$LINEAR_TOKEN --var LINEAR_TEAM_ID=team-123

# Dry run to see what would execute
act -n -l

# Run with custom Docker image
act -P ubuntu-latest=myimage:latest
```

## Adding to Makefile

Consider adding these commands to your Makefile:

```makefile
.PHONY: test-actions
test-actions: ## Test GitHub Actions locally with act
	@command -v act >/dev/null 2>&1 || { echo "act is not installed. Run: brew install act"; exit 1; }
	act -l

.PHONY: test-claude-review
test-claude-review: ## Test Claude code review workflow
	act pull_request -j claude-review --secret-file .secrets

.PHONY: test-linear-sync
test-linear-sync: ## Test Linear sync workflow
	act issues -j create-linear-issue --secret-file .secrets
```

## Best Practices

1. **Keep secrets local**: Never commit `.secrets` or `.env` files
2. **Test before pushing**: Run `act` before pushing workflow changes
3. **Use event payloads**: Create realistic test event JSON files
4. **Document requirements**: Note which secrets/vars each workflow needs
5. **Version control test files**: Keep test event payloads in `test-events/`

## Resources

- [Act GitHub Repository](https://github.com/nektos/act)
- [Act Documentation](https://nektosact.com/)
- [GitHub Actions Documentation](https://docs.github.com/en/actions)