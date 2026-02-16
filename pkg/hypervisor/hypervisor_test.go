package hypervisor

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GothShoot/proxkube/pkg/proxmox"
)

// envelope wraps data in a Proxmox-style {"data": ...} response.
func envelope(data interface{}) []byte {
	b, _ := json.Marshal(map[string]interface{}{"data": data})
	return b
}

// testSocketServer creates a Unix domain socket HTTP server and a Client
// configured to talk to it, plus a mock pct script.
func testSocketServer(t *testing.T, handler http.HandlerFunc) (*Client, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "pvedaemon.sock")

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}

	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(listener) }()

	// Create a mock pct script that echoes arguments.
	pctPath := filepath.Join(tmpDir, "pct")
	pctScript := "#!/bin/sh\necho \"UPID:mock:pct-$1:$2\"\n"
	if err := os.WriteFile(pctPath, []byte(pctScript), 0755); err != nil { //nolint:gosec // test file
		t.Fatalf("write mock pct: %v", err)
	}

	c, err := NewClient(Config{
		SocketPath: sockPath,
		PctPath:    pctPath,
		Node:       "pve-test",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	cleanup := func() {
		srv.Close()
		listener.Close()
	}

	return c, cleanup
}

func TestNewClientDefaultNode(t *testing.T) {
	// With an explicit node name.
	c, err := NewClient(Config{
		SocketPath: "/nonexistent",
		Node:       "mynode",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Node() != "mynode" {
		t.Errorf("expected node mynode, got %s", c.Node())
	}
}

func TestNextIDViaSocket(t *testing.T) {
	c, cleanup := testSocketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/cluster/nextid") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write(envelope("200"))
	})
	defer cleanup()

	id, err := c.NextID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 200 {
		t.Errorf("expected 200, got %d", id)
	}
}

func TestGetLXCStatusViaSocket(t *testing.T) {
	c, cleanup := testSocketServer(t, func(w http.ResponseWriter, r *http.Request) {
		status := proxmox.LXCStatus{
			VMID:   100,
			Status: "running",
			Name:   "test-ct",
		}
		w.Write(envelope(status))
	})
	defer cleanup()

	s, err := c.GetLXCStatus("pve-test", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.VMID != 100 || s.Status != "running" {
		t.Errorf("unexpected status: %+v", s)
	}
}

func TestListLXCViaSocket(t *testing.T) {
	c, cleanup := testSocketServer(t, func(w http.ResponseWriter, r *http.Request) {
		list := []proxmox.LXCSummary{
			{VMID: 100, Name: "ct1", Status: "running"},
			{VMID: 101, Name: "ct2", Status: "stopped"},
		}
		w.Write(envelope(list))
	})
	defer cleanup()

	items, err := c.ListLXC("pve-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestGetLXCInterfacesViaSocket(t *testing.T) {
	c, cleanup := testSocketServer(t, func(w http.ResponseWriter, r *http.Request) {
		ifaces := []proxmox.LXCInterface{
			{Name: "eth0", Inet: "10.0.0.5/24"},
		}
		w.Write(envelope(ifaces))
	})
	defer cleanup()

	ifaces, err := c.GetLXCInterfaces("pve-test", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ifaces) != 1 || ifaces[0].Inet != "10.0.0.5/24" {
		t.Errorf("unexpected interfaces: %+v", ifaces)
	}
}

func TestCreateLXCViaPct(t *testing.T) {
	c, cleanup := testSocketServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(envelope("OK"))
	})
	defer cleanup()

	cfg := proxmox.LXCConfig{
		VMID:         100,
		OSTemplate:   "local:vztmpl/ubuntu.tar.zst",
		Hostname:     "test-ct",
		Cores:        2,
		Memory:       512,
		RootfsDisk:   8,
		Storage:      "local-lvm",
		Unprivileged: true,
		Tags:         "proxkube;web",
		Pool:         "web-pool",
		Description:  "Test container",
		Networks: []proxmox.LXCNetConfig{
			{Name: "eth0", Bridge: "vmbr0", IP: "dhcp"},
		},
		MountPoints: []proxmox.LXCMountPoint{
			{Storage: "local-lvm", Size: 10, MountPath: "/mnt/data", Backup: true},
		},
	}

	taskID, err := c.CreateLXC(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskID == "" {
		t.Error("expected non-empty task ID")
	}
	// The mock pct script returns UPID:mock:pct-create:100
	if !strings.Contains(taskID, "pct-create") && !strings.Contains(taskID, "create") {
		t.Errorf("expected create task ID, got %s", taskID)
	}
}

func TestStartLXCViaPct(t *testing.T) {
	c, cleanup := testSocketServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(envelope("OK"))
	})
	defer cleanup()

	taskID, err := c.StartLXC("pve-test", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(taskID, "pct-start") {
		t.Errorf("expected start task ID, got %s", taskID)
	}
}

func TestStopLXCViaPct(t *testing.T) {
	c, cleanup := testSocketServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(envelope("OK"))
	})
	defer cleanup()

	taskID, err := c.StopLXC("pve-test", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(taskID, "pct-stop") {
		t.Errorf("expected stop task ID, got %s", taskID)
	}
}

func TestDeleteLXCViaPct(t *testing.T) {
	c, cleanup := testSocketServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(envelope("OK"))
	})
	defer cleanup()

	taskID, err := c.DeleteLXC("pve-test", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(taskID, "pct-destroy") {
		t.Errorf("expected destroy task ID, got %s", taskID)
	}
}

func TestWaitForTaskPctNoOp(t *testing.T) {
	c, cleanup := testSocketServer(t, func(w http.ResponseWriter, r *http.Request) {})
	defer cleanup()

	// pct-originated tasks are synchronous, WaitForTask should return nil.
	err := c.WaitForTask("pve-test", "UPID:pve:pct-create:100", 5)
	if err != nil {
		t.Fatalf("expected no-op for pct task, got: %v", err)
	}
}

func TestSocketRequestError(t *testing.T) {
	c, cleanup := testSocketServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	})
	defer cleanup()

	_, err := c.ListLXC("pve-test")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestExecViaPct(t *testing.T) {
	c, cleanup := testSocketServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(envelope("OK"))
	})
	defer cleanup()

	out, err := c.Exec(100, []string{"ls", "-la"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Error("expected some output from mock pct")
	}
}
