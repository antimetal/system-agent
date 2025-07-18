# Simple eBPF build environment
FROM ubuntu:24.04

# Install minimal required packages for eBPF development
RUN apt-get update && apt-get install -y --no-install-recommends \
		curl \
		xz-utils \
		ca-certificates \
		clang \
		llvm \
		libbpf-dev \
		linux-tools-generic \
		make \
		git \
		build-essential \
		&& rm -rf /var/lib/apt/lists/*

# Build bpftool from source to get latest version with Go support
# Pin to specific commit hash for security (v7.5.0 tag)
RUN git clone https://github.com/libbpf/bpftool.git /tmp/bpftool && \
    cd /tmp/bpftool && \
    git checkout v7.5.0 && \
    git verify-tag v7.5.0 2>/dev/null || echo "Warning: Could not verify git tag signature" && \
    git submodule update --init && \
    cd src && \
    make -j$(nproc) && \
    make install && \
    cd / && \
    rm -rf /tmp/bpftool

# Create script to generate vmlinux.h at build time
# This allows the container to either use a local vmlinux.h or download from btfhub
RUN printf '#!/bin/bash\n\
set -euo pipefail\n\
\n\
VMLINUX_PATH="/workspace/ebpf/include/vmlinux.h"\n\
\n\
# Check if local vmlinux.h exists and is non-empty\n\
if [ -f "$VMLINUX_PATH" ] && [ -s "$VMLINUX_PATH" ]; then\n\
    echo "Using existing vmlinux.h from $VMLINUX_PATH"\n\
    exit 0\n\
fi\n\
\n\
echo "Local vmlinux.h not found or empty, downloading from btfhub..."\n\
\n\
# Create include directory if it doesnt exist\n\
mkdir -p "$(dirname "$VMLINUX_PATH")"\n\
\n\
# Detect architecture\n\
ARCH=$(uname -m)\n\
case "$ARCH" in\n\
    x86_64)\n\
        BTF_ARCH="x86"\n\
        BTF_URL="https://raw.githubusercontent.com/aquasecurity/btfhub-archive/main/ubuntu/20.04/x86/5.4.0-91-generic.btf.tar.xz"\n\
        BTF_FILENAME="5.4.0-91-generic.btf"\n\
        ;;\n\
    aarch64)\n\
        BTF_ARCH="arm64"\n\
        BTF_URL="https://raw.githubusercontent.com/aquasecurity/btfhub-archive/main/ubuntu/20.04/arm64/5.8.0-63-generic.btf.tar.xz"\n\
        BTF_FILENAME="5.8.0-63-generic.btf"\n\
        ;;\n\
    *)\n\
        echo "Error: Unsupported architecture $ARCH"\n\
        exit 1\n\
        ;;\n\
esac\n\
\n\
# Download BTF archive\n\
echo "Downloading BTF for $BTF_ARCH from btfhub..."\n\
if ! curl -sL "$BTF_URL" -o /tmp/btf.tar.xz; then\n\
    echo "Error: Failed to download BTF archive from $BTF_URL"\n\
    exit 1\n\
fi\n\
\n\
# Extract BTF file\n\
echo "Extracting BTF archive..."\n\
if ! tar -xJf /tmp/btf.tar.xz -C /tmp; then\n\
    echo "Error: Failed to extract BTF archive"\n\
    rm -f /tmp/btf.tar.xz\n\
    exit 1\n\
fi\n\
\n\
# Generate vmlinux.h\n\
echo "Generating vmlinux.h..."\n\
if ! bpftool btf dump file "/tmp/$BTF_FILENAME" format c > "$VMLINUX_PATH"; then\n\
    echo "Error: Failed to generate vmlinux.h"\n\
    rm -f /tmp/btf.tar.xz "/tmp/$BTF_FILENAME"\n\
    exit 1\n\
fi\n\
\n\
# Cleanup\n\
rm -f /tmp/btf.tar.xz "/tmp/$BTF_FILENAME"\n\
\n\
echo "Successfully generated vmlinux.h at $VMLINUX_PATH"\n\
echo "Size: $(wc -l < "$VMLINUX_PATH") lines"\n' > /usr/local/bin/ensure-vmlinux.sh && \
    chmod +x /usr/local/bin/ensure-vmlinux.sh

# Install Go 1.24.1 from official tarball based on architecture
# Add checksum verification for security
#
# To update checksums for new Go versions:
# 1. Temporarily comment out the checksum verification section below
# 2. Run: make build-ebpf-builder
# 3. Copy the displayed SHA256 checksums from the build output
# 4. Update the EXPECTED_GO_SHA256_* variables below
# 5. Uncomment the verification section
# 6. Test with: make build-ebpf-builder
RUN ARCH=$(uname -m) && \
    case "$ARCH" in \
        x86_64) GO_ARCH="amd64" ;; \
        aarch64) GO_ARCH="arm64" ;; \
        *) echo "Unsupported architecture: $ARCH" && exit 1 ;; \
    esac && \
    GO_VERSION="1.24.1" && \
    GO_URL="https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz" && \
    # Expected checksums for Go 1.24.1 (update when changing GO_VERSION)
    case "$GO_ARCH" in \
        amd64) EXPECTED_GO_SHA256="cb2396bae64183cdccf81a9a6df0aea3bce9511fc21469fb89a0c00470088073" ;; \
        arm64) EXPECTED_GO_SHA256="8df5750ffc0281017fb6070fba450f5d22b600a02081dceef47966ffaf36a3af" ;; \
        *) echo "Unsupported Go architecture: $GO_ARCH" && exit 1 ;; \
    esac && \
    curl -sL "$GO_URL" -o /tmp/go.tar.gz && \
    echo "Downloaded Go ${GO_VERSION} for ${GO_ARCH}, verifying checksum..." && \
    ACTUAL_SHA256=$(sha256sum /tmp/go.tar.gz | cut -d' ' -f1) && \
    echo "Go archive SHA256: $ACTUAL_SHA256" && \
    echo "Expected SHA256: $EXPECTED_GO_SHA256" && \
    if [ "$ACTUAL_SHA256" != "$EXPECTED_GO_SHA256" ]; then \
        echo "ERROR: Go archive checksum mismatch!" && \
        echo "Expected: $EXPECTED_GO_SHA256" && \
        echo "Actual:   $ACTUAL_SHA256" && \
        echo "If updating Go version, see comments above for checksum update procedure" && \
        exit 1; \
    fi && \
    echo "✓ Go archive integrity verified" && \
    tar -C /usr/local -xzf /tmp/go.tar.gz && \
    rm -f /tmp/go.tar.gz

# Set Go in PATH
ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /workspace
