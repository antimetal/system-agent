#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm
# Script to generate Go bindings from eBPF C code using Docker

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "Building Docker image for eBPF compilation..."
docker build -t ebpf-builder -f "${PROJECT_ROOT}/ebpf/Dockerfile" "${PROJECT_ROOT}/ebpf"

echo "Generating eBPF Go bindings using Docker..."
docker run --rm \
    -v "${PROJECT_ROOT}:/workspace" \
    -w /workspace \
    ebpf-builder \
    bash -c "
        apt-get update && apt-get install -y ca-certificates git wget
        # Install Go 1.23
        wget -q https://go.dev/dl/go1.23.4.linux-arm64.tar.gz
        tar -C /usr/local -xzf go1.23.4.linux-arm64.tar.gz
        export PATH=/usr/local/go/bin:\$PATH
        export GOPROXY=direct
        export GOSUMDB=off
        go install github.com/cilium/ebpf/cmd/bpf2go@v0.19.0
        export PATH=\$PATH:/root/go/bin
        cd /workspace/pkg/ebpf
        # Add kernel headers to include path
        bpf2go -cc clang -cflags '-O2 -g -Wall -Werror -I/usr/include/aarch64-linux-gnu -I../../ebpf/include' -target bpfel,bpfeb -go-package ebpf hello ../../ebpf/src/hello.bpf.c -- -I/usr/include/aarch64-linux-gnu -I../../ebpf/include
    "

echo "eBPF Go bindings generation complete!"