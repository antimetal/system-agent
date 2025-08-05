# Developing

The project uses Make to manage common operations for developing, building, and distributing the project.
At the root of the repo, run `make help` to see all commands.

## Prerequisites

Some system prerequisites are required that our Make system depends on.
Ensure that these prerequisites are installed on your machine.
Use the links for installation steps for each prerequisite as neccessary.

- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Docker](https://docs.docker.com/engine/install/)

## Initial Setup

After cloning the repository, install the shared git hooks for optimal development workflow:

```bash
make install-hooks
```

This sets up git hooks that automatically configure Claude Code context when working with git worktrees for PR reviews. See [.githooks/README.md](.githooks/README.md) for details.

### Docker setup

There are a couple things you'll want to configure for your Docker setup:
- Make sure you can run docker commands as a non-root user i.e without sudo. See the [Docker docs](https://docs.docker.com/engine/security/rootless/) for instructions.

- Use the containerd image store. Under the hood, Docker uses containerd, a container runtime to run and manage containers but Docker can also be configured to use containerd to store image snapshots as well.
By default, Docker uses its classic storage driver.
Using containerd snapshotters is helpful for managing multiarch builds locally (i.e multiarch builds can be visible under `docker images`).
In order to configure Docker to use containerd snapshotters, follow the steps below according to your platform:

  - **Linux**: Add the following to `etc/docker/daemon.json`:
    ```json
    {
      "features": {
       "containerd-snapshotter": true
      }
    }
    ```
    Save the file and restart the Docker daemon:
    ```
    sudo systemctl restart docker
    ```
  - **Mac/Windows**: Navigate to the Docker Desktop settings and check to use containerd for storing images under the General tab.

## Creating Dev Cluster

The project uses [Kind](https://kind.sigs.k8s.io/) to deploy a Kubernetes cluster inside a Docker container for testing and development.
You can start a cluster by running:
```
make cluster
```
This will start a new Kind cluster and add it to your Kube config (`~/.kube/config`). If you want to change the name of the cluster created you can use the `KIND_CLUSTER` setting:
```
KIND_CLUSTER=foo make cluster // creates foo cluster
```

## Building

In order to build the project locally run:
```
make build
```
OR
```
make
```
This will build the binary in the `bin/` directory

To build the docker image run:
```
make docker-build
```

Make will automatically select your machine's platform for building Docker images.
So for example if you're on M1 Macs, then the Docker build will use `linux/arm64` as the platform.
For multi-arch builds you can use the `BUILD_PLATFORMS` settings to specifiy multiple platforms:
```
BUILD_PLATFORMS=linux/amd64,linux/arm64 make docker-build
```
If using containerd for storing images is enabled, both images will show up in `docker images`
```
docker images amagent
REPOSITORY   TAG       IMAGE ID       SIZE
amagent      latest    c8e1fbfaadce   81.8MB
amagent      latest    c8e1fbfaadce   85.2MB
```

## Deploying to Cluster
Once the docker image is built, run the following commands to load the image to the cluster and deploy:
```
make load-image
make deploy
```

You can verify that the deployment succeeded by checking the agent pods:
```
kubectl get pod -n antimetal-system
NAME                               READY   STATUS    RESTARTS
antimetal-agent-7d7dcccbcb-gj4s8   2/2     Running
```
Once ready you can view the logs:
```
kubectl logs -n antimetal-system $(kubectl get pod -n antimetal-system | awk '{print $1}' | tail -n 1)
```

## Developing, Building, and Loading a New Image

As you're developing, you can deploy a new image using:
```
make build-and-load-image
```
This will build a new image with your changes, load that image into the cluster, and restart the agent pods to use the new build.

## Cleanup

- Undeploy the agent from the cluster:
  ```
  make undeploy
  ```

- Destroy the cluster:
  ```
  make destroy-cluster
  ```
  If you used a different name when creating the cluster, make sure to pass in the `KIND_CLUSTER` when destroying as well:
  ```
  KIND_CLUSTER=foo make destroy-cluster // destroy foo cluster
  ```
