# Antimetal Agent

Component that connects your infrastructure to the [Antimetal](https://antimetal.com) platform.

## Documentation

📚 **[View the complete documentation in our Wiki](https://github.com/antimetal/agent/wiki)**

The wiki contains comprehensive guides for:
- Architecture and system design
- Installation and configuration
- Performance collectors reference
- Hardware discovery features
- Testing and development guides

## Contributing

If you want to contribute, refer to our [DEVELOPING](./docs/DEVELOPING.md) docs.

### Developer Quick References

The `/docs` directory contains quick references for developers:
- [DEVELOPING.md](./docs/DEVELOPING.md) - Development setup and workflow
- [performance-collectors.md](./docs/performance-collectors.md) - Collector implementation guide
- [ebpf-development.md](./docs/ebpf-development.md) - eBPF program development
- [COMMIT_MESSAGE_GUIDELINES.md](./docs/COMMIT_MESSAGE_GUIDELINES.md) - Commit conventions

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
