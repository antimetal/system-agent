# Antimetal Agent

Component that connects your infrastructure to the [Antimetal](https://antimetal.com) platform.

## Contributing

If you want to contribute, refer to our [DEVELOPING](./DEVELOPING.md) docs.

## Helm chart

The helm chart for the Antimetal Agent is published in the [antimetal/helm-charts](https://github.com/antimetal/helm-charts) repo.

## Docker images

We publish Docker images for each new release on [DockerHub](https://hub.docker.com/r/antimetal/agent).
We build linux/amd64 and linux/arm64 images.

## Support
For questions and support feel free to post a Github Issue.
For commercial support, contact us at support@antimetal.com.

## License

This project uses split licensing:

- **Userspace components** (`/cmd`, `/internal`, `/pkg`): PolyForm Shield
- **eBPF programs** (`/ebpf`): GPL-2.0-only

The eBPF programs must be GPL-licensed due to their use of GPL-only kernel helper functions. 
This is a standard practice in the industry for projects that include both userspace and eBPF code.

If you are an end user, broadly you can do whatever you want with the userspace code under PolyForm Shield.
See the License FAQ for more information about PolyForm licensing.
