#!/bin/bash
# Generate test results summary for GitHub Actions
# This script creates a markdown summary of test execution results

set -euo pipefail

# Configuration
TEST_RESULTS_DIR="${1:-test-results}"

# Validate that the test results directory exists
if [ ! -d "${TEST_RESULTS_DIR}" ]; then
    echo "ERROR: Test results directory not found: ${TEST_RESULTS_DIR}"
    exit 1
fi

echo "# Test Results Summary" >> $GITHUB_STEP_SUMMARY
echo "" >> $GITHUB_STEP_SUMMARY

echo "## Test Execution Status" >> $GITHUB_STEP_SUMMARY
echo "" >> $GITHUB_STEP_SUMMARY

# Check for unit test results (could be in subdirectory due to artifact download)
UNIT_TEST_FILE=$(find "${TEST_RESULTS_DIR}" -name "unit-test-results.txt" -type f | head -1)
if [ -n "$UNIT_TEST_FILE" ] && [ -f "$UNIT_TEST_FILE" ]; then
    echo "✅ Unit tests completed" >> $GITHUB_STEP_SUMMARY
    # Extract test summary
    if grep -q "PASS" "$UNIT_TEST_FILE" || grep -q "ok" "$UNIT_TEST_FILE"; then
        echo "  - Status: PASSED" >> $GITHUB_STEP_SUMMARY
    elif grep -q "FAIL" "$UNIT_TEST_FILE"; then
        echo "  - Status: FAILED" >> $GITHUB_STEP_SUMMARY
    fi
    
    # Extract coverage percentage if available
    COVERAGE_LINE=$(grep -E "^total:.*[0-9]+\.[0-9]+%" "$UNIT_TEST_FILE" | tail -1)
    if [ -n "$COVERAGE_LINE" ]; then
        COVERAGE_PCT=$(echo "$COVERAGE_LINE" | grep -oE "[0-9]+\.[0-9]+%" | tail -1)
        echo "  - Coverage: **$COVERAGE_PCT**" >> $GITHUB_STEP_SUMMARY
    fi
else
    echo "⚠️ Unit test results not found" >> $GITHUB_STEP_SUMMARY
    echo "  - Searched in: ${TEST_RESULTS_DIR}" >> $GITHUB_STEP_SUMMARY
fi

# Check for integration test results (multiple kernel versions)
INTEGRATION_TEST_FILES=$(find "${TEST_RESULTS_DIR}" -name "integration-test-results.txt" -type f)
if [ -n "$INTEGRATION_TEST_FILES" ]; then
    echo "✅ Integration tests completed" >> $GITHUB_STEP_SUMMARY
    echo "" >> $GITHUB_STEP_SUMMARY
    echo "### Integration Test Results by Kernel" >> $GITHUB_STEP_SUMMARY
    for file in $INTEGRATION_TEST_FILES; do
        # Extract kernel version from the Test Summary section at the end of the file
        # Look for lines that start with "Kernel:" (not indented)
        kernel_info=$(grep "^Kernel:" "$file" | tail -1 | cut -d: -f2 | tr -d ' ')
        status=$(grep "^Status:" "$file" | tail -1 | cut -d: -f2 | tr -d ' ')
        
        # If we found a kernel version, display it
        if [ -n "$kernel_info" ]; then
            echo "  - Kernel $kernel_info: ${status:-UNKNOWN}" >> $GITHUB_STEP_SUMMARY
        else
            # Fallback: try to extract from parent directory name (artifact name)
            parent_dir=$(basename $(dirname "$file"))
            if [[ "$parent_dir" =~ test-results-(.+) ]]; then
                kernel_from_dir="${BASH_REMATCH[1]}"
                echo "  - Kernel $kernel_from_dir: ${status:-UNKNOWN}" >> $GITHUB_STEP_SUMMARY
            fi
        fi
    done
else
    echo "⚠️ Integration test results not found" >> $GITHUB_STEP_SUMMARY
    echo "  - Searched in: ${TEST_RESULTS_DIR}" >> $GITHUB_STEP_SUMMARY
fi

