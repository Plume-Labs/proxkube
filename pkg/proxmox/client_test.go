package proxmox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// helper wraps data in a Proxmox-style {"data": ...} envelope.
func envelope(data interface{}) []byte {
	b, _ := json.Marshal(map[string]interface{}{"data": data})
	return b
}

func testServer(handler http.HandlerFunc) (*httptest.Server, *Client) {
	ts := httptest.NewTLSServer(handler)
	c := &Client{
		baseURL:    ts.URL,
		httpClient: ts.Client(),
		token:      "PVEAPIToken=test@pam!tok=secret",
	}
	return ts, c
}

func TestNewClientRequiresBaseURL(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("expected error for missing base URL")
	}
}

func TestNewClientRequiresAuth(t *testing.T) {
	_, err := NewClient(Config{BaseURL: "https://localhost:8006"})
	if err == nil {
		t.Fatal("expected error for missing auth")
	}
}

func TestNewClientWithToken(t *testing.T) {
	c, err := NewClient(Config{
		BaseURL:            "https://localhost:8006",
		TokenID:            "root@pam!mytoken",
		Secret:             "secret-value",
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.token != "PVEAPIToken=root@pam!mytoken=secret-value" {
		t.Errorf("unexpected token: %s", c.token)
	}
}

func TestNextID(t *testing.T) {
	ts, c := testServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/cluster/nextid" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write(envelope("100"))
	})
	defer ts.Close()

	id, err := c.NextID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 100 {
		t.Errorf("expected 100, got %d", id)
	}
}

func TestListLXC(t *testing.T) {
	ts, c := testServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/nodes/pve/lxc" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		list := []LXCSummary{
			{VMID: 100, Name: "test1", Status: "running"},
			{VMID: 101, Name: "test2", Status: "stopped"},
		}
		w.Write(envelope(list))
	})
	defer ts.Close()

	items, err := c.ListLXC("pve")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].VMID != 100 || items[0].Name != "test1" {
		t.Errorf("unexpected first item: %+v", items[0])
	}
}

func TestGetLXCStatus(t *testing.T) {
	ts, c := testServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/nodes/pve/lxc/100/status/current" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		status := LXCStatus{
			VMID:   100,
			Status: "running",
			Name:   "myct",
		}
		w.Write(envelope(status))
	})
	defer ts.Close()

	s, err := c.GetLXCStatus("pve", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.VMID != 100 || s.Status != "running" {
		t.Errorf("unexpected status: %+v", s)
	}
}

func TestCreateLXC(t *testing.T) {
	ts, c := testServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api2/json/nodes/pve/lxc" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write(envelope("UPID:pve:00001234:12345678:abcdefab:vzcreate:100:root@pam:"))
	})
	defer ts.Close()

	taskID, err := c.CreateLXC(LXCConfig{
		Node:       "pve",
		VMID:       100,
		OSTemplate: "local:vztmpl/ubuntu.tar.zst",
		Cores:      2,
		Memory:     512,
		RootfsDisk: 8,
		Storage:    "local-lvm",
		NetBridge:  "vmbr0",
		NetIP:      "dhcp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskID == "" {
		t.Error("expected non-empty task ID")
	}
}

func TestStartLXC(t *testing.T) {
	ts, c := testServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/nodes/pve/lxc/100/status/start" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write(envelope("UPID:pve:start:100"))
	})
	defer ts.Close()

	_, err := c.StartLXC("pve", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStopLXC(t *testing.T) {
	ts, c := testServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/nodes/pve/lxc/100/status/stop" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write(envelope("UPID:pve:stop:100"))
	})
	defer ts.Close()

	_, err := c.StopLXC("pve", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteLXC(t *testing.T) {
	ts, c := testServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/nodes/pve/lxc/100" || r.Method != http.MethodDelete {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Write(envelope("UPID:pve:delete:100"))
	})
	defer ts.Close()

	_, err := c.DeleteLXC("pve", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetLXCInterfaces(t *testing.T) {
	ts, c := testServer(func(w http.ResponseWriter, r *http.Request) {
		ifaces := []LXCInterface{
			{Name: "eth0", Inet: "192.168.1.100/24"},
		}
		w.Write(envelope(ifaces))
	})
	defer ts.Close()

	ifaces, err := c.GetLXCInterfaces("pve", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ifaces) != 1 || ifaces[0].Inet != "192.168.1.100/24" {
		t.Errorf("unexpected interfaces: %+v", ifaces)
	}
}

func TestDecodeResponseError(t *testing.T) {
	ts, c := testServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	})
	defer ts.Close()

	_, err := c.ListLXC("pve")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
