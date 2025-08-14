#!/bin/bash
# Copyright 2024 Antimetal LLC
# SPDX-License-Identifier: PolyForm-Shield-1.0.0

set -e

# Generate Go code from protobuf definitions
echo "Generating protobuf code..."

# Ensure protoc-gen-go is installed
if ! command -v protoc-gen-go &> /dev/null; then
    echo "Installing protoc-gen-go..."
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
fi

# Generate profile.proto
protoc \
    --go_out=. \
    --go_opt=paths=source_relative \
    pkg/performance/proto/profile.proto

echo "Protobuf generation complete"