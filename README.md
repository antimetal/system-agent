# Antimetal Agent

Component that connects your infrastructure to the [Antimetal](https://antimetal.com) platform.

## Features

- **Kubernetes Resource Monitoring**: Real-time collection of Kubernetes resources via controller-runtime
- **System Performance Monitoring**: Comprehensive system metrics including CPU, memory, disk, and network
- **Container Resource Monitoring**: Per-container CPU and memory metrics with throttling and pressure detection
- **eBPF-based Observability**: Kernel-level insights with CO-RE support for cross-kernel compatibility
- **Multi-Cloud Support**: Works with EKS, GKE, AKS, and self-managed Kubernetes clusters
- **Efficient Data Storage**: BadgerDB-backed resource state tracking with event-driven updates
- **gRPC Streaming**: Efficient data upload to Antimetal platform with automatic retry and recovery

### Container Monitoring Capabilities

- **CPU Throttling Detection**: Identify containers hitting CPU limits
- **Memory Pressure Analysis**: Detect containers approaching OOM conditions  
- **Resource Contention**: Find "noisy neighbor" containers affecting others
- **Multi-Runtime Support**: Works with Docker, containerd, and CRI-O
- **Cgroup v1 and v2**: Compatible with both cgroup hierarchies

## Contributing

If you want to contribute, refer to our [DEVELOPING](./DEVELOPING.md) docs.

## Helm chart

The helm chart for the Antimetal Agent is published in the [antimetal/helm-charts](https://github.com/antimetal/helm-charts) repo.

## Docker images

We publish Docker images for each new release on [DockerHub](https://hub.docker.com/r/antimetal/agent).
We build linux/amd64 and linux/arm64 images.

## Support
For questions and support feel free to post a GitHub Issue.
For commercial support, contact us at support@antimetal.com.

## License

This project uses split licensing:

- **Userspace components** (`/cmd`, `/internal`, `/pkg`): PolyForm Shield
- **eBPF programs** (`/ebpf`): GPL-2.0-only

The eBPF programs must be GPL-licensed due to their use of GPL-only kernel helper functions. 
This is a standard practice in the industry for projects that include both userspace and eBPF code.

If you are an end user, broadly you can do whatever you want with the userspace code under PolyForm Shield.
See the License FAQ for more information about PolyForm licensing.
