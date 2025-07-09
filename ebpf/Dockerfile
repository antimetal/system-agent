# Simple eBPF build environment
FROM ubuntu:22.04

# Install minimal required packages
RUN apt-get update && apt-get install -y \
    wget \
    git \
    make \
    clang \
    llvm \
    && rm -rf /var/lib/apt/lists/*

# Install bpftool from Ubuntu packages (stable version)
RUN apt-get update && apt-get install -y \
    linux-tools-common \
    linux-tools-generic \
    && rm -rf /var/lib/apt/lists/*

# Make bpftool accessible (Ubuntu puts it in version-specific directory)
RUN ln -sf /usr/lib/linux-tools/*/bpftool /usr/local/bin/bpftool || \
    echo "Warning: bpftool symlink failed, will try at runtime"

# Install Go 1.23 from official tarball
RUN wget -q https://go.dev/dl/go1.23.4.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.23.4.linux-amd64.tar.gz && \
    rm go1.23.4.linux-amd64.tar.gz

# Set Go in PATH
ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /workspace