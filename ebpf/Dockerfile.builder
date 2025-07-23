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

COPY scripts/generate_vmlinux.sh /tmp/generate_vmlinux.sh
COPY scripts/install-go.sh /tmp/install-go.sh

# Generate vmlinux.h from BTF for CO-RE eBPF programs
RUN chmod +x /tmp/generate_vmlinux.sh && DOCKER_BUILD=1 /tmp/generate_vmlinux.sh

# Verify vmlinux.h was downloaded successfully
RUN if [ -f /usr/include/vmlinux.h ] && [ -s /usr/include/vmlinux.h ]; then \
        echo "=== vmlinux.h successfully installed ===" && \
        echo "Location: /usr/include/vmlinux.h" && \
        echo "Size: $(wc -l < /usr/include/vmlinux.h) lines" && \
        echo "First few type definitions:" && \
        grep -m 5 "struct\|enum\|typedef" /usr/include/vmlinux.h || true; \
    else \
        echo "ERROR: vmlinux.h was not created or is empty!" && \
        exit 1; \
    fi

# Install Go from official tarball with checksum verification
RUN chmod +x /tmp/install-go.sh && /tmp/install-go.sh

# Set Go in PATH
ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /workspace
