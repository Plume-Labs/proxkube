package daemon

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GothShoot/proxkube/pkg/proxmox"
)

// mockWatcher implements ProxmoxWatcher for testing.
type mockWatcher struct {
	mu         sync.Mutex
	containers map[string][]proxmox.LXCSummary
}

func newMockWatcher() *mockWatcher {
	return &mockWatcher{
		containers: make(map[string][]proxmox.LXCSummary),
	}
}

func (m *mockWatcher) ListLXC(node string) ([]proxmox.LXCSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.containers[node], nil
}

func (m *mockWatcher) setContainers(node string, cts []proxmox.LXCSummary) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.containers[node] = cts
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 30*time.Second)
	}
	if len(cfg.Nodes) != 1 || cfg.Nodes[0] != "pve" {
		t.Errorf("Nodes = %v, want [pve]", cfg.Nodes)
	}
}

func TestDaemonDetectsAddition(t *testing.T) {
	watcher := newMockWatcher()
	var events []ChangeEvent
	var mu sync.Mutex

	cfg := Config{
		PollInterval: 50 * time.Millisecond,
		Nodes:        []string{"node1"},
		OnChange: func(event ChangeEvent) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		},
	}

	d := New(cfg, watcher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the daemon in a goroutine.
	go d.Run(ctx) //nolint:errcheck

	// Wait for initial poll to complete.
	time.Sleep(100 * time.Millisecond)

	// Add a container.
	watcher.setContainers("node1", []proxmox.LXCSummary{
		{VMID: 100, Name: "test-pod", Status: "running"},
	})

	// Wait for next poll.
	time.Sleep(100 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != ChangeAdded {
		t.Errorf("event type = %q, want %q", events[0].Type, ChangeAdded)
	}
	if events[0].Name != "test-pod" {
		t.Errorf("event name = %q, want %q", events[0].Name, "test-pod")
	}
}

func TestDaemonDetectsRemoval(t *testing.T) {
	watcher := newMockWatcher()
	watcher.setContainers("node1", []proxmox.LXCSummary{
		{VMID: 100, Name: "test-pod", Status: "running"},
	})

	var events []ChangeEvent
	var mu sync.Mutex

	cfg := Config{
		PollInterval: 50 * time.Millisecond,
		Nodes:        []string{"node1"},
		OnChange: func(event ChangeEvent) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		},
	}

	d := New(cfg, watcher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Run(ctx) //nolint:errcheck

	// Wait for initial poll to establish state.
	time.Sleep(100 * time.Millisecond)

	// Remove the container.
	watcher.setContainers("node1", []proxmox.LXCSummary{})

	// Wait for next poll.
	time.Sleep(100 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()

	// First event is the initial "added" from the first poll;
	// second event should be "removed".
	found := false
	for _, ev := range events {
		if ev.Type == ChangeRemoved && ev.VMID == 100 {
			found = true
		}
	}
	if !found {
		t.Error("expected a ChangeRemoved event for VMID 100")
	}
}

func TestDaemonDetectsStatusChange(t *testing.T) {
	watcher := newMockWatcher()
	watcher.setContainers("node1", []proxmox.LXCSummary{
		{VMID: 100, Name: "test-pod", Status: "running"},
	})

	var events []ChangeEvent
	var mu sync.Mutex

	cfg := Config{
		PollInterval: 50 * time.Millisecond,
		Nodes:        []string{"node1"},
		OnChange: func(event ChangeEvent) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		},
	}

	d := New(cfg, watcher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.Run(ctx) //nolint:errcheck

	// Wait for initial poll.
	time.Sleep(100 * time.Millisecond)

	// Change status.
	watcher.setContainers("node1", []proxmox.LXCSummary{
		{VMID: 100, Name: "test-pod", Status: "stopped"},
	})

	// Wait for next poll.
	time.Sleep(100 * time.Millisecond)
	cancel()

	mu.Lock()
	defer mu.Unlock()

	found := false
	for _, ev := range events {
		if ev.Type == ChangeStatus && ev.VMID == 100 {
			if ev.OldStatus != "running" || ev.Status != "stopped" {
				t.Errorf("status change: old=%q new=%q, want running->stopped", ev.OldStatus, ev.Status)
			}
			found = true
		}
	}
	if !found {
		t.Error("expected a ChangeStatus event for VMID 100")
	}
}

func TestSnapshot(t *testing.T) {
	watcher := newMockWatcher()
	watcher.setContainers("node1", []proxmox.LXCSummary{
		{VMID: 100, Name: "pod-a", Status: "running"},
		{VMID: 101, Name: "pod-b", Status: "stopped"},
	})

	d := New(Config{
		PollInterval: time.Hour, // won't tick in test
		Nodes:        []string{"node1"},
	}, watcher)

	// Manual poll to populate state.
	d.poll()

	snap := d.Snapshot()
	if len(snap["node1"]) != 2 {
		t.Errorf("snapshot has %d containers, want 2", len(snap["node1"]))
	}
}

func TestFormatEvent(t *testing.T) {
	ts := time.Date(2026, 2, 17, 6, 0, 0, 0, time.UTC)

	tests := []struct {
		event    ChangeEvent
		contains string
	}{
		{
			event:    ChangeEvent{Type: ChangeAdded, Node: "pve", Name: "web", VMID: 100, Status: "running", Timestamp: ts},
			contains: "ADDED",
		},
		{
			event:    ChangeEvent{Type: ChangeRemoved, Node: "pve", Name: "web", VMID: 100, Timestamp: ts},
			contains: "REMOVED",
		},
		{
			event:    ChangeEvent{Type: ChangeStatus, Node: "pve", Name: "web", VMID: 100, OldStatus: "running", Status: "stopped", Timestamp: ts},
			contains: "running -> stopped",
		},
	}

	for _, tt := range tests {
		out := FormatEvent(tt.event)
		if !strings.Contains(out, tt.contains) {
			t.Errorf("FormatEvent(%v) = %q, want to contain %q", tt.event.Type, out, tt.contains)
		}
	}
}

func TestChangeTypeConstants(t *testing.T) {
	if ChangeAdded != "added" {
		t.Errorf("ChangeAdded = %q", ChangeAdded)
	}
	if ChangeRemoved != "removed" {
		t.Errorf("ChangeRemoved = %q", ChangeRemoved)
	}
	if ChangeStatus != "status" {
		t.Errorf("ChangeStatus = %q", ChangeStatus)
	}
}
