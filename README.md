# proxkube

Orchestrate Proxmox LXC containers as Kubernetes-like pods.

`proxkube` is a CLI tool that lets you manage Proxmox VE LXC containers using
familiar Kubernetes pod concepts — declare your desired state in a YAML manifest
and let proxkube handle creation, start-up, status, and teardown through the
Proxmox REST API or low-level hypervisor communication. It supports Proxmox 9
OCI images, multi-network configurations, port exposure rules, deploying full
stacks from Docker Compose files, Kubernetes operator compatibility (CRD), Helm
chart deployments, a Proxmox VE dashboard plugin, and direct hypervisor
communication via Unix sockets and the `pct` CLI.

## Features

- **Proxmox 9 OCI image support** — use OCI container images from Docker Hub or any OCI-compliant registry directly as LXC containers.
- **Declarative YAML manifests** — describe pods with resources (CPU, memory, disk, network) in a Kubernetes-style YAML file.
- **Network exposure & routing** — control whether pods are exposed externally or kept internal, with port-forwarding rules.
- **Multi-network support** — attach pods to multiple named networks mapped to Proxmox bridges or SDN zones.
- **Docker Compose support** — deploy full stacks from `compose.yaml` files with `proxkube compose up`.
- **Kubernetes operator compatibility** — `ProxKubePod` CRD lets you manage Proxmox LXC containers from Kubernetes using `kubectl`.
- **Helm chart support** — deploy from Helm values files with `proxkube helm install`, or install the operator into K8s with the bundled Helm chart.
- **Proxmox VE dashboard plugin** — web UI plugin that adds a "ProxKube Pods" panel to the Proxmox dashboard, showing all proxkube-managed containers with status, tags, resources, and lifecycle controls (start/stop/delete).
- **Low-level hypervisor communication** — `pkg/hypervisor` communicates directly with the PVE daemon via its Unix socket (`/var/run/pvedaemon/socket`) and uses the `pct` CLI for LXC operations, bypassing the REST API for maximum performance on the host.
- **Container exec** — run commands inside containers via `proxkube exec <vmid> -- <command>` using low-level `pct exec`.
- **Tags & dashboard visibility** — tag containers with custom labels visible in the Proxmox dashboard; auto-generated descriptions and the `proxkube` tag ensure containers are always identifiable.
- **Resource pool support** — assign containers to Proxmox resource pools for organisation and access control.
- **Storage mount points** — attach additional Proxmox storage pools as mount points inside containers.
- **Full LXC lifecycle** — create, start, stop, and delete containers via a single CLI.
- **Auto VMID allocation** — automatically picks the next available VMID when none is specified.
- **Dependency ordering** — respects `depends_on` to start pods in the correct order.
- **Token & ticket auth** — supports both API tokens and username/password authentication.
- **Monitoring daemon** — watches Proxmox for configuration changes (container additions, removals, status changes) and manages monitoring via the Kubernetes Prometheus operator.
- **Kubernetes engine integration** — supports minikube (single-node) and kubeadm (multi-node) for running monitoring workloads.
- **CI/CD** — GitHub Actions workflows for continuous integration and release automation with Debian package building.
- **Debian packaging** — installable via `apt` for seamless upgrades on Proxmox hosts.

## Installation

### From source

```bash
go install github.com/GothShoot/proxkube/cmd/proxkube@latest
```

Or build from source:

```bash
git clone https://github.com/GothShoot/proxkube.git
cd proxkube
go build -o proxkube ./cmd/proxkube
```

### Install script (one command)

The install script builds proxkube, installs the binary, the systemd service,
the PVE dashboard plugin, enables local mode (no API tokens required), and
starts the daemon — all in a single command:

```bash
sudo ./scripts/install.sh
```

Everything is ready immediately — the daemon runs and the plugin appears in
the Proxmox web interface after a page reload.

To uninstall everything in one command:

```bash
sudo ./scripts/uninstall.sh
```

### Debian package (apt)

Add the ProxKube APT repository and install with a single command:

```bash
echo "deb [trusted=yes] https://gothshoot.github.io/proxkube stable main" \
  | sudo tee /etc/apt/sources.list.d/proxkube.list \
  && sudo apt update && sudo apt install -y proxkube
```

