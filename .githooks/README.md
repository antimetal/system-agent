# Shared Git Hooks

This directory contains git hooks that can be shared across the team to ensure consistent development workflows.

## Installation

To install these hooks on your local repository:

```bash
./.githooks/install.sh
```

## Available Hook

### post-checkout

Automatically sets up Claude Code context when working with git worktrees.

**What it does:**
- Detects when you're checking out a git worktree (used for PR reviews)
- Creates symlinks for personal Claude configuration files:
  - `CLAUDE.local.md` → symlinked to main repo
  - `.claude/settings.local.json` → symlinked to main repo
- Ensures the `.claude` directory exists in worktrees
- Copies other Claude context files as needed

**Why this is useful:**
- When reviewing PRs in separate worktrees, you'll have access to your personal Claude configuration
- Changes to your Claude settings are automatically shared across all worktrees
- No need to manually set up Claude context for each PR review

## Usage with PR Reviews

1. Create a PR (this may trigger the Claude hook to create a worktree automatically)
2. Or manually create a worktree: `git worktree add ../pr-review main`
3. The post-checkout hook will automatically set up Claude context
4. Use Claude Code in the worktree with your full configuration available

## Development

To modify hooks:
1. Edit the hook file in `.githooks/`
2. Test your changes
3. Run `.githooks/install.sh` to update your local installation
4. Commit the changes to share with the team

## Team Onboarding

New team members should run:
```bash
./.githooks/install.sh
```

This ensures everyone has the same git hook setup for consistent development workflows.