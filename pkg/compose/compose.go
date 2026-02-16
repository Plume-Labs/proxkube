// Package compose parses Docker Compose files and converts them into
// proxkube Stack/Pod definitions backed by Proxmox LXC containers.
//
// It supports the latest Compose Specification format (no version field
// required) with services, networks, volumes, ports, environment,
// depends_on, and deploy.resources.
package compose

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/GothShoot/proxkube/pkg/api"
)

// ComposeFile represents a Docker Compose file structure.
type ComposeFile struct {
	Services map[string]Service        `yaml:"services"`
	Networks map[string]ComposeNetwork `yaml:"networks,omitempty"`
	Volumes  map[string]interface{}    `yaml:"volumes,omitempty"`
}

// Service represents a single service in a Compose file.
type Service struct {
	Image       string            `yaml:"image"`
	Build       interface{}       `yaml:"build,omitempty"` // ignored for proxkube
	Command     interface{}       `yaml:"command,omitempty"`
	Ports       []string          `yaml:"ports,omitempty"`
	Environment interface{}       `yaml:"environment,omitempty"` // list or map
	Volumes     []string          `yaml:"volumes,omitempty"`
	Networks    interface{}       `yaml:"networks,omitempty"` // list or map
	DependsOn   interface{}       `yaml:"depends_on,omitempty"` // list or map
	Deploy      *DeployConfig     `yaml:"deploy,omitempty"`
	Hostname    string            `yaml:"hostname,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Restart     string            `yaml:"restart,omitempty"`
}

// ComposeNetwork represents a network definition in a Compose file.
type ComposeNetwork struct {
	Driver   string            `yaml:"driver,omitempty"`
	Internal bool              `yaml:"internal,omitempty"`
	IPAM     *IPAM             `yaml:"ipam,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty"`
}

// IPAM holds IP address management config for a network.
type IPAM struct {
	Config []IPAMConfig `yaml:"config,omitempty"`
}

// IPAMConfig holds a single IPAM pool configuration.
type IPAMConfig struct {
	Subnet  string `yaml:"subnet,omitempty"`
	Gateway string `yaml:"gateway,omitempty"`
}

// DeployConfig holds deploy-time resource configuration.
type DeployConfig struct {
	Resources *DeployResources `yaml:"resources,omitempty"`
}

// DeployResources holds resource limits/reservations.
type DeployResources struct {
	Limits       *ResourceSpec `yaml:"limits,omitempty"`
	Reservations *ResourceSpec `yaml:"reservations,omitempty"`
}

// ResourceSpec holds cpu/memory resource values.
type ResourceSpec struct {
	CPUs   string `yaml:"cpus,omitempty"`
	Memory string `yaml:"memory,omitempty"`
}

// ConvertOptions provides context for the conversion.
type ConvertOptions struct {
	// Node is the Proxmox node to deploy on.
	Node string
	// Storage is the Proxmox storage for container disks.
	Storage string
	// DefaultBridge is the default Proxmox bridge to use.
	DefaultBridge string
	// DefaultDiskGB is the default disk size when not specified.
	DefaultDiskGB int
	// DefaultMemoryMB is the default memory when not specified.
	DefaultMemoryMB int
	// DefaultCPU is the default CPU count when not specified.
	DefaultCPU int
}

// DefaultConvertOptions returns sensible defaults for conversion.
func DefaultConvertOptions() ConvertOptions {
	return ConvertOptions{
		Node:            "pve",
		Storage:         "local-lvm",
		DefaultBridge:   "vmbr0",
		DefaultDiskGB:   8,
		DefaultMemoryMB: 512,
		DefaultCPU:      1,
	}
}

// LoadComposeFile reads and parses a compose.yaml file.
func LoadComposeFile(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read compose file: %w", err)
	}
	var cf ComposeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}
	if len(cf.Services) == 0 {
		return nil, fmt.Errorf("compose file has no services")
	}
	return &cf, nil
}

// ToStack converts a ComposeFile into a proxkube Stack.
func (cf *ComposeFile) ToStack(name string, opts ConvertOptions) (*api.Stack, error) {
	stack := &api.Stack{
		Name:     name,
		Networks: make(map[string]api.Network),
	}

	// Convert networks.
	for netName, cn := range cf.Networks {
		n := api.Network{
			Bridge:   opts.DefaultBridge,
			Internal: cn.Internal,
		}
		if cn.IPAM != nil && len(cn.IPAM.Config) > 0 {
			n.Subnet = cn.IPAM.Config[0].Subnet
			n.Gateway = cn.IPAM.Config[0].Gateway
		}
		stack.Networks[netName] = n
	}

	// Convert services to pods.
	for svcName, svc := range cf.Services {
		pod, err := serviceToPod(svcName, &svc, opts, stack.Networks)
		if err != nil {
			return nil, fmt.Errorf("service %q: %w", svcName, err)
		}
		stack.Pods = append(stack.Pods, *pod)
	}

	return stack, nil
}

func serviceToPod(name string, svc *Service, opts ConvertOptions, nets map[string]api.Network) (*api.Pod, error) {
	pod := &api.Pod{
		APIVersion: "proxkube/v1",
		Kind:       "Pod",
		Metadata: api.Metadata{
			Name:   name,
			Labels: svc.Labels,
		},
		Spec: api.PodSpec{
			Node:        opts.Node,
			Image:       svc.Image,
			Hostname:    svc.Hostname,
			Expose:      len(svc.Ports) > 0,
			Environment: parseEnvironment(svc.Environment),
			Resources: api.Resources{
				CPU:     opts.DefaultCPU,
				Memory:  opts.DefaultMemoryMB,
				Disk:    opts.DefaultDiskGB,
				Storage: opts.Storage,
			},
		},
	}

	if pod.Spec.Hostname == "" {
		pod.Spec.Hostname = name
	}

	// Parse resources from deploy config.
	if svc.Deploy != nil && svc.Deploy.Resources != nil {
		applyResources(pod, svc.Deploy.Resources, opts)
	}

	// Parse ports.
	for _, p := range svc.Ports {
		pm, err := parsePort(p)
		if err != nil {
			return nil, err
		}
		pod.Spec.Ports = append(pod.Spec.Ports, pm)
	}

	// Parse volumes.
	for _, v := range svc.Volumes {
		vm := parseVolume(v)
		pod.Spec.Volumes = append(pod.Spec.Volumes, vm)
	}

	// Parse depends_on.
	pod.Spec.DependsOn = parseDependsOn(svc.DependsOn)

	// Parse networks.
	svcNets := parseServiceNetworks(svc.Networks)
	if len(svcNets) > 0 {
		for _, netName := range svcNets {
			pn := api.PodNetwork{Name: netName, Bridge: opts.DefaultBridge}
			if n, ok := nets[netName]; ok {
				if n.Bridge != "" {
					pn.Bridge = n.Bridge
				}
				pn.IP = "dhcp"
				pn.Gateway = n.Gateway
			}
			pod.Spec.Networks = append(pod.Spec.Networks, pn)
		}
	}

	// Auto-restart => startOnBoot.
	if svc.Restart == "always" || svc.Restart == "unless-stopped" {
		pod.Spec.StartOnBoot = true
	}

	// Parse command.
	pod.Spec.Command = parseCommand(svc.Command)

	return pod, nil
}

func parsePort(s string) (api.PortMapping, error) {
	pm := api.PortMapping{Protocol: "tcp"}

	// Check for protocol suffix.
	if idx := strings.LastIndex(s, "/"); idx > 0 {
		pm.Protocol = s[idx+1:]
		s = s[:idx]
	}

	parts := strings.SplitN(s, ":", 2)
	if len(parts) == 2 {
		host, err := strconv.Atoi(parts[0])
		if err != nil {
			return pm, fmt.Errorf("invalid host port %q: %w", parts[0], err)
		}
		pm.HostPort = host
		container, err := strconv.Atoi(parts[1])
		if err != nil {
			return pm, fmt.Errorf("invalid container port %q: %w", parts[1], err)
		}
		pm.ContainerPort = container
	} else {
		port, err := strconv.Atoi(parts[0])
		if err != nil {
			return pm, fmt.Errorf("invalid port %q: %w", parts[0], err)
		}
		pm.HostPort = port
		pm.ContainerPort = port
	}
	return pm, nil
}

func parseEnvironment(env interface{}) map[string]string {
	if env == nil {
		return nil
	}
	result := make(map[string]string)
	switch v := env.(type) {
	case map[string]interface{}:
		for key, val := range v {
			result[key] = fmt.Sprintf("%v", val)
		}
	case []interface{}:
		for _, item := range v {
			s := fmt.Sprintf("%v", item)
			parts := strings.SplitN(s, "=", 2)
			if len(parts) == 2 {
				result[parts[0]] = parts[1]
			} else {
				result[parts[0]] = ""
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseVolume(s string) api.VolumeMount {
	parts := strings.SplitN(s, ":", 3)
	vm := api.VolumeMount{Name: parts[0]}
	if len(parts) >= 2 {
		vm.MountPath = parts[1]
	}
	if len(parts) == 3 && parts[2] == "ro" {
		vm.ReadOnly = true
	}
	return vm
}

func parseDependsOn(dep interface{}) []string {
	if dep == nil {
		return nil
	}
	switch v := dep.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			result = append(result, fmt.Sprintf("%v", item))
		}
		return result
	case map[string]interface{}:
		result := make([]string, 0, len(v))
		for key := range v {
			result = append(result, key)
		}
		return result
	}
	return nil
}

func parseServiceNetworks(nets interface{}) []string {
	if nets == nil {
		return nil
	}
	switch v := nets.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			result = append(result, fmt.Sprintf("%v", item))
		}
		return result
	case map[string]interface{}:
		result := make([]string, 0, len(v))
		for key := range v {
			result = append(result, key)
		}
		return result
	}
	return nil
}

func parseCommand(cmd interface{}) string {
	if cmd == nil {
		return ""
	}
	switch v := cmd.(type) {
	case string:
		return v
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, fmt.Sprintf("%v", item))
		}
		return strings.Join(parts, " ")
	}
	return fmt.Sprintf("%v", cmd)
}

func applyResources(pod *api.Pod, res *DeployResources, opts ConvertOptions) {
	spec := res.Limits
	if spec == nil {
		spec = res.Reservations
	}
	if spec == nil {
		return
	}
	if spec.CPUs != "" {
		if cpus, err := strconv.ParseFloat(spec.CPUs, 64); err == nil {
			c := int(cpus)
			if c < 1 {
				c = 1
			}
			pod.Spec.Resources.CPU = c
		}
	}
	if spec.Memory != "" {
		pod.Spec.Resources.Memory = parseMemoryMB(spec.Memory)
	}
}

func parseMemoryMB(s string) int {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	if strings.HasSuffix(s, "g") || strings.HasSuffix(s, "gb") {
		num := strings.TrimRight(s, "gb")
		if v, err := strconv.ParseFloat(strings.TrimSpace(num), 64); err == nil {
			return int(v * 1024)
		}
	}
	if strings.HasSuffix(s, "m") || strings.HasSuffix(s, "mb") {
		num := strings.TrimRight(s, "mb")
		if v, err := strconv.Atoi(strings.TrimSpace(num)); err == nil {
			return v
		}
	}
	// Try plain integer (assume MB).
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return 512
}
