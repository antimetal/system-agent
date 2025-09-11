#!/usr/bin/env bash
# Check if code formatting is needed and apply it
# This script is used by the auto-format workflow

set -euo pipefail

# Function to check for changes
check_changes() {
    if [ -n "$(git status --porcelain)" ]; then
        echo "true"
    else
        echo "false"
    fi
}

# Run Go formatter
echo "Running Go formatter..."
make fmt.go

# Check if Go formatting made changes
GO_CHANGES=$(check_changes)

# Run C/C++ formatter
echo "Running clang formatter..."
# Check if there are any C/C++ files to format
if find . -name "*.c" -o -name "*.h" | grep -E "(ebpf|bpf)" | head -1 > /dev/null 2>&1; then
    make fmt.clang || {
        echo "::warning::Clang formatting failed or skipped"
    }
else
    echo "No C/C++ files to format"
fi

# Final check for any changes
CHANGES=$(check_changes)

# Output results for GitHub Actions
echo "changes=${CHANGES}" >> "${GITHUB_OUTPUT}"

# Show what files were changed (if any)
if [ "${CHANGES}" = "true" ]; then
    echo "Files modified by formatting:"
    git status --porcelain
else
    echo "✅ Code is already properly formatted - no changes needed"
fi

exit 0