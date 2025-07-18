#!/usr/bin/env bash
# SPDX-License-Identifier: PolyForm
# Script to generate Go bindings from eBPF C code

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "Generating eBPF Go bindings..."

# Generate Go bindings for each eBPF program
cd "${PROJECT_ROOT}"

# Download only the bpf2go tool dependency
echo "Downloading bpf2go tool..."
go mod download github.com/cilium/ebpf

echo "Generating bindings for bpf programs..."
go generate ./pkg/ebpf/...

echo "eBPF Go bindings generation complete!"