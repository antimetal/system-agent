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

### Main Project
The source code is licensed using PolyForm Shield.
If you are an end user, broadly you can do whatever you want with this code.
See the License FAQ for more information.

### eBPF Programs
The eBPF programs in the `ebpf/` directory are dual-licensed under GPLv2 and BSD 2-Clause licenses.
This dual licensing is required for kernel compatibility while allowing permissive downstream use.
See [ebpf/LICENSE](ebpf/LICENSE) for details.
