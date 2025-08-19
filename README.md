# Antimetal Agent

Component that connects your infrastructure to the [Antimetal](https://antimetal.com) platform.

## Features

### Memory Growth Monitoring (eBPF-based)

The Antimetal Agent includes an advanced **multi-detector memory leak detection system** that uses eBPF technology to identify memory leaks before they cause OOM kills. The system employs three complementary detection algorithms:

- **Linear Regression Detector** - Statistical trend analysis for steady growth patterns
- **RSS Component Ratio Detector** - Distinguishes heap leaks from cache growth
- **Multi-Factor Threshold Detector** - Industry-proven heuristics from Google, Microsoft, and Facebook

Key capabilities:
- **<0.1% CPU overhead** in production environments
- **2-10 minute advance warning** before OOM events
- **Multiple validation signals** for high-confidence detection
- **Zero network overhead** - analysis happens in-kernel

For detailed documentation, see:
- [Memory Monitor Architecture](docs/memory-monitor-architecture.md)
- [Memory Growth Monitor Whitepaper](docs/memory-growth-monitor-whitepaper.md)
- [Implementation Guide](docs/memory-monitor-implementation.md)

### Kubernetes Resource Monitoring

The agent watches and collects Kubernetes resources, providing real-time visibility into your cluster's state through the Antimetal platform.

### Performance Metrics Collection

Comprehensive system performance monitoring from `/proc` and `/sys` filesystems, including CPU, memory, disk, and network metrics.

## Contributing

If you want to contribute, refer to our [DEVELOPING](./docs/DEVELOPING.md) docs.

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