echo "" >> $GITHUB_STEP_SUMMARY
echo "## Test Matrix" >> $GITHUB_STEP_SUMMARY
echo "" >> $GITHUB_STEP_SUMMARY
echo "| Kernel Version | Description |" >> $GITHUB_STEP_SUMMARY
echo "|----------------|-------------|" >> $GITHUB_STEP_SUMMARY
echo "| 5.4 | Ubuntu 20.04 LTS |" >> $GITHUB_STEP_SUMMARY
echo "| 5.10 | Stable kernel |" >> $GITHUB_STEP_SUMMARY
echo "| 5.15 | Ubuntu 22.04 LTS |" >> $GITHUB_STEP_SUMMARY
echo "| 6.1 | LTS kernel |" >> $GITHUB_STEP_SUMMARY
echo "| 6.6 | Current LTS kernel |" >> $GITHUB_STEP_SUMMARY
echo "| 6.12 | Next LTS kernel |" >> $GITHUB_STEP_SUMMARY
echo "" >> $GITHUB_STEP_SUMMARY

echo "## Testing Strategy" >> $GITHUB_STEP_SUMMARY
echo "" >> $GITHUB_STEP_SUMMARY
echo "- **Unit Tests**: Run natively on GitHub Actions runner" >> $GITHUB_STEP_SUMMARY
echo "- **Integration Tests**: Run in VMs with real kernel features" >> $GITHUB_STEP_SUMMARY
echo "- **Build Tags**: Use \`//go:build integration\` for integration tests" >> $GITHUB_STEP_SUMMARY
echo "- **eBPF Tests**: Run in VMs with CAP_SYS_ADMIN capability" >> $GITHUB_STEP_SUMMARY
echo "" >> $GITHUB_STEP_SUMMARY

# Check if coverage report exists
COVERAGE_FILE=$(find "${TEST_RESULTS_DIR}" -name "coverage-unit.out" -o -name "coverage-integration.out" -type f | head -1 || true)
if [ -n "$COVERAGE_FILE" ] && [ -f "$COVERAGE_FILE" ] && command -v go >/dev/null 2>&1; then
    echo "## Coverage Details" >> $GITHUB_STEP_SUMMARY
    echo "" >> $GITHUB_STEP_SUMMARY
    
    # Process unit coverage if exists
    UNIT_COVERAGE=$(find "${TEST_RESULTS_DIR}" -name "coverage-unit.out" -type f | head -1)
    if [ -n "$UNIT_COVERAGE" ] && [ -f "$UNIT_COVERAGE" ]; then
        COVERAGE_PCT=$(go tool cover -func="$UNIT_COVERAGE" 2>/dev/null | grep "^total:" | awk '{print $3}')
        if [ -n "$COVERAGE_PCT" ]; then
            echo "### Unit Test Coverage: $COVERAGE_PCT" >> $GITHUB_STEP_SUMMARY
        fi
    fi
    
    # Process integration coverage if exists
    INT_COVERAGE=$(find "${TEST_RESULTS_DIR}" -name "coverage-integration.out" -type f | head -1)
    if [ -n "$INT_COVERAGE" ] && [ -f "$INT_COVERAGE" ]; then
        COVERAGE_PCT=$(go tool cover -func="$INT_COVERAGE" 2>/dev/null | grep "^total:" | awk '{print $3}')
        if [ -n "$COVERAGE_PCT" ]; then
            echo "### Integration Test Coverage: $COVERAGE_PCT" >> $GITHUB_STEP_SUMMARY
        fi
    fi
    
    echo "" >> $GITHUB_STEP_SUMMARY
    
    # Add link to coverage artifact if we're in GitHub Actions
    if [ -n "$GITHUB_RUN_ID" ] && [ -n "$GITHUB_REPOSITORY" ]; then
        echo "### 📊 Coverage Reports" >> $GITHUB_STEP_SUMMARY
        echo "" >> $GITHUB_STEP_SUMMARY
        echo "[📥 Download Combined Coverage Report](https://github.com/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}#artifacts)" >> $GITHUB_STEP_SUMMARY
        echo "" >> $GITHUB_STEP_SUMMARY
        echo "The **coverage-combined** artifact contains:" >> $GITHUB_STEP_SUMMARY
        echo "- 📄 `coverage-all.html` - Interactive HTML coverage report" >> $GITHUB_STEP_SUMMARY
        echo "- 📊 `COVERAGE.md` - Coverage summary" >> $GITHUB_STEP_SUMMARY
        echo "- 🔍 `coverage-all.out` - Raw coverage data" >> $GITHUB_STEP_SUMMARY
    else
        echo "📊 Download coverage artifacts for detailed reports" >> $GITHUB_STEP_SUMMARY
    fi
    echo "" >> $GITHUB_STEP_SUMMARY
fi

echo "✅ Test summary generated successfully"