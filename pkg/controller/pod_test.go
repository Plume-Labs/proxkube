package controller

import (
	"fmt"
	"testing"
	"time"

	"github.com/GothShoot/proxkube/pkg/api"
	"github.com/GothShoot/proxkube/pkg/proxmox"
)

// mockProxmox implements ProxmoxAPI for testing.
type mockProxmox struct {
	nextID     int
	containers map[int]*proxmox.LXCStatus
	ifaces     map[int][]proxmox.LXCInterface
	created    []proxmox.LXCConfig
	started    []int
	stopped    []int
	deleted    []int
	failCreate bool
}

func newMock() *mockProxmox {
	return &mockProxmox{
		nextID:     100,
		containers: make(map[int]*proxmox.LXCStatus),
		ifaces:     make(map[int][]proxmox.LXCInterface),
	}
}

func (m *mockProxmox) NextID() (int, error) {
	id := m.nextID
	m.nextID++
	return id, nil
}

func (m *mockProxmox) CreateLXC(cfg proxmox.LXCConfig) (string, error) {
	if m.failCreate {
		return "", fmt.Errorf("create failed")
	}
	m.created = append(m.created, cfg)
	m.containers[cfg.VMID] = &proxmox.LXCStatus{
		VMID:   cfg.VMID,
		Status: "stopped",
		Name:   cfg.Hostname,
	}
	return "UPID:task:create", nil
}

func (m *mockProxmox) StartLXC(node string, vmid int) (string, error) {
	m.started = append(m.started, vmid)
	if ct, ok := m.containers[vmid]; ok {
		ct.Status = "running"
	}
	return "UPID:task:start", nil
}

func (m *mockProxmox) StopLXC(node string, vmid int) (string, error) {
	m.stopped = append(m.stopped, vmid)
	if ct, ok := m.containers[vmid]; ok {
		ct.Status = "stopped"
	}
	return "UPID:task:stop", nil
}

func (m *mockProxmox) DeleteLXC(node string, vmid int) (string, error) {
	m.deleted = append(m.deleted, vmid)
	delete(m.containers, vmid)
	return "UPID:task:delete", nil
}

func (m *mockProxmox) GetLXCStatus(node string, vmid int) (*proxmox.LXCStatus, error) {
	ct, ok := m.containers[vmid]
	if !ok {
		return nil, fmt.Errorf("container %d not found", vmid)
	}
	return ct, nil
}

func (m *mockProxmox) ListLXC(node string) ([]proxmox.LXCSummary, error) {
	var list []proxmox.LXCSummary
	for _, ct := range m.containers {
		list = append(list, proxmox.LXCSummary{
			VMID:   ct.VMID,
			Name:   ct.Name,
			Status: ct.Status,
		})
	}
	return list, nil
}

func (m *mockProxmox) GetLXCInterfaces(node string, vmid int) ([]proxmox.LXCInterface, error) {
	ifaces, ok := m.ifaces[vmid]
	if !ok {
		return nil, nil
	}
	return ifaces, nil
}

func (m *mockProxmox) WaitForTask(node, taskID string, timeout time.Duration) error {
	return nil
}

func testPod() *api.Pod {
	return &api.Pod{
		APIVersion: "proxkube/v1",
		Kind:       "Pod",
		Metadata:   api.Metadata{Name: "test-pod"},
		Spec: api.PodSpec{
			Node:  "pve",
			Image: "docker.io/library/ubuntu:22.04",
			Resources: api.Resources{
				CPU:     2,
				Memory:  512,
				Disk:    8,
				Storage: "local-lvm",
			},
			Networks: []api.PodNetwork{
				{Name: "default", Bridge: "vmbr0", IP: "dhcp"},
			},
		},
	}
}

func TestApplyCreatesPod(t *testing.T) {
	m := newMock()
	ctrl := NewPodController(m)

	pod := testPod()
	result, err := ctrl.Apply(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.created) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(m.created))
	}
	if m.created[0].VMID != 100 {
		t.Errorf("expected VMID 100, got %d", m.created[0].VMID)
	}
	if len(m.started) != 1 {
		t.Fatalf("expected 1 start call, got %d", len(m.started))
	}
	if result.Status.Phase != api.PhaseRunning {
		t.Errorf("expected Running phase, got %s", result.Status.Phase)
	}
	if result.Status.VMID != 100 {
		t.Errorf("expected VMID 100 in status, got %d", result.Status.VMID)
	}
}

func TestApplyExistingPod(t *testing.T) {
	m := newMock()
	m.containers[200] = &proxmox.LXCStatus{
		VMID:   200,
		Status: "running",
		Name:   "test-pod",
	}
	m.ifaces[200] = []proxmox.LXCInterface{
		{Name: "eth0", Inet: "10.0.0.5/24"},
	}
	ctrl := NewPodController(m)

	pod := testPod()
	result, err := ctrl.Apply(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.created) != 0 {
		t.Error("should not have created a new container")
	}
	if result.Status.VMID != 200 {
		t.Errorf("expected VMID 200, got %d", result.Status.VMID)
	}
	if result.Status.IP != "10.0.0.5" {
		t.Errorf("expected IP 10.0.0.5, got %s", result.Status.IP)
	}
}

func TestApplyValidationError(t *testing.T) {
	m := newMock()
	ctrl := NewPodController(m)

	pod := &api.Pod{} // invalid: no name, node, etc.
	_, err := ctrl.Apply(pod)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestApplyCreateError(t *testing.T) {
	m := newMock()
	m.failCreate = true
	ctrl := NewPodController(m)

	pod := testPod()
	_, err := ctrl.Apply(pod)
	if err == nil {
		t.Fatal("expected create error")
	}
}

func TestGet(t *testing.T) {
	m := newMock()
	m.containers[100] = &proxmox.LXCStatus{
		VMID:   100,
		Status: "running",
		Name:   "my-pod",
	}
	ctrl := NewPodController(m)

	pod := &api.Pod{
		Metadata: api.Metadata{Name: "my-pod"},
		Spec:     api.PodSpec{Node: "pve"},
	}
	result, err := ctrl.Get(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status.Phase != api.PhaseRunning {
		t.Errorf("expected Running, got %s", result.Status.Phase)
	}
}

func TestGetNotFound(t *testing.T) {
	m := newMock()
	ctrl := NewPodController(m)

	pod := &api.Pod{
		Metadata: api.Metadata{Name: "not-found"},
		Spec:     api.PodSpec{Node: "pve"},
	}
	_, err := ctrl.Get(pod)
	if err == nil {
		t.Fatal("expected not found error")
	}
}

func TestDeleteRunningPod(t *testing.T) {
	m := newMock()
	m.containers[100] = &proxmox.LXCStatus{
		VMID:   100,
		Status: "running",
		Name:   "del-pod",
	}
	ctrl := NewPodController(m)

	pod := &api.Pod{
		Metadata: api.Metadata{Name: "del-pod"},
		Spec:     api.PodSpec{Node: "pve"},
	}
	if err := ctrl.Delete(pod); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.stopped) != 1 {
		t.Error("expected container to be stopped first")
	}
	if len(m.deleted) != 1 {
		t.Error("expected container to be deleted")
	}
}

func TestDeleteStoppedPod(t *testing.T) {
	m := newMock()
	m.containers[101] = &proxmox.LXCStatus{
		VMID:   101,
		Status: "stopped",
		Name:   "stopped-pod",
	}
	ctrl := NewPodController(m)

	pod := &api.Pod{
		Metadata: api.Metadata{Name: "stopped-pod"},
		Spec:     api.PodSpec{Node: "pve"},
	}
	if err := ctrl.Delete(pod); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.stopped) != 0 {
		t.Error("should not stop an already stopped container")
	}
	if len(m.deleted) != 1 {
		t.Error("expected container to be deleted")
	}
}

func TestDeleteNotFound(t *testing.T) {
	m := newMock()
	ctrl := NewPodController(m)

	pod := &api.Pod{
		Metadata: api.Metadata{Name: "nope"},
		Spec:     api.PodSpec{Node: "pve"},
	}
	err := ctrl.Delete(pod)
	if err == nil {
		t.Fatal("expected not found error")
	}
}

func TestListPods(t *testing.T) {
	m := newMock()
	m.containers[100] = &proxmox.LXCStatus{VMID: 100, Status: "running", Name: "pod-a"}
	m.containers[101] = &proxmox.LXCStatus{VMID: 101, Status: "stopped", Name: "pod-b"}
	ctrl := NewPodController(m)

	pods, err := ctrl.List("pve")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods) != 2 {
		t.Fatalf("expected 2 pods, got %d", len(pods))
	}
}

func TestStatusToPhase(t *testing.T) {
	tests := []struct {
		in  string
		out api.Phase
	}{
		{"running", api.PhaseRunning},
		{"stopped", api.PhaseStopped},
		{"unknown", api.PhaseUnknown},
		{"", api.PhaseUnknown},
	}
	for _, tc := range tests {
		got := statusToPhase(tc.in)
		if got != tc.out {
			t.Errorf("statusToPhase(%q) = %s, want %s", tc.in, got, tc.out)
		}
	}
}

func TestPodToLXCConfig(t *testing.T) {
	pod := testPod()
	cfg := podToLXCConfig(pod, 150)

	if cfg.VMID != 150 {
		t.Errorf("expected VMID 150, got %d", cfg.VMID)
	}
	if cfg.Hostname != "test-pod" {
		t.Errorf("expected hostname test-pod, got %s", cfg.Hostname)
	}
	if cfg.Cores != 2 {
		t.Errorf("expected 2 cores, got %d", cfg.Cores)
	}
	if cfg.Memory != 512 {
		t.Errorf("expected 512 MB memory, got %d", cfg.Memory)
	}
	if cfg.IsOCI != true {
		t.Error("expected IsOCI true for image-based pod")
	}
	if len(cfg.Networks) != 1 {
		t.Fatalf("expected 1 network, got %d", len(cfg.Networks))
	}
	if cfg.Networks[0].Bridge != "vmbr0" {
		t.Errorf("expected vmbr0 bridge, got %s", cfg.Networks[0].Bridge)
	}
	if cfg.Networks[0].IP != "dhcp" {
		t.Errorf("expected dhcp IP, got %s", cfg.Networks[0].IP)
	}
}

func TestPodToLXCConfigWithExplicitHostname(t *testing.T) {
	pod := testPod()
	pod.Spec.Hostname = "custom-host"
	cfg := podToLXCConfig(pod, 151)
	if cfg.Hostname != "custom-host" {
		t.Errorf("expected hostname custom-host, got %s", cfg.Hostname)
	}
}

func TestPodToLXCConfigExposeDisablesFirewall(t *testing.T) {
	pod := testPod()
	pod.Spec.Expose = true
	cfg := podToLXCConfig(pod, 160)
	// When expose is true and firewall is not explicitly set, firewall should
	// NOT be forced on.
	if len(cfg.Networks) < 1 {
		t.Fatal("expected at least one network")
	}
	if cfg.Networks[0].Firewall {
		t.Error("expected firewall off when expose=true")
	}
}

func TestPodToLXCConfigInternalFirewall(t *testing.T) {
	pod := testPod()
	pod.Spec.Expose = false
	cfg := podToLXCConfig(pod, 161)
	if len(cfg.Networks) < 1 {
		t.Fatal("expected at least one network")
	}
	if !cfg.Networks[0].Firewall {
		t.Error("expected firewall on when expose=false (internal)")
	}
}

func TestPodToLXCConfigMultiNetwork(t *testing.T) {
	pod := testPod()
	pod.Spec.Networks = []api.PodNetwork{
		{Name: "frontend", Bridge: "vmbr0", IP: "dhcp"},
		{Name: "backend", Bridge: "vmbr1", IP: "10.10.0.5/24", Gateway: "10.10.0.1"},
	}
	pod.Spec.Expose = true
	cfg := podToLXCConfig(pod, 162)
	if len(cfg.Networks) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(cfg.Networks))
	}
	if cfg.Networks[0].Name != "eth0" || cfg.Networks[0].Bridge != "vmbr0" {
		t.Errorf("unexpected net0: %+v", cfg.Networks[0])
	}
	if cfg.Networks[1].Name != "eth1" || cfg.Networks[1].Bridge != "vmbr1" {
		t.Errorf("unexpected net1: %+v", cfg.Networks[1])
	}
}

func TestApplyStackBasic(t *testing.T) {
	m := newMock()
	ctrl := NewPodController(m)

	stack := &api.Stack{
		Name: "mystack",
		Pods: []api.Pod{
			{
				Metadata: api.Metadata{Name: "db"},
				Spec: api.PodSpec{
					Node:      "pve",
					Image:     "postgres:16",
					Resources: api.Resources{CPU: 1, Memory: 256, Disk: 4, Storage: "local-lvm"},
				},
			},
			{
				Metadata: api.Metadata{Name: "web"},
				Spec: api.PodSpec{
					Node:      "pve",
					Image:     "nginx:latest",
					Resources: api.Resources{CPU: 1, Memory: 256, Disk: 4, Storage: "local-lvm"},
					DependsOn: []string{"db"},
				},
			},
		},
	}

	result, err := ctrl.ApplyStack(stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Pods) != 2 {
		t.Fatalf("expected 2 pods, got %d", len(result.Pods))
	}
	// DB should have been created first.
	if len(m.created) != 2 {
		t.Fatalf("expected 2 creates, got %d", len(m.created))
	}
}

func TestApplyStackCircularDep(t *testing.T) {
	m := newMock()
	ctrl := NewPodController(m)

	stack := &api.Stack{
		Name: "circular",
		Pods: []api.Pod{
			{
				Metadata: api.Metadata{Name: "a"},
				Spec: api.PodSpec{
					Node:      "pve",
					Image:     "img:1",
					Resources: api.Resources{CPU: 1, Memory: 256, Disk: 4, Storage: "local-lvm"},
					DependsOn: []string{"b"},
				},
			},
			{
				Metadata: api.Metadata{Name: "b"},
				Spec: api.PodSpec{
					Node:      "pve",
					Image:     "img:1",
					Resources: api.Resources{CPU: 1, Memory: 256, Disk: 4, Storage: "local-lvm"},
					DependsOn: []string{"a"},
				},
			},
		},
	}

	_, err := ctrl.ApplyStack(stack)
	if err == nil {
		t.Fatal("expected circular dependency error")
	}
}

func TestDeleteStack(t *testing.T) {
	m := newMock()
	m.containers[100] = &proxmox.LXCStatus{VMID: 100, Status: "running", Name: "db"}
	m.containers[101] = &proxmox.LXCStatus{VMID: 101, Status: "running", Name: "web"}
	ctrl := NewPodController(m)

	stack := &api.Stack{
		Name: "mystack",
		Pods: []api.Pod{
			{Metadata: api.Metadata{Name: "db"}, Spec: api.PodSpec{Node: "pve"}},
			{Metadata: api.Metadata{Name: "web"}, Spec: api.PodSpec{Node: "pve"}},
		},
	}

	if err := ctrl.DeleteStack(stack); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.deleted) != 2 {
		t.Errorf("expected 2 deletes, got %d", len(m.deleted))
	}
}
