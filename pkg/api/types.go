// Package api defines the Pod-like data model used by proxkube to map
// Kubernetes pod concepts onto Proxmox LXC containers.
package api

import "strconv"

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
	// Image is an OCI image reference for Proxmox 9+ (e.g. "docker.io/library/nginx:latest").
	// When set, it takes precedence over OSTemplate.
	Image string `json:"image,omitempty" yaml:"image,omitempty"`
	// OSTemplate is the LXC template to use (e.g. "local:vztmpl/ubuntu-22.04-standard_22.04-1_amd64.tar.zst").
	OSTemplate string `json:"osTemplate,omitempty" yaml:"osTemplate,omitempty"`
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
	// Ports defines port-forwarding rules to expose the pod externally.
	Ports []PortMapping `json:"ports,omitempty" yaml:"ports,omitempty"`
	// Networks lists named networks the pod should join.
	Networks []PodNetwork `json:"networks,omitempty" yaml:"networks,omitempty"`
	// Expose controls whether the pod is reachable from outside (default false = internal only).
	Expose bool `json:"expose,omitempty" yaml:"expose,omitempty"`
	// Environment variables to set inside the container.
	Environment map[string]string `json:"environment,omitempty" yaml:"environment,omitempty"`
	// Command overrides the container entrypoint.
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
	// Volumes describes bind or named volume mounts.
	Volumes []VolumeMount `json:"volumes,omitempty" yaml:"volumes,omitempty"`
	// DependsOn lists pod names that must be running before this pod starts.
	DependsOn []string `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
}

// PortMapping maps a host port to a container port, similar to Docker's port publishing.
type PortMapping struct {
	// HostPort is the port on the Proxmox host.
	HostPort int `json:"hostPort" yaml:"hostPort"`
	// ContainerPort is the port inside the container.
	ContainerPort int `json:"containerPort" yaml:"containerPort"`
	// Protocol is "tcp" or "udp" (default "tcp").
	Protocol string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
}

// PodNetwork attaches the pod to a named network.
type PodNetwork struct {
	// Name is the logical network name (maps to a Proxmox bridge or SDN zone).
	Name string `json:"name" yaml:"name"`
	// Bridge is the Proxmox bridge for this network (e.g. "vmbr0").
	Bridge string `json:"bridge,omitempty" yaml:"bridge,omitempty"`
	// IP address in CIDR notation or "dhcp".
	IP string `json:"ip,omitempty" yaml:"ip,omitempty"`
	// Gateway for this network interface.
	Gateway string `json:"gateway,omitempty" yaml:"gateway,omitempty"`
	// Firewall enables the Proxmox firewall for this interface.
	Firewall bool `json:"firewall,omitempty" yaml:"firewall,omitempty"`
}

// VolumeMount describes a volume or bind mount into the container.
type VolumeMount struct {
	// Name is the volume name (for named volumes) or host path (for bind mounts).
	Name string `json:"name" yaml:"name"`
	// MountPath is the path inside the container.
	MountPath string `json:"mountPath" yaml:"mountPath"`
	// ReadOnly makes the mount read-only.
	ReadOnly bool `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
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
	// Network configuration (single interface, for backward compatibility).
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

// IsOCI returns true if the pod spec uses an OCI image reference.
func (s *PodSpec) IsOCI() bool {
	return s.Image != ""
}

// EffectiveTemplate returns the ostemplate value to pass to the Proxmox API.
// For OCI images it returns the image reference directly; for traditional
// templates it returns the OSTemplate field.
func (s *PodSpec) EffectiveTemplate() string {
	if s.Image != "" {
		return s.Image
	}
	return s.OSTemplate
}

// Validate performs basic validation on a Pod spec.
func (p *Pod) Validate() error {
	if p.Metadata.Name == "" {
		return &ValidationError{Field: "metadata.name", Message: "must not be empty"}
	}
	if p.Spec.Node == "" {
		return &ValidationError{Field: "spec.node", Message: "must not be empty"}
	}
	if p.Spec.Image == "" && p.Spec.OSTemplate == "" {
		return &ValidationError{Field: "spec.image", Message: "either image or osTemplate must be set"}
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
	for i, pm := range p.Spec.Ports {
		if pm.ContainerPort <= 0 {
			return &ValidationError{
				Field:   "spec.ports[" + strconv.Itoa(i) + "].containerPort",
				Message: "must be greater than 0",
			}
		}
		if pm.Protocol != "" && pm.Protocol != "tcp" && pm.Protocol != "udp" {
			return &ValidationError{
				Field:   "spec.ports[" + strconv.Itoa(i) + "].protocol",
				Message: "must be tcp or udp",
			}
		}
	}
	return nil
}

// Stack represents a collection of pods deployed together, similar to
// a Docker Compose project.
type Stack struct {
	Name     string            `json:"name"               yaml:"name"`
	Pods     []Pod             `json:"pods"               yaml:"pods"`
	Networks map[string]Network `json:"networks,omitempty" yaml:"networks,omitempty"`
}

// Network defines a named network within a stack.
type Network struct {
	// Bridge is the Proxmox bridge backing this network.
	Bridge string `json:"bridge" yaml:"bridge"`
	// Subnet in CIDR notation (optional, for documentation / SDN).
	Subnet string `json:"subnet,omitempty" yaml:"subnet,omitempty"`
	// Gateway for the network.
	Gateway string `json:"gateway,omitempty" yaml:"gateway,omitempty"`
	// Internal when true means pods on this network are not exposed externally.
	Internal bool `json:"internal,omitempty" yaml:"internal,omitempty"`
}

// ValidationError represents a validation failure on a specific field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
