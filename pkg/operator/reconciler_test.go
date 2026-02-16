package operator

import (
	"encoding/json"
	"testing"

	"github.com/GothShoot/proxkube/pkg/api"
	"github.com/GothShoot/proxkube/pkg/controller"
	"github.com/GothShoot/proxkube/pkg/proxmox"
	"fmt"
	"time"
)

// --- mock ProxmoxAPI for testing ---

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
		list = append(list, proxmox.LXCSummary{VMID: ct.VMID, Name: ct.Name, Status: ct.Status})
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

// --- tests ---

func testPod() *api.Pod {
	return &api.Pod{
		APIVersion: "proxkube/v1",
		Kind:       "Pod",
		Metadata:   api.Metadata{Name: "test-pod"},
		Spec: api.PodSpec{
			Node:  "pve",
			Image: "nginx:latest",
			Resources: api.Resources{
				CPU: 1, Memory: 256, Disk: 4, Storage: "local-lvm",
			},
		},
	}
}

func TestReconcileAdded(t *testing.T) {
	m := newMock()
	ctrl := controller.NewPodController(m)
	rec := NewReconciler(ctrl)

	event := Event{Type: EventAdded, Object: testPod()}
	result := rec.Reconcile(event)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Requeue {
		t.Error("expected no requeue")
	}
	if len(m.created) != 1 {
		t.Fatalf("expected 1 create, got %d", len(m.created))
	}
	if event.Object.Status.Phase != api.PhaseRunning {
		t.Errorf("expected Running phase, got %s", event.Object.Status.Phase)
	}
}

func TestReconcileModified(t *testing.T) {
	m := newMock()
	m.containers[200] = &proxmox.LXCStatus{VMID: 200, Status: "running", Name: "test-pod"}
	ctrl := controller.NewPodController(m)
	rec := NewReconciler(ctrl)

	event := Event{Type: EventModified, Object: testPod()}
	result := rec.Reconcile(event)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(m.created) != 0 {
		t.Error("should not create on modify for existing pod")
	}
}

func TestReconcileDeleted(t *testing.T) {
	m := newMock()
	m.containers[100] = &proxmox.LXCStatus{VMID: 100, Status: "running", Name: "test-pod"}
	ctrl := controller.NewPodController(m)
	rec := NewReconciler(ctrl)

	event := Event{Type: EventDeleted, Object: testPod()}
	result := rec.Reconcile(event)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(m.deleted) != 1 {
		t.Error("expected 1 delete")
	}
}

func TestReconcileDeleteNotFound(t *testing.T) {
	m := newMock()
	ctrl := controller.NewPodController(m)
	rec := NewReconciler(ctrl)

	event := Event{Type: EventDeleted, Object: testPod()}
	result := rec.Reconcile(event)

	// Not found on delete should succeed (already gone).
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Requeue {
		t.Error("should not requeue on not-found delete")
	}
}

func TestReconcileCreateError(t *testing.T) {
	m := newMock()
	m.failCreate = true
	ctrl := controller.NewPodController(m)
	rec := NewReconciler(ctrl)

	event := Event{Type: EventAdded, Object: testPod()}
	result := rec.Reconcile(event)

	if result.Error == nil {
		t.Fatal("expected error")
	}
	if !result.Requeue {
		t.Error("expected requeue on error")
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected 30s requeue, got %v", result.RequeueAfter)
	}
}

func TestReconcileUnknownEvent(t *testing.T) {
	m := newMock()
	ctrl := controller.NewPodController(m)
	rec := NewReconciler(ctrl)

	result := rec.Reconcile(Event{Type: "BOOKMARK", Object: testPod()})
	if result.Error == nil {
		t.Fatal("expected error for unknown event type")
	}
}

func TestProxKubePodFromJSON(t *testing.T) {
	raw := `{
		"apiVersion": "proxkube.io/v1",
		"kind": "ProxKubePod",
		"metadata": {"name": "my-app", "namespace": "default"},
		"spec": {
			"node": "pve",
			"image": "nginx:latest",
			"resources": {"cpu": 2, "memory": 512, "disk": 8, "storage": "local-lvm"},
			"tags": ["web"],
			"pool": "web-pool"
		}
	}`

	pod, err := ProxKubePodFromJSON([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pod.Metadata.Name != "my-app" {
		t.Errorf("expected name my-app, got %s", pod.Metadata.Name)
	}
	if pod.Spec.Node != "pve" {
		t.Errorf("expected node pve, got %s", pod.Spec.Node)
	}
	if pod.Spec.Image != "nginx:latest" {
		t.Errorf("expected image nginx:latest, got %s", pod.Spec.Image)
	}
	if pod.Spec.Resources.CPU != 2 {
		t.Errorf("expected 2 CPUs, got %d", pod.Spec.Resources.CPU)
	}
	if pod.Spec.Pool != "web-pool" {
		t.Errorf("expected pool web-pool, got %s", pod.Spec.Pool)
	}
}

func TestProxKubePodFromJSONInvalid(t *testing.T) {
	_, err := ProxKubePodFromJSON([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestPodToStatusJSON(t *testing.T) {
	pod := &api.Pod{
		Status: api.Status{
			Phase: api.PhaseRunning,
			VMID:  100,
			Node:  "pve",
			IP:    "10.0.0.5",
		},
	}
	data, err := PodToStatusJSON(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	status := result["status"].(map[string]interface{})
	if status["phase"] != "Running" {
		t.Errorf("expected Running, got %v", status["phase"])
	}
	if int(status["vmid"].(float64)) != 100 {
		t.Errorf("expected VMID 100, got %v", status["vmid"])
	}
}

func TestCRDManifest(t *testing.T) {
	crd := CRDManifest()
	if crd == "" {
		t.Fatal("expected non-empty CRD manifest")
	}
	if !contains(crd, "proxkubepods.proxkube.io") {
		t.Error("expected CRD name in manifest")
	}
	if !contains(crd, "ProxKubePod") {
		t.Error("expected kind in manifest")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
