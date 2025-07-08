FROM ubuntu:22.04

# Install dependencies for eBPF development
RUN apt-get update -q && \
    apt-get install -y -q \
        clang \
        llvm \
        libbpf-dev \
        linux-headers-generic \
        golang-go \
        git \
        wget && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install newer Go if the default version is too old
RUN if ! go version | grep -q '1.2[0-9]'; then \
        wget -q https://go.dev/dl/go1.21.5.linux-$(dpkg --print-architecture).tar.gz && \
        tar -C /usr/local -xzf go1.21.5.linux-$(dpkg --print-architecture).tar.gz && \
        rm go1.21.5.linux-$(dpkg --print-architecture).tar.gz && \
        ln -sf /usr/local/go/bin/go /usr/bin/go; \
    fi

# Set up Go environment
ENV GOPATH=/tmp/go
ENV PATH="${GOPATH}/bin:${PATH}"

# Install bpf2go
RUN go install github.com/cilium/ebpf/cmd/bpf2go@v0.19.0

WORKDIR /workspace