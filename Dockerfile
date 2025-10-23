# syntax=docker/dockerfile:1

# Use distroless as minimal base image to package the agent binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot

ARG TARGETOS
ARG TARGETARCH

COPY ${TARGETOS}/${TARGETARCH}/agent /agent

# Copy eBPF objects files
# These are expected at /usr/local/lib/antimetal/ebpf/ in the container
COPY --chown=65532:65532 ebpf/build/*.bpf.o /usr/local/lib/antimetal/ebpf/

USER 65532:65532

ENTRYPOINT ["/agent"]
