#!/bin/bash
set -e

# Script to generate eBPF Go bindings using Docker
# This allows development on macOS while targeting Linux

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
DOCKER_IMAGE="antimetal-ebpf-builder"

echo "Generating eBPF Go bindings using Docker..."

# Build the Docker image if it doesn't exist or if Dockerfile is newer
if [[ ! "$(docker images -q ${DOCKER_IMAGE} 2> /dev/null)" ]] || \
   [[ "$PROJECT_ROOT/ebpf/Dockerfile" -nt "$(docker inspect -f '{{.Created}}' ${DOCKER_IMAGE} 2> /dev/null || echo '')" ]]; then
    echo "Building Docker image for eBPF generation..."
    docker build -t ${DOCKER_IMAGE} -f "$PROJECT_ROOT/ebpf/Dockerfile" "$PROJECT_ROOT/ebpf"
fi

# Run the container to generate bindings
docker run --rm \
    -v "$PROJECT_ROOT:/workspace" \
    -w /workspace/pkg/ebpf \
    ${DOCKER_IMAGE} \
    bash -c "
        set -e
        
        # Generate bindings with correct package name
        # Use the kernel headers from the installed linux-headers package
        CFLAGS=\"-I/usr/include/aarch64-linux-gnu -I/usr/include\"
        GOPACKAGE=ebpf bpf2go -cc clang -cflags \"\$CFLAGS\" -target amd64,arm64 hello ../../ebpf/programs/hello.bpf.c
        
        echo 'eBPF Go bindings generated successfully!'
    "

echo "Generation complete!"