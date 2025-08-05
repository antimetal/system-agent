#!/bin/bash
# Give the PR a moment to be created
sleep 2

# Get the most recent PR number and branch info for this repo
pr_info=$(gh pr list --limit 1 --json number,headRefName --jq '.[0] | {number, branch: .headRefName}' 2>&1)
gh_exit_code=$?

if [ $gh_exit_code -ne 0 ]; then
    echo "⚠️  Failed to fetch PR information from GitHub"
    echo "Error: $pr_info"
    exit 1
fi

if [ -n "$pr_info" ] && [ "$pr_info" != "null" ]; then
    pr_number=$(echo "$pr_info" | jq -r '.number')
    pr_branch=$(echo "$pr_info" | jq -r '.branch')
    
    if [ "$pr_number" != "null" ] && [ "$pr_branch" != "null" ]; then
        echo "🔄 Creating review worktree for PR #$pr_number (branch: $pr_branch)"
        
        # Fetch the latest remote refs to ensure we have the PR branch
        if ! git fetch origin "$pr_branch:$pr_branch" 2>/dev/null; then
            echo "⚠️  Failed to fetch PR branch directly, trying general fetch..."
            if ! git fetch origin; then
                echo "❌ Failed to fetch from origin. Check your network connection."
                exit 1
            fi
        fi
        
        # Create worktree with the PR branch, tracking the remote
        if git worktree add "../pr-$pr_number" -b "review-pr-$pr_number" --track "origin/$pr_branch" 2>/dev/null; then
            echo "✅ Created worktree at ../pr-$pr_number tracking origin/$pr_branch"
        else
            echo "❌ Failed to create worktree for PR #$pr_number"
            exit 1
        fi
    fi
fi