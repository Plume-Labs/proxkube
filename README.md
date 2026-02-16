# proxkube

Orchestrate Proxmox LXC containers as Kubernetes-like pods.

`proxkube` is a CLI tool that lets you manage Proxmox VE LXC containers using
familiar Kubernetes pod concepts — declare your desired state in a YAML manifest
and let proxkube handle creation, start-up, status, and teardown through the
Proxmox REST API.

## Features

- **Declarative YAML manifests** — describe pods with resources (CPU, memory, disk, network) in a Kubernetes-style YAML file.
- **Full LXC lifecycle** — create, start, stop, and delete containers via a single CLI.
- **Auto VMID allocation** — automatically picks the next available VMID when none is specified.
- **Status & IP reporting** — retrieve runtime status and IP addresses.
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

## Usage

### Define a pod manifest

```yaml
apiVersion: proxkube/v1
kind: Pod
metadata:
  name: my-web-server
  labels:
    app: nginx
spec:
  node: pve
  osTemplate: "local:vztmpl/ubuntu-22.04-standard_22.04-1_amd64.tar.zst"
  unprivileged: true
  startOnBoot: true
  resources:
    cpu: 2
    memory: 512
    disk: 8
    storage: local-lvm
    network:
      bridge: vmbr0
      ip: dhcp
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

## Project Structure

```
cmd/proxkube/         CLI entrypoint
pkg/api/              Pod data model & validation
pkg/proxmox/          Proxmox VE REST API client
pkg/controller/       Pod lifecycle controller
examples/             Example YAML manifests
```

## Testing

```bash
go test ./...
```

## License

MIT