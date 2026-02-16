// Package api defines the Pod-like data model used by proxkube to map
// Kubernetes pod concepts onto Proxmox LXC containers.
package api

// Pod represents a Kubernetes-style pod backed by a Proxmox LXC container.
type Pod struct {
	APIVersion string   `json:"apiVersion" yaml:"apiVersion"`
	Kind       string   `json:"kind"       yaml:"kind"`
	Metadata   Metadata `json:"metadata"   yaml:"metadata"`
	Spec       PodSpec  `json:"spec"       yaml:"spec"`
	Status     Status   `json:"status,omitempty" yaml:"status,omitempty"`
}

// Metadata holds identifying information about a pod.
type Metadata struct {
	Name      string            `json:"name"                yaml:"name"`
	Namespace string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"    yaml:"labels,omitempty"`
}

// PodSpec describes the desired state of the LXC container.
type PodSpec struct {
	// Node is the Proxmox node on which the container runs.
	Node string `json:"node" yaml:"node"`
	// VMID is the Proxmox container ID (optional; auto-assigned when 0).
	VMID int `json:"vmid,omitempty" yaml:"vmid,omitempty"`
	// OSTemplate is the LXC template to use (e.g. "local:vztmpl/ubuntu-22.04-standard_22.04-1_amd64.tar.zst").
	OSTemplate string `json:"osTemplate" yaml:"osTemplate"`
	// Resources describes CPU, memory, disk and network resources.
	Resources Resources `json:"resources" yaml:"resources"`
	// Hostname sets the container hostname.
	Hostname string `json:"hostname,omitempty" yaml:"hostname,omitempty"`
	// Password for the root account (optional).
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
	// SSHPublicKeys to inject into the container (optional).
	SSHPublicKeys string `json:"sshPublicKeys,omitempty" yaml:"sshPublicKeys,omitempty"`
	// StartOnBoot indicates whether the container should start on boot.
	StartOnBoot bool `json:"startOnBoot,omitempty" yaml:"startOnBoot,omitempty"`
	// Unprivileged indicates whether the container runs unprivileged.
	Unprivileged bool `json:"unprivileged,omitempty" yaml:"unprivileged,omitempty"`
	// Nameserver sets the DNS nameserver for the container.
	Nameserver string `json:"nameserver,omitempty" yaml:"nameserver,omitempty"`
	// SearchDomain sets the DNS search domain.
	SearchDomain string `json:"searchDomain,omitempty" yaml:"searchDomain,omitempty"`
}

// Resources defines compute, storage and network resources for a container.
type Resources struct {
	// CPU is the number of CPU cores.
	CPU int `json:"cpu" yaml:"cpu"`
	// Memory in megabytes.
	Memory int `json:"memory" yaml:"memory"`
	// Swap in megabytes (optional).
	Swap int `json:"swap,omitempty" yaml:"swap,omitempty"`
	// Disk in gigabytes on the given storage.
	Disk int `json:"disk" yaml:"disk"`
	// Storage name in Proxmox (e.g. "local-lvm").
	Storage string `json:"storage" yaml:"storage"`
	// Network configuration.
	Network *NetworkConfig `json:"network,omitempty" yaml:"network,omitempty"`
}

// NetworkConfig describes the network interface for the container.
type NetworkConfig struct {
	// Bridge is the Proxmox bridge (e.g. "vmbr0").
	Bridge string `json:"bridge" yaml:"bridge"`
	// IP address in CIDR notation (e.g. "dhcp" or "192.168.1.100/24").
	IP string `json:"ip" yaml:"ip"`
	// Gateway IP address.
	Gateway string `json:"gateway,omitempty" yaml:"gateway,omitempty"`
	// Firewall enables the Proxmox firewall for this interface.
	Firewall bool `json:"firewall,omitempty" yaml:"firewall,omitempty"`
}

// Phase represents the lifecycle phase of a pod.
type Phase string

const (
	PhasePending  Phase = "Pending"
	PhaseRunning  Phase = "Running"
	PhaseStopped  Phase = "Stopped"
	PhaseFailed   Phase = "Failed"
	PhaseUnknown  Phase = "Unknown"
)

// Status represents the observed state of the pod.
type Status struct {
	Phase Phase  `json:"phase"            yaml:"phase"`
	VMID  int    `json:"vmid,omitempty"   yaml:"vmid,omitempty"`
	Node  string `json:"node,omitempty"   yaml:"node,omitempty"`
	IP    string `json:"ip,omitempty"     yaml:"ip,omitempty"`
}

// Validate performs basic validation on a Pod spec.
func (p *Pod) Validate() error {
	if p.Metadata.Name == "" {
		return &ValidationError{Field: "metadata.name", Message: "must not be empty"}
	}
	if p.Spec.Node == "" {
		return &ValidationError{Field: "spec.node", Message: "must not be empty"}
	}
	if p.Spec.OSTemplate == "" {
		return &ValidationError{Field: "spec.osTemplate", Message: "must not be empty"}
	}
	if p.Spec.Resources.CPU <= 0 {
		return &ValidationError{Field: "spec.resources.cpu", Message: "must be greater than 0"}
	}
	if p.Spec.Resources.Memory <= 0 {
		return &ValidationError{Field: "spec.resources.memory", Message: "must be greater than 0"}
	}
	if p.Spec.Resources.Disk <= 0 {
		return &ValidationError{Field: "spec.resources.disk", Message: "must be greater than 0"}
	}
	if p.Spec.Resources.Storage == "" {
		return &ValidationError{Field: "spec.resources.storage", Message: "must not be empty"}
	}
	return nil
}

// ValidationError represents a validation failure on a specific field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
