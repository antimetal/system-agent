# Build environment for eBPF programs
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y \
    clang \
    llvm \
    libbpf-dev \
    linux-headers-generic \
    linux-tools-generic \
    make \
    gcc \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Install kernel headers for the specific kernel version
RUN apt-get update && apt-get install -y linux-headers-$(uname -r) || true

WORKDIR /ebpf

# Copy eBPF source files
COPY . .

# Default command
CMD ["make"]