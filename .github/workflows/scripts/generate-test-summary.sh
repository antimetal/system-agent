#!/bin/bash
# Generate test results summary for GitHub Actions
# This script creates a markdown summary of test execution results

set -euo pipefail

# Configuration
TEST_RESULTS_DIR="${1:-test-results}"

echo "# Test Results Summary" >> $GITHUB_STEP_SUMMARY
echo "" >> $GITHUB_STEP_SUMMARY

echo "## Test Execution Status" >> $GITHUB_STEP_SUMMARY
echo "" >> $GITHUB_STEP_SUMMARY

if [ -f "${TEST_RESULTS_DIR}/unit-test-results.txt" ]; then
    echo "✅ Unit tests completed" >> $GITHUB_STEP_SUMMARY
else
    echo "⚠️ Unit test results not found" >> $GITHUB_STEP_SUMMARY
fi

if [ -f "${TEST_RESULTS_DIR}/integration-test-results.txt" ]; then
    echo "✅ Integration tests completed" >> $GITHUB_STEP_SUMMARY
else
    echo "⚠️ Integration test results not found" >> $GITHUB_STEP_SUMMARY
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
echo "| 6.6 | Latest LTS kernel |" >> $GITHUB_STEP_SUMMARY
echo "" >> $GITHUB_STEP_SUMMARY

echo "## Testing Strategy" >> $GITHUB_STEP_SUMMARY
echo "" >> $GITHUB_STEP_SUMMARY
echo "- **Unit Tests**: Run natively on GitHub Actions runner" >> $GITHUB_STEP_SUMMARY
echo "- **Integration Tests**: Run in VMs with real kernel features" >> $GITHUB_STEP_SUMMARY
echo "- **Build Tags**: Use \`//go:build integration\` for integration tests" >> $GITHUB_STEP_SUMMARY
echo "- **eBPF Tests**: Run in VMs with CAP_SYS_ADMIN capability" >> $GITHUB_STEP_SUMMARY

echo "Test summary generated successfully"