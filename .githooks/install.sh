#!/bin/bash
# Install git hook for Claude worktree setup

set -e

REPO_ROOT=$(git rev-parse --show-toplevel)
GITHOOKS_DIR="$REPO_ROOT/.githooks"
HOOKS_DIR="$REPO_ROOT/.git/hooks"

# Check if we're in a git repository
if ! git rev-parse --git-dir > /dev/null 2>&1; then
    echo "Error: Not in a git repository"
    exit 1
fi

# Install post-checkout hook
if [ -f "$GITHOOKS_DIR/post-checkout" ]; then
    cp "$GITHOOKS_DIR/post-checkout" "$HOOKS_DIR/post-checkout"
    chmod +x "$HOOKS_DIR/post-checkout"
    echo "✅ Installed post-checkout hook for Claude worktree setup"
else
    echo "Error: post-checkout hook not found"
    exit 1
fi

# Set git config to use our hooks directory
git config core.hooksPath .githooks