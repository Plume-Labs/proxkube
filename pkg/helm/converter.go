// Package helm provides a converter from Helm chart values into proxkube Pod
// definitions. This enables deploying applications defined as Helm charts onto
// Proxmox LXC containers via proxkube, bridging the Helm ecosystem with
// Proxmox infrastructure.
//
// It reads a values.yaml file and converts it into a proxkube Stack of Pods.
package helm

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/GothShoot/proxkube/pkg/api"
)

// ChartMeta represents the Chart.yaml metadata.
type ChartMeta struct {
	APIVersion  string `yaml:"apiVersion"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Version     string `yaml:"version"`
	AppVersion  string `yaml:"appVersion,omitempty"`
}

// Values represents the values.yaml structure used to define proxkube pods
// for Helm-based deployment. This is the proxkube-specific values schema.
type Values struct {
	// Pods defines the pods to deploy.
	Pods map[string]PodValues `yaml:"pods"`
	// Global settings applied to all pods unless overridden.
	Global GlobalValues `yaml:"global,omitempty"`
}

// GlobalValues holds default settings for all pods in the chart.
type GlobalValues struct {
	Node    string   `yaml:"node,omitempty"`
	Storage string   `yaml:"storage,omitempty"`
	Bridge  string   `yaml:"bridge,omitempty"`
	Pool    string   `yaml:"pool,omitempty"`
	Tags    []string `yaml:"tags,omitempty"`
}

// PodValues defines a single pod in the Helm values.
type PodValues struct {
	Image       string            `yaml:"image"`
	OSTemplate  string            `yaml:"osTemplate,omitempty"`
	Node        string            `yaml:"node,omitempty"`
	Hostname    string            `yaml:"hostname,omitempty"`
	Expose      bool              `yaml:"expose,omitempty"`
	Pool        string            `yaml:"pool,omitempty"`
	Tags        []string          `yaml:"tags,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Resources   ResourceValues    `yaml:"resources,omitempty"`
	Ports       []PortValues      `yaml:"ports,omitempty"`
	Networks    []NetworkValues   `yaml:"networks,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Volumes     []VolumeValues    `yaml:"volumes,omitempty"`
	MountPoints []MountValues     `yaml:"mountPoints,omitempty"`
	DependsOn   []string          `yaml:"dependsOn,omitempty"`
	StartOnBoot bool              `yaml:"startOnBoot,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
}

// ResourceValues defines resource limits for a pod.
type ResourceValues struct {
	CPU     int    `yaml:"cpu,omitempty"`
	Memory  int    `yaml:"memory,omitempty"`
	Disk    int    `yaml:"disk,omitempty"`
	Storage string `yaml:"storage,omitempty"`
}

// PortValues defines a port mapping.
type PortValues struct {
	HostPort      int    `yaml:"hostPort"`
	ContainerPort int    `yaml:"containerPort"`
	Protocol      string `yaml:"protocol,omitempty"`
}

// NetworkValues defines a network attachment.
type NetworkValues struct {
	Name    string `yaml:"name"`
	Bridge  string `yaml:"bridge,omitempty"`
	IP      string `yaml:"ip,omitempty"`
	Gateway string `yaml:"gateway,omitempty"`
}

// VolumeValues defines a volume mount.
type VolumeValues struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mountPath"`
	ReadOnly  bool   `yaml:"readOnly,omitempty"`
}

// MountValues defines a Proxmox storage mount point.
type MountValues struct {
	Storage   string `yaml:"storage"`
	Size      int    `yaml:"size"`
	MountPath string `yaml:"mountPath"`
	ReadOnly  bool   `yaml:"readOnly,omitempty"`
	Backup    bool   `yaml:"backup,omitempty"`
}

// LoadValues reads and parses a Helm values.yaml file.
func LoadValues(path string) (*Values, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read values file: %w", err)
	}
	return ParseValues(data)
}

// ParseValues parses raw YAML bytes into Values.
func ParseValues(data []byte) (*Values, error) {
	var vals Values
	if err := yaml.Unmarshal(data, &vals); err != nil {
		return nil, fmt.Errorf("parse values: %w", err)
	}
	if len(vals.Pods) == 0 {
		return nil, fmt.Errorf("values file has no pods defined")
	}
	return &vals, nil
}

