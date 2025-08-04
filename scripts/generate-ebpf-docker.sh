#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm
# Script to generate Go bindings from eBPF C code using Docker

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "Building Docker image for eBPF compilation..."
docker build -t ebpf-builder -f "${PROJECT_ROOT}/ebpf/Dockerfile" "${PROJECT_ROOT}/ebpf"

echo "Generating eBPF Go bindings using Docker..."

# Check if BTF mount is available (Linux only)
BTF_MOUNT=""
if [ -d "/sys/kernel/btf" ]; then
    BTF_MOUNT="-v /sys/kernel/btf:/sys/kernel/btf:ro"
fi

# Run the container with necessary mounts
docker run --rm \
    -v "${PROJECT_ROOT}:/workspace" \
    ${BTF_MOUNT} \
    -w /workspace \
    ebpf-builder \
    bash -c "
        # Run the eBPF generation script
        cd /workspace
        ./scripts/generate-ebpf.sh
    "

echo "eBPF Go bindings generation complete!"