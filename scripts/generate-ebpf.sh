#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm
# Script to generate Go bindings from eBPF C code

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "Generating eBPF Go bindings..."

# Ensure bpf2go is installed
go install github.com/cilium/ebpf/cmd/bpf2go@latest

# Generate Go bindings for each eBPF program
cd "${PROJECT_ROOT}"

# Generate hello.bpf.c bindings
if [ -f "ebpf/src/hello.bpf.c" ]; then
    echo "Generating bindings for hello.bpf.c..."
    go generate ./pkg/ebpf/...
fi

echo "eBPF Go bindings generation complete!"