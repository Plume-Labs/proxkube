// Package controller implements the reconciliation logic that maps Pod
// specifications to Proxmox LXC container operations.
package controller

import (
	"fmt"
	"strings"
	"time"

	"github.com/GothShoot/proxkube/pkg/api"
	"github.com/GothShoot/proxkube/pkg/proxmox"
)

// ProxmoxAPI defines the subset of the Proxmox client used by the controller.
// Using an interface allows for easy testing with mocks.
type ProxmoxAPI interface {
	NextID() (int, error)
	CreateLXC(cfg proxmox.LXCConfig) (string, error)
	StartLXC(node string, vmid int) (string, error)
	StopLXC(node string, vmid int) (string, error)
	DeleteLXC(node string, vmid int) (string, error)
	GetLXCStatus(node string, vmid int) (*proxmox.LXCStatus, error)
	ListLXC(node string) ([]proxmox.LXCSummary, error)
	GetLXCInterfaces(node string, vmid int) ([]proxmox.LXCInterface, error)
	WaitForTask(node, taskID string, timeout time.Duration) error
}

// PodController orchestrates LXC containers on Proxmox as Kubernetes-style pods.
type PodController struct {
	client      ProxmoxAPI
	taskTimeout time.Duration
}

// NewPodController creates a new controller with the given Proxmox client.
func NewPodController(client ProxmoxAPI) *PodController {
	return &PodController{
		client:      client,
		taskTimeout: 120 * time.Second,
	}
}

// Apply creates or updates a pod. If the pod (identified by VMID or name) does
// not exist it will be created and started; otherwise its status is refreshed.
func (pc *PodController) Apply(pod *api.Pod) (*api.Pod, error) {
	if err := pod.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}

	// Determine VMID.
	vmid := pod.Spec.VMID
	if vmid == 0 {
		// Check if a container with this name already exists.
		existing, err := pc.findByName(pod.Spec.Node, pod.Metadata.Name)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			vmid = existing.VMID
		}
	}

	if vmid > 0 {
		// Container exists, refresh status.
		return pc.refreshStatus(pod, vmid)
	}

	// Allocate a new VMID.
	newID, err := pc.client.NextID()
	if err != nil {
		return nil, fmt.Errorf("allocate VMID: %w", err)
	}

	cfg := podToLXCConfig(pod, newID)
	taskID, err := pc.client.CreateLXC(cfg)
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	if err := pc.client.WaitForTask(pod.Spec.Node, taskID, pc.taskTimeout); err != nil {
		return nil, fmt.Errorf("wait for create: %w", err)
	}

	// Start the container.
	startTask, err := pc.client.StartLXC(pod.Spec.Node, newID)
	if err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}
	if err := pc.client.WaitForTask(pod.Spec.Node, startTask, pc.taskTimeout); err != nil {
		return nil, fmt.Errorf("wait for start: %w", err)
	}

	return pc.refreshStatus(pod, newID)
}

// Get retrieves the current status of a pod.
func (pc *PodController) Get(pod *api.Pod) (*api.Pod, error) {
	vmid := pod.Spec.VMID
	if vmid == 0 {
		existing, err := pc.findByName(pod.Spec.Node, pod.Metadata.Name)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, fmt.Errorf("pod %q not found on node %q", pod.Metadata.Name, pod.Spec.Node)
		}
		vmid = existing.VMID
	}
	return pc.refreshStatus(pod, vmid)
}

// Delete stops and destroys the container backing the pod.
func (pc *PodController) Delete(pod *api.Pod) error {
	vmid := pod.Spec.VMID
	if vmid == 0 {
		existing, err := pc.findByName(pod.Spec.Node, pod.Metadata.Name)
		if err != nil {
			return err
		}
		if existing == nil {
			return fmt.Errorf("pod %q not found on node %q", pod.Metadata.Name, pod.Spec.Node)
		}
		vmid = existing.VMID
	}

	// Stop if running.
	status, err := pc.client.GetLXCStatus(pod.Spec.Node, vmid)
	if err != nil {
		return fmt.Errorf("get status: %w", err)
	}
	if status.Status == "running" {
		stopTask, err := pc.client.StopLXC(pod.Spec.Node, vmid)
		if err != nil {
			return fmt.Errorf("stop container: %w", err)
		}
		if err := pc.client.WaitForTask(pod.Spec.Node, stopTask, pc.taskTimeout); err != nil {
			return fmt.Errorf("wait for stop: %w", err)
		}
	}

	delTask, err := pc.client.DeleteLXC(pod.Spec.Node, vmid)
	if err != nil {
		return fmt.Errorf("delete container: %w", err)
	}
	return pc.client.WaitForTask(pod.Spec.Node, delTask, pc.taskTimeout)
}

// ApplyStack creates or updates all pods in a stack, respecting dependsOn order.
func (pc *PodController) ApplyStack(stack *api.Stack) (*api.Stack, error) {
	applied := make(map[string]bool)
	result := &api.Stack{
		Name:     stack.Name,
		Networks: stack.Networks,
		Pods:     make([]api.Pod, 0, len(stack.Pods)),
	}

	// Iteratively apply pods whose dependencies are already satisfied.
	remaining := make([]api.Pod, len(stack.Pods))
	copy(remaining, stack.Pods)

	for len(remaining) > 0 {
		progress := false
		var next []api.Pod
		for i := range remaining {
			pod := &remaining[i]
			ready := true
			for _, dep := range pod.Spec.DependsOn {
				if !applied[dep] {
					ready = false
					break
				}
			}
			if !ready {
				next = append(next, *pod)
				continue
			}
			res, err := pc.Apply(pod)
			if err != nil {
				return nil, fmt.Errorf("apply pod %q in stack %q: %w", pod.Metadata.Name, stack.Name, err)
			}
			result.Pods = append(result.Pods, *res)
			applied[pod.Metadata.Name] = true
			progress = true
		}
		if !progress {
			names := make([]string, 0, len(next))
			for _, p := range next {
				names = append(names, p.Metadata.Name)
			}
			return nil, fmt.Errorf("circular or unresolvable dependency among pods: %s", strings.Join(names, ", "))
		}
		remaining = next
	}

	return result, nil
}

// DeleteStack stops and destroys all pods in a stack (in reverse order).
func (pc *PodController) DeleteStack(stack *api.Stack) error {
	for i := len(stack.Pods) - 1; i >= 0; i-- {
		pod := &stack.Pods[i]
		if err := pc.Delete(pod); err != nil {
			return fmt.Errorf("delete pod %q in stack %q: %w", pod.Metadata.Name, stack.Name, err)
		}
	}
	return nil
}

// List returns all pods on a given node.
func (pc *PodController) List(node string) ([]api.Pod, error) {
	containers, err := pc.client.ListLXC(node)
	if err != nil {
		return nil, err
	}
	pods := make([]api.Pod, 0, len(containers))
	for _, ct := range containers {
		p := api.Pod{
			APIVersion: "proxkube/v1",
			Kind:       "Pod",
			Metadata:   api.Metadata{Name: ct.Name},
			Spec:       api.PodSpec{Node: node, VMID: ct.VMID},
			Status: api.Status{
				VMID:  ct.VMID,
				Node:  node,
				Phase: statusToPhase(ct.Status),
			},
		}
		pods = append(pods, p)
	}
	return pods, nil
}

func (pc *PodController) findByName(node, name string) (*proxmox.LXCSummary, error) {
	containers, err := pc.client.ListLXC(node)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	for _, ct := range containers {
		if ct.Name == name {
			return &ct, nil
		}
	}
	return nil, nil
}

func (pc *PodController) refreshStatus(pod *api.Pod, vmid int) (*api.Pod, error) {
	status, err := pc.client.GetLXCStatus(pod.Spec.Node, vmid)
	if err != nil {
		return nil, fmt.Errorf("get status: %w", err)
	}

	result := *pod
	result.Status = api.Status{
		VMID:  vmid,
		Node:  pod.Spec.Node,
		Phase: statusToPhase(status.Status),
	}

	// Try to get the IP address.
	if status.Status == "running" {
		ifaces, err := pc.client.GetLXCInterfaces(pod.Spec.Node, vmid)
		if err == nil {
			for _, iface := range ifaces {
				if iface.Name != "lo" && iface.Inet != "" {
					ip := iface.Inet
					if idx := strings.Index(ip, "/"); idx > 0 {
						ip = ip[:idx]
					}
					result.Status.IP = ip
					break
				}
			}
		}
	}

	return &result, nil
}

func statusToPhase(s string) api.Phase {
	switch s {
	case "running":
		return api.PhaseRunning
	case "stopped":
		return api.PhaseStopped
	default:
		return api.PhaseUnknown
	}
}

func podToLXCConfig(pod *api.Pod, vmid int) proxmox.LXCConfig {
	cfg := proxmox.LXCConfig{
		Node:         pod.Spec.Node,
		VMID:         vmid,
		OSTemplate:   pod.Spec.EffectiveTemplate(),
		IsOCI:        pod.Spec.IsOCI(),
		Hostname:     pod.Spec.Hostname,
		Cores:        pod.Spec.Resources.CPU,
		Memory:       pod.Spec.Resources.Memory,
		Swap:         pod.Spec.Resources.Swap,
		RootfsDisk:   pod.Spec.Resources.Disk,
		Storage:      pod.Spec.Resources.Storage,
		Password:     pod.Spec.Password,
		SSHPublicKey: pod.Spec.SSHPublicKeys,
		Unprivileged: pod.Spec.Unprivileged,
		StartOnBoot:  pod.Spec.StartOnBoot,
		Nameserver:   pod.Spec.Nameserver,
		SearchDomain: pod.Spec.SearchDomain,
		Environment:  pod.Spec.Environment,
	}
	if pod.Spec.Hostname == "" {
		cfg.Hostname = pod.Metadata.Name
	}

	// Build network interfaces from Networks slice, falling back to the
	// legacy single-interface Resources.Network config.
	if len(pod.Spec.Networks) > 0 {
		for i, n := range pod.Spec.Networks {
			nc := proxmox.LXCNetConfig{
				Name:     fmt.Sprintf("eth%d", i),
				Bridge:   n.Bridge,
				IP:       n.IP,
				Gateway:  n.Gateway,
				Firewall: n.Firewall,
			}
			// When the pod is internal-only (expose=false) and the user
			// has not explicitly enabled the firewall, automatically
			// enable it to block external access by default.
			if !pod.Spec.Expose && !n.Firewall {
				nc.Firewall = true
			}
			cfg.Networks = append(cfg.Networks, nc)
		}
	} else if pod.Spec.Resources.Network != nil {
		nc := proxmox.LXCNetConfig{
			Name:     "eth0",
			Bridge:   pod.Spec.Resources.Network.Bridge,
			IP:       pod.Spec.Resources.Network.IP,
			Gateway:  pod.Spec.Resources.Network.Gateway,
			Firewall: pod.Spec.Resources.Network.Firewall,
		}
		if !pod.Spec.Expose && !nc.Firewall {
			nc.Firewall = true
		}
		cfg.Networks = append(cfg.Networks, nc)
	}

	return cfg
}