> **Note:** The `[trusted=yes]` option skips GPG signature verification.
> This is acceptable for internal use. For production environments, a
> GPG-signed repository is recommended — see the project wiki for details.

The install configures local mode automatically (`PROXMOX_LOCAL=true`) so
the daemon uses the PVE Unix socket and `pct` CLI — no API tokens needed.
The PVE dashboard plugin is deployed and the daemon is started.

Future updates are pulled in with the standard Proxmox upgrade workflow:

```bash
sudo apt update && sudo apt full-upgrade
```

To remove everything:

```bash
sudo apt remove proxkube
```

Alternatively, download the `.deb` package directly from the
[releases page](https://github.com/GothShoot/proxkube/releases):

```bash
sudo apt install ./proxkube_0.5.0_amd64.deb
```

## Configuration

Set the following environment variables to connect to your Proxmox instance:

| Variable | Description |
|---|---|
| `PROXMOX_URL` | Proxmox API URL (e.g. `https://proxmox.example.com:8006`) |
| `PROXMOX_TOKEN_ID` | API token ID (e.g. `root@pam!mytoken`) |
| `PROXMOX_SECRET` | API token secret |
| `PROXMOX_USER` | Username for ticket auth (alternative to token) |
| `PROXMOX_PASSWORD` | Password for ticket auth |
| `PROXMOX_INSECURE` | (Development only) Set to `true` to temporarily skip TLS verification. Not recommended for production; instead configure a trusted CA or pinned certificate. |
| `PROXMOX_NODE` | Default Proxmox node for compose (default: `pve`) |
| `PROXMOX_STORAGE` | Default storage for compose (default: `local-lvm`) |
| `PROXMOX_BRIDGE` | Default network bridge for compose (default: `vmbr0`) |
| `PROXMOX_POOL` | Default resource pool for containers |
| `PROXMOX_TAGS` | Comma-separated default tags for containers |
| `PROXMOX_LOCAL` | Set to `true` to use low-level hypervisor communication (Unix socket + pct CLI) instead of the REST API. Only works on the Proxmox host. |
| `PROXKUBE_POLL_INTERVAL` | Daemon poll interval (default: `30s`) |
| `PROXKUBE_NODES` | Comma-separated list of nodes the daemon monitors (default: value of `PROXMOX_NODE`) |
| `PROXKUBE_K8S_MODE` | Kubernetes engine mode: `minikube` or `kubeadm` (default: `minikube`) |
| `PROXKUBE_K8S_NAMESPACE` | Kubernetes namespace for proxkube workloads (default: `proxkube-system`) |

## Usage

### Define a pod manifest (OCI image)

```yaml
apiVersion: proxkube/v1
kind: Pod
metadata:
  name: my-web-server
  labels:
    app: nginx
spec:
  node: pve
  image: docker.io/library/nginx:latest   # Proxmox 9 OCI image
  expose: true
  ports:
    - hostPort: 8080
      containerPort: 80
  networks:
    - name: frontend
      bridge: vmbr0
      ip: dhcp
  tags:
    - web
    - production
  pool: web-servers
  description: "NGINX web server"
  resources:
    cpu: 2
    memory: 512
    disk: 8
    storage: local-lvm
  mountPoints:
    - storage: local-lvm
      size: 10
      mountPath: /mnt/data
```

You can also use a traditional LXC template with `osTemplate` instead of `image`:

```yaml
spec:
  osTemplate: "local:vztmpl/ubuntu-22.04-standard_22.04-1_amd64.tar.zst"
```

### Apply (create & start)

```bash
proxkube apply -f examples/pod.yaml
```

### Get pod status

```bash
proxkube get -f examples/pod.yaml
proxkube get -f examples/pod.yaml -json   # JSON output
```

### List all pods on a node

```bash
proxkube list --node pve
```

### Delete a pod

```bash
proxkube delete -f examples/pod.yaml
```

### Deploy a stack from Docker Compose

```bash
proxkube compose up   -f examples/compose.yaml
proxkube compose ps   -f examples/compose.yaml
proxkube compose down -f examples/compose.yaml
```

Example `compose.yaml`:

```yaml
services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
    networks:
      - frontend
    depends_on:
      - db

  db:
    image: postgres:16
    environment:
      POSTGRES_USER: myuser
      POSTGRES_DB: mydb
    volumes:
      - db_data:/var/lib/postgresql/data
    networks:
      - backend

networks:
  frontend: {}
  backend:
    internal: true

volumes:
  db_data: {}
```

### Deploy from Helm values

proxkube supports Helm-style deployments using a `values.yaml` file that defines
pods, resources, networks, and dependencies.

```bash
# Deploy pods from Helm values
proxkube helm install myrelease -f examples/helm-values.yaml

# Render manifests without deploying (like helm template)
proxkube helm template myrelease -f examples/helm-values.yaml

# Remove all pods from a release
proxkube helm uninstall myrelease --node pve
```

Example `helm-values.yaml`:

```yaml
global:
  node: pve
  storage: local-lvm
  pool: web-pool
  tags: [production]

pods:
  web:
    image: nginx:latest
    expose: true
    ports:
      - hostPort: 8080
        containerPort: 80
    resources:
      cpu: 2
      memory: 1024
      disk: 10
    dependsOn: [api]

  api:
    image: node:20-slim
    resources:
      cpu: 2
      memory: 512
      disk: 8
    dependsOn: [db]

  db:
    image: postgres:16
    mountPoints:
      - storage: local-lvm
        size: 20
        mountPath: /var/lib/postgresql/data
        backup: true
```

### Kubernetes Operator (CRD)

proxkube provides a Kubernetes CRD (`ProxKubePod`) so you can manage Proxmox LXC
containers using `kubectl` and standard Kubernetes operators.

```bash
# Print the CRD manifest (apply it to your K8s cluster)
proxkube operator crd | kubectl apply -f -

# Then create ProxKubePod resources
kubectl apply -f examples/proxkubepod.yaml
kubectl get proxkubepods
kubectl get pkp   # short name
```

Example `ProxKubePod` resource:

```yaml
apiVersion: proxkube.io/v1
kind: ProxKubePod
metadata:
  name: my-nginx
spec:
  node: pve
  image: docker.io/library/nginx:latest
  expose: true
  pool: web-servers
  tags: [web, production]
  ports:
    - hostPort: 8080
      containerPort: 80
  resources:
    cpu: 2
    memory: 512
    disk: 8
    storage: local-lvm
```

### Helm Chart (deploy the operator into K8s)

A Helm chart is provided in `deploy/helm/proxkube/` to install the proxkube
operator into a Kubernetes cluster.

```bash
# Install the operator with Helm
helm install proxkube deploy/helm/proxkube/ \
  --set proxmox.url=https://proxmox:8006 \
  --set proxmox.tokenId=root@pam!mytoken \
  --set proxmox.secret=my-secret

# Upgrade
helm upgrade proxkube deploy/helm/proxkube/

# Uninstall
helm uninstall proxkube
```

### PVE Dashboard Plugin

proxkube includes a Proxmox VE dashboard plugin that adds a "ProxKube Pods"
panel to the web interface. The plugin shows all proxkube-managed containers
(filtered by the `proxkube` tag) with real-time status, CPU/memory metrics,
tags, IPs, and lifecycle controls (start, stop, delete).

When installed via `apt` or `scripts/install.sh`, the plugin is set up
automatically — no extra steps are needed.

For a manual installation:

```bash
make -C deploy/pve-plugin install
```

Then reload the Proxmox web interface. The "ProxKube Pods" panel will appear
in the datacenter view.

#### Multi-node deployment

The install process copies plugin assets to `/etc/pve/proxkube/` — the
Proxmox cluster filesystem that is shared across all nodes. To deploy the
plugin on another node, copy the files from there:

```bash
# On any other node in the same cluster:
mkdir -p /usr/share/pve-manager/proxkube
mkdir -p /usr/share/perl5/PVE/API2
cp /etc/pve/proxkube/ProxKubePanel.js /usr/share/pve-manager/proxkube/
cp /etc/pve/proxkube/proxkube.css      /usr/share/pve-manager/proxkube/
cp /etc/pve/proxkube/ProxKube.pm       /usr/share/perl5/PVE/API2/ProxKube.pm
systemctl restart pvedaemon pveproxy
```

Or simply install the `proxkube` package on each node — `apt` will handle
the setup identically.

### Low-Level Hypervisor Mode

When running directly on the Proxmox host, proxkube can bypass the REST API
and communicate with the hypervisor at a lower level:

- **PVE daemon Unix socket** (`/var/run/pvedaemon/socket`) for read operations
  (listing containers, getting status, fetching next VMID)
- **`pct` CLI** for write operations (create, start, stop, destroy containers)

This mode is enabled automatically when proxkube is installed via `apt` or
`scripts/install.sh` (`PROXMOX_LOCAL=true` in `/etc/default/proxkube`). No
API tokens are needed.

To enable it manually:

```bash
export PROXMOX_LOCAL=true
proxkube apply -f pod.yaml   # Uses pct + Unix socket instead of REST API
proxkube list --node pve     # Reads directly from PVE daemon socket
```

### Execute Commands Inside Containers

Run commands inside a running container using `proxkube exec`:

```bash
proxkube exec 100 -- ls -la /
proxkube exec 100 -- cat /etc/hostname
```

This uses `pct exec` under the hood and requires running on the Proxmox host.

### Monitoring Daemon

proxkube includes a monitoring daemon that continuously watches the Proxmox
cluster for configuration changes (container additions, removals, status
changes) and integrates with the Kubernetes Prometheus operator for monitoring.

```bash
# Run the daemon in the foreground
proxkube daemon

# Deploy Prometheus monitoring stack to Kubernetes and start watching
proxkube daemon --setup-monitoring

# Run as a systemd service (installed by install.sh or the .deb package)
sudo systemctl enable --now proxkube-daemon
```

The daemon polls every configured Proxmox node at a configurable interval
(default 30s) and emits change events. When `--setup-monitoring` is used,
it deploys Prometheus ServiceMonitor resources to the Kubernetes cluster
to scrape proxkube metrics.

Environment variables for the daemon:

```bash
export PROXKUBE_POLL_INTERVAL=15s       # Poll more frequently
export PROXKUBE_NODES=pve1,pve2,pve3    # Monitor multiple nodes
export PROXKUBE_K8S_MODE=kubeadm        # Use kubeadm for multi-node clusters
export PROXKUBE_K8S_NAMESPACE=monitoring # Custom K8s namespace
```

### Kubernetes Engine

proxkube supports two Kubernetes deployment modes for running monitoring
and operator workloads:

- **minikube** (default) — single-node cluster ideal for standalone Proxmox hosts
- **kubeadm** — multi-node cluster for Proxmox clusters

The Kubernetes engine is used by the daemon to deploy and manage the
Prometheus operator and related monitoring resources.

## Project Structure

```
cmd/proxkube/           CLI entrypoint (includes daemon command)
pkg/api/                Pod & Stack data models, validation
pkg/proxmox/            Proxmox VE REST API client
pkg/hypervisor/         Low-level hypervisor client (Unix socket + pct CLI)
pkg/controller/         Pod lifecycle & stack orchestration controller
pkg/compose/            Docker Compose file parser & converter
pkg/helm/               Helm values parser & converter
pkg/operator/           Kubernetes operator reconciler & CRD
pkg/daemon/             Monitoring daemon (Proxmox change detection)
pkg/k8s/                Kubernetes engine integration (minikube / kubeadm)
deploy/crds/            Kubernetes CRD YAML
deploy/helm/proxkube/   Helm chart for deploying the operator into K8s
deploy/pve-plugin/      Proxmox VE dashboard plugin (JS + CSS + Perl)
debian/                 Debian packaging files for apt-based installation
scripts/                Install/uninstall scripts and systemd service
examples/               Example YAML manifests, compose & Helm values files
.github/workflows/      CI/CD workflows (GitHub Actions)
```

## Testing

```bash
go test ./...
```

## License

MIT