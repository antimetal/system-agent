#!/bin/bash
# Install shared git hooks for the Antimetal Agent project
#
# This script sets up git hooks that automatically configure Claude context
# files when working with git worktrees for PR reviews.
#
# Usage: ./.githooks/install.sh

set -e

REPO_ROOT=$(git rev-parse --show-toplevel)
GITHOOKS_DIR="$REPO_ROOT/.githooks"
HOOKS_DIR="$REPO_ROOT/.git/hooks"

echo "🔧 Installing shared git hooks for Antimetal Agent"
echo "=================================================="

# Check if we're in a git repository
if ! git rev-parse --git-dir > /dev/null 2>&1; then
    echo "❌ Error: Not in a git repository"
    exit 1
fi

# Install post-checkout hook for Claude context setup
if [ -f "$GITHOOKS_DIR/post-checkout" ]; then
    cp "$GITHOOKS_DIR/post-checkout" "$HOOKS_DIR/post-checkout"
    chmod +x "$HOOKS_DIR/post-checkout"
    echo "✅ Installed post-checkout hook (Claude context setup for worktrees)"
else
    echo "❌ Error: post-checkout hook not found in .githooks/"
    exit 1
fi

# Install pre-commit hook for branch protection
if [ -f "$GITHOOKS_DIR/pre-commit" ]; then
    cp "$GITHOOKS_DIR/pre-commit" "$HOOKS_DIR/pre-commit"
    chmod +x "$HOOKS_DIR/pre-commit"
    echo "✅ Installed pre-commit hook (prevents direct commits to main branch)"
else
    echo "❌ Error: pre-commit hook not found in .githooks/"
    exit 1
fi

# Set git config to use our hooks directory for future hooks
# (This is optional but recommended for consistency)
git config core.hooksPath .githooks

echo ""
echo "🎉 Git hooks installation complete!"
echo ""
echo "What these hooks do:"
echo "  • post-checkout: Sets up Claude context files (CLAUDE.local.md, settings.local.json)"
echo "                   when creating or switching to git worktrees"
echo "  • pre-commit:    Prevents direct commits to main branch (enforces feature branch workflow)"
echo ""
echo "To test:"
echo "  • Create a git worktree and verify Claude files are properly symlinked"
echo "  • Try committing directly to main branch (should be blocked)"