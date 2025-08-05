# Git Hooks

This directory contains a git hook for Claude Code worktree setup.

## Installation

```bash
make install-hooks
```

## post-checkout Hook

Automatically symlinks Claude configuration files when working in git worktrees.

**What it does:**
- Symlinks `CLAUDE.local.md` from main repo
- Symlinks `.claude/settings.local.json` from main repo
- Ensures Claude Code works seamlessly in PR review worktrees

**Usage:**
1. Create a worktree: `git worktree add ../pr-123 branch-name`
2. Claude configuration is automatically set up
3. Your personal settings are available immediately