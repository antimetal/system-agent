#!/bin/bash
# Merge coverage reports from unit and integration tests into a combined artifact
set -euo pipefail

TEST_RESULTS_DIR="${1:-test-results}"

# Find all coverage files
UNIT_COV=$(find "$TEST_RESULTS_DIR" -name "coverage-unit.out" -type f | head -1)
INT_COV=$(find "$TEST_RESULTS_DIR" -name "coverage-integration.out" -type f | head -1)

if [ -n "$UNIT_COV" ] || [ -n "$INT_COV" ]; then
  mkdir -p coverage
  
  # If we have both, merge them
  if [ -n "$UNIT_COV" ] && [ -n "$INT_COV" ]; then
    echo "Merging unit and integration coverage..."
    go install github.com/wadey/gocovmerge@latest
    $(go env GOPATH)/bin/gocovmerge "$UNIT_COV" "$INT_COV" > coverage/coverage-all.out
    
    # Generate summary
    echo "Combined coverage:"
    go tool cover -func=coverage/coverage-all.out | tail -n 1
    
    # Generate HTML report
    go tool cover -html=coverage/coverage-all.out -o coverage/coverage-all.html
    
    # Copy individual files too
    cp "$UNIT_COV" coverage/
    [ -n "$INT_COV" ] && cp "$INT_COV" coverage/
    
  elif [ -n "$UNIT_COV" ]; then
    # Only unit tests available
    cp "$UNIT_COV" coverage/coverage-all.out
    cp "$UNIT_COV" coverage/coverage-unit.out
    go tool cover -html=coverage/coverage-all.out -o coverage/coverage-all.html
  fi
  
  # Generate coverage summary file
  echo "# Coverage Summary" > coverage/COVERAGE.md
  echo "" >> coverage/COVERAGE.md
  echo "Generated: $(date)" >> coverage/COVERAGE.md
  echo "" >> coverage/COVERAGE.md
  
  if [ -f coverage/coverage-unit.out ]; then
    UNIT_PCT=$(go tool cover -func=coverage/coverage-unit.out | grep "^total:" | awk '{print $3}')
    echo "## Unit Test Coverage: $UNIT_PCT" >> coverage/COVERAGE.md
  fi
  
  if [ -n "$INT_COV" ] && [ -f "$INT_COV" ]; then
    INT_PCT=$(go tool cover -func="$INT_COV" | grep "^total:" | awk '{print $3}')
    echo "## Integration Test Coverage: $INT_PCT" >> coverage/COVERAGE.md
  fi
  
  if [ -f coverage/coverage-all.out ]; then
    TOTAL_PCT=$(go tool cover -func=coverage/coverage-all.out | grep "^total:" | awk '{print $3}')
    echo "## **Total Coverage: $TOTAL_PCT**" >> coverage/COVERAGE.md
  fi
  
  echo "" >> coverage/COVERAGE.md
  echo "### 📊 How to View Coverage Reports" >> coverage/COVERAGE.md
  echo "" >> coverage/COVERAGE.md
  echo "1. **HTML Report**: Open \`coverage-all.html\` in a browser for interactive line-by-line coverage" >> coverage/COVERAGE.md
  echo "2. **Raw Data**: Use \`go tool cover -func=coverage-all.out\` to see function-level coverage" >> coverage/COVERAGE.md
  echo "" >> coverage/COVERAGE.md
  
  # If we're in GitHub Actions, output to job summary too
  if [ -n "$GITHUB_STEP_SUMMARY" ]; then
    echo "### ✅ Coverage Merged Successfully" >> $GITHUB_STEP_SUMMARY
    echo "" >> $GITHUB_STEP_SUMMARY
    if [ -f coverage/coverage-all.out ]; then
      echo "**Combined Test Coverage: $TOTAL_PCT**" >> $GITHUB_STEP_SUMMARY
    fi
  fi
else
  echo "No coverage files found to merge"
  exit 0
fi