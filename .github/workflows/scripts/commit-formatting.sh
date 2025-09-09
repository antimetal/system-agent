#!/usr/bin/env bash
# Commit and push formatting changes to PR branch
# This script is used by the auto-format workflow

set -euo pipefail

# Ensure we have required environment variables
if [ -z "${GITHUB_HEAD_REF:-}" ]; then
    echo "Error: GITHUB_HEAD_REF is not set"
    exit 1
fi

# Configure git user for bot commits
git config user.name "github-actions[bot]"
git config user.email "github-actions[bot]@users.noreply.github.com"

# Add all formatting changes
git add .

# Create commit with proper message format
# Using a here-document for multi-line message
git commit -m "chore: auto-format code

Applied automatic code formatting:
- Go formatting via 'make fmt.go'
- C/C++ formatting via 'make fmt.clang'

[skip ci]" || {
    echo "No changes to commit (this shouldn't happen)"
    exit 1
}

# Push changes back to the PR branch
# Use --force-with-lease for safety in case of concurrent changes
echo "Pushing formatting changes to branch: ${GITHUB_HEAD_REF}"
git push --force-with-lease origin "HEAD:${GITHUB_HEAD_REF}"

echo "✅ Formatting changes pushed to PR branch"

exit 0