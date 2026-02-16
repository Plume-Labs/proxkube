# proxkube

Orchestrate Proxmox LXC containers as Kubernetes-like pods.

`proxkube` is a CLI tool that lets you manage Proxmox VE LXC containers using
familiar Kubernetes pod concepts — declare your desired state in a YAML manifest
and let proxkube handle creation, start-up, status, and teardown through the
Proxmox REST API. It supports Proxmox 9 OCI images, multi-network
configurations, port exposure rules, and deploying full stacks from Docker
Compose files.

## Features

- **Proxmox 9 OCI image support** — use OCI container images from Docker Hub or any OCI-compliant registry directly as LXC containers.
- **Declarative YAML manifests** — describe pods with resources (CPU, memory, disk, network) in a Kubernetes-style YAML file.
- **Network exposure & routing** — control whether pods are exposed externally or kept internal, with port-forwarding rules.
- **Multi-network support** — attach pods to multiple named networks mapped to Proxmox bridges or SDN zones.
- **Docker Compose support** — deploy full stacks from `compose.yaml` files with `proxkube compose up`.
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
| `PROXMOX_INSECURE` | Set to `true` to skip TLS verification |
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

## Project Structure

```
cmd/proxkube/         CLI entrypoint
pkg/api/              Pod & Stack data models, validation
pkg/proxmox/          Proxmox VE REST API client
pkg/controller/       Pod lifecycle & stack orchestration controller
pkg/compose/          Docker Compose file parser & converter
examples/             Example YAML manifests & compose files
```

## Testing

```bash
go test ./...
```

## License

MIT