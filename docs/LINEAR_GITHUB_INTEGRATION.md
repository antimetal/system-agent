# GitHub-Linear Integration Workflow

## Overview
This project uses GitHub Issues for public tracking and Linear for internal
planning. The integration automatically syncs minimal information of public changes into our internal systems.

## Workflow

### 1. GitHub Issue → Linear Issue
- **Automatic**: When a GitHub issue is created, a corresponding Linear issue is auto-created
- **Manual**: Reference the GitHub issue URL in the Linear issue description
- **Cross-reference**: Both issues link to each other

### 2. Pull Request → Issue Linking
- **PR linking**: Because GitHub issue status is linkes, PRs can just reference
  the GitHub issue. This avoids having to know the internal identifier or use a
  long, very specific, branch name to link PRs to linear tasks.

### 3. Status Updates
- **GitHub → Linear**: Issue/PR status changes update Linear automatically
- **Linear → GitHub**: Linear changes do NOT affect GitHub (one-way sync)

## Configuration Requirements

### GitHub Secrets
- `LINEAR_API_TOKEN`: Linear API token for creating issues