// LoadChart reads and parses a Chart.yaml file.
func LoadChart(path string) (*ChartMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read chart file: %w", err)
	}
	var chart ChartMeta
	if err := yaml.Unmarshal(data, &chart); err != nil {
		return nil, fmt.Errorf("parse chart: %w", err)
	}
	return &chart, nil
}

// ToStack converts Helm Values into a proxkube Stack.
func (v *Values) ToStack(releaseName string) (*api.Stack, error) {
	stack := &api.Stack{
		Name:     releaseName,
		Networks: make(map[string]api.Network),
	}

	for name, pv := range v.Pods {
		pod, err := podValuesToPod(name, &pv, &v.Global, releaseName)
		if err != nil {
			return nil, fmt.Errorf("pod %q: %w", name, err)
		}
		stack.Pods = append(stack.Pods, *pod)
	}

	return stack, nil
}

func podValuesToPod(name string, pv *PodValues, global *GlobalValues, release string) (*api.Pod, error) {
	pod := &api.Pod{
		APIVersion: "proxkube/v1",
		Kind:       "Pod",
		Metadata: api.Metadata{
			Name:   release + "-" + name,
			Labels: pv.Labels,
		},
		Spec: api.PodSpec{
			Image:       pv.Image,
			OSTemplate:  pv.OSTemplate,
			Hostname:    pv.Hostname,
			Expose:      pv.Expose,
			Environment: pv.Environment,
			Description: pv.Description,
			StartOnBoot: pv.StartOnBoot,
		},
	}

	// Apply global defaults.
	pod.Spec.Node = coalesce(pv.Node, global.Node, "pve")
	pod.Spec.Pool = coalesce(pv.Pool, global.Pool)

	// Resources with defaults.
	pod.Spec.Resources = api.Resources{
		CPU:     coalesceInt(pv.Resources.CPU, 1),
		Memory:  coalesceInt(pv.Resources.Memory, 512),
		Disk:    coalesceInt(pv.Resources.Disk, 8),
		Storage: coalesce(pv.Resources.Storage, global.Storage, "local-lvm"),
	}

	// Tags — merge global + pod-specific + release name.
	var tags []string
	tags = append(tags, global.Tags...)
	tags = append(tags, pv.Tags...)
	tags = append(tags, "helm-release="+release)
	pod.Spec.Tags = tags

	// Prefix dependsOn names with the release name.
	for _, dep := range pv.DependsOn {
		pod.Spec.DependsOn = append(pod.Spec.DependsOn, release+"-"+dep)
	}

	// Ports.
	for _, p := range pv.Ports {
		pm := api.PortMapping{
			HostPort:      p.HostPort,
			ContainerPort: p.ContainerPort,
			Protocol:      p.Protocol,
		}
		if pm.Protocol == "" {
			pm.Protocol = "tcp"
		}
		pod.Spec.Ports = append(pod.Spec.Ports, pm)
	}

	// Networks.
	for _, n := range pv.Networks {
		pn := api.PodNetwork{
			Name:    n.Name,
			Bridge:  coalesce(n.Bridge, global.Bridge, "vmbr0"),
			IP:      n.IP,
			Gateway: n.Gateway,
		}
		pod.Spec.Networks = append(pod.Spec.Networks, pn)
	}

	// Volumes.
	for _, vol := range pv.Volumes {
		pod.Spec.Volumes = append(pod.Spec.Volumes, api.VolumeMount{
			Name:      vol.Name,
			MountPath: vol.MountPath,
			ReadOnly:  vol.ReadOnly,
		})
	}

	// Mount points.
	for _, mp := range pv.MountPoints {
		pod.Spec.MountPoints = append(pod.Spec.MountPoints, api.MountPoint{
			Storage:   mp.Storage,
			Size:      mp.Size,
			MountPath: mp.MountPath,
			ReadOnly:  mp.ReadOnly,
			Backup:    mp.Backup,
		})
	}

	return pod, nil
}

// RenderTemplate renders a simple proxkube pod manifest from values,
// returning the YAML output (similar to `helm template`).
func RenderTemplate(releaseName string, vals *Values) (string, error) {
	stack, err := vals.ToStack(releaseName)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)

	for i, pod := range stack.Pods {
		if i > 0 {
			sb.WriteString("---\n")
		}
		if err := enc.Encode(pod); err != nil {
			return "", fmt.Errorf("encode pod %s: %w", pod.Metadata.Name, err)
		}
	}
	enc.Close()

	return sb.String(), nil
}

func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func coalesceInt(vals ...int) int {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}
