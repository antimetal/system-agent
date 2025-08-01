#!/bin/bash
# PolyForm Shield License: https://polyformproject.org/licenses/shield/1.0.0
# Copyright 2024 Antimetal Inc.

set -euo pipefail

# Script to run integration tests inside the VM
# This script is copied into the VM and executed by LVH

echo "=== Kernel Integration Tests ==="
echo "Kernel version: $(uname -r)"
echo "Distribution: $(cat /etc/os-release | grep PRETTY_NAME | cut -d'=' -f2 | tr -d '"')"
echo "Architecture: $(uname -m)"
echo "================================"

# Set up test environment
export TEST_ROOT="/tests"
export RESULTS_DIR="${TEST_ROOT}/results"
export LOGS_DIR="${TEST_ROOT}/logs"

# Create output directories
mkdir -p "${RESULTS_DIR}" "${LOGS_DIR}"

# Run the tests
echo ""
echo "Running kernel compatibility tests..."

# Change to test directory
cd "${TEST_ROOT}"

# Set up Go test environment
export GOCACHE="/tmp/go-cache"
export GOMODCACHE="/tmp/go-mod-cache"
export CGO_ENABLED=1

# Run tests with JSON output for better parsing
"${TEST_ROOT}/bin/test-kernel-compat" \
  -test.v \
  -test.timeout=10m \
  -test.json \
  > "${RESULTS_DIR}/test-output.json" \
  2> "${LOGS_DIR}/test-stderr.log" || TEST_EXIT_CODE=$?

# Also create a human-readable output
"${TEST_ROOT}/bin/test-kernel-compat" \
  -test.v \
  -test.timeout=10m \
  > "${RESULTS_DIR}/test-output.txt" \
  2>&1 || true

# Extract test summary
echo ""
echo "=== Test Summary ==="
if [ -f "${RESULTS_DIR}/test-output.txt" ]; then
    grep -E "^(PASS|FAIL|SKIP)" "${RESULTS_DIR}/test-output.txt" | tail -20
fi

# Check for kernel-specific issues
echo ""
echo "=== Kernel Feature Detection ==="
if grep -q "BTF Support" "${RESULTS_DIR}/test-output.txt"; then
    grep -A2 "BTF Support" "${RESULTS_DIR}/test-output.txt" || true
fi
if grep -q "CO-RE Support" "${RESULTS_DIR}/test-output.txt"; then
    grep -A2 "CO-RE Support" "${RESULTS_DIR}/test-output.txt" || true
fi
if grep -q "Ring Buffer" "${RESULTS_DIR}/test-output.txt"; then
    grep -A2 "Ring Buffer" "${RESULTS_DIR}/test-output.txt" || true
fi

# Create test report
cat > "${RESULTS_DIR}/test-report.txt" <<EOF
Kernel Integration Test Report
==============================
Date: $(date -u +"%Y-%m-%d %H:%M:%S UTC")
Kernel: $(uname -r)
Architecture: $(uname -m)
Distribution: $(cat /etc/os-release | grep PRETTY_NAME | cut -d'=' -f2 | tr -d '"')

Test Results:
$(grep -c "^PASS" "${RESULTS_DIR}/test-output.txt" || echo "0") tests passed
$(grep -c "^FAIL" "${RESULTS_DIR}/test-output.txt" || echo "0") tests failed
$(grep -c "^SKIP" "${RESULTS_DIR}/test-output.txt" || echo "0") tests skipped

EOF

# Exit with test exit code
exit ${TEST_EXIT_CODE:-0}