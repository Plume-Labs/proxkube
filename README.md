# proxkube

Orchestrate Proxmox LXC containers as Kubernetes-like pods.

`proxkube` is a CLI tool that lets you manage Proxmox VE LXC containers using
familiar Kubernetes pod concepts — declare your desired state in a YAML manifest
and let proxkube handle creation, start-up, status, and teardown through the
Proxmox REST API. It supports Proxmox 9 OCI images, multi-network
configurations, port exposure rules, deploying full stacks from Docker Compose
files, Kubernetes operator compatibility (CRD), and Helm chart deployments.

## Features

- **Proxmox 9 OCI image support** — use OCI container images from Docker Hub or any OCI-compliant registry directly as LXC containers.
- **Declarative YAML manifests** — describe pods with resources (CPU, memory, disk, network) in a Kubernetes-style YAML file.
- **Network exposure & routing** — control whether pods are exposed externally or kept internal, with port-forwarding rules.
- **Multi-network support** — attach pods to multiple named networks mapped to Proxmox bridges or SDN zones.
- **Docker Compose support** — deploy full stacks from `compose.yaml` files with `proxkube compose up`.
- **Kubernetes operator compatibility** — `ProxKubePod` CRD lets you manage Proxmox LXC containers from Kubernetes using `kubectl`.
- **Helm chart support** — deploy from Helm values files with `proxkube helm install`, or install the operator into K8s with the bundled Helm chart.
- **Tags & dashboard visibility** — tag containers with custom labels visible in the Proxmox dashboard; auto-generated descriptions and the `proxkube` tag ensure containers are always identifiable.
- **Resource pool support** — assign containers to Proxmox resource pools for organisation and access control.
- **Storage mount points** — attach additional Proxmox storage pools as mount points inside containers.
- **Full LXC lifecycle** — create, start, stop, and delete containers via a single CLI.
- **Auto VMID allocation** — automatically picks the next available VMID when none is specified.
- **Dependency ordering** — respects `depends_on` to start pods in the correct order.
- **Token & ticket auth** — supports both API tokens and username/password authentication.

## Installation

```bash
go install github.com/GothShoot/proxkube/cmd/proxkube@latest
```

Or build from source:

```bash
git clone https://github.com/GothShoot/proxkube.git
cd proxkube
go build -o proxkube ./cmd/proxkube
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

## Project Structure

```
cmd/proxkube/         CLI entrypoint
pkg/api/              Pod & Stack data models, validation
pkg/proxmox/          Proxmox VE REST API client
pkg/controller/       Pod lifecycle & stack orchestration controller
pkg/compose/          Docker Compose file parser & converter
pkg/helm/             Helm values parser & converter
pkg/operator/         Kubernetes operator reconciler & CRD
deploy/crds/          Kubernetes CRD YAML
deploy/helm/proxkube/ Helm chart for deploying the operator into K8s
examples/             Example YAML manifests, compose & Helm values files
```

## Testing

```bash
go test ./...
```

## License

MIT