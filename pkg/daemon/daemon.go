// Package daemon implements the proxkube monitoring daemon. It watches the
// Proxmox VE cluster for configuration changes (container lifecycle events,
// resource modifications) and reconciles monitoring state through the
// Kubernetes Prometheus operator.
//
// The daemon periodically polls the Proxmox API for the list of containers
// on each configured node, detects additions, deletions, and status changes,
// and emits events that can be consumed by the monitoring pipeline.
package daemon

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/GothShoot/proxkube/pkg/proxmox"
)

// Config holds the daemon configuration.
type Config struct {
	// PollInterval is the time between Proxmox state polls.
	PollInterval time.Duration
	// Nodes is the list of Proxmox nodes to monitor.
	Nodes []string
	// ProxmoxConfig is used to create the Proxmox API client.
	ProxmoxConfig proxmox.Config
	// OnChange is called whenever a container change is detected.
	// If nil, changes are logged but not forwarded.
	OnChange func(event ChangeEvent)
}

// DefaultConfig returns a daemon configuration with sensible defaults.
func DefaultConfig() Config {
	return Config{
		PollInterval: 30 * time.Second,
		Nodes:        []string{"pve"},
	}
}

// ChangeType identifies the kind of configuration change detected.
type ChangeType string

const (
	ChangeAdded   ChangeType = "added"
	ChangeRemoved ChangeType = "removed"
	ChangeStatus  ChangeType = "status"
)

// ChangeEvent describes a single detected change on a Proxmox node.
type ChangeEvent struct {
	Type      ChangeType
	Node      string
	VMID      int
	Name      string
	Status    string
	OldStatus string
	Timestamp time.Time
}

// Daemon watches Proxmox for changes and manages the monitoring pipeline.
type Daemon struct {
	cfg    Config
	client ProxmoxWatcher
	// state tracks the last known container state per node.
	state map[string]map[int]proxmox.LXCSummary
	mu    sync.Mutex
}

// ProxmoxWatcher is the subset of the Proxmox client needed by the daemon.
type ProxmoxWatcher interface {
	ListLXC(node string) ([]proxmox.LXCSummary, error)
}

// New creates a new daemon with the given configuration and Proxmox client.
func New(cfg Config, client ProxmoxWatcher) *Daemon {
	return &Daemon{
		cfg:    cfg,
		client: client,
		state:  make(map[string]map[int]proxmox.LXCSummary),
	}
}

// Run starts the daemon loop. It blocks until the context is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	log.Printf("proxkube-daemon: starting (poll every %s, nodes: %v)", d.cfg.PollInterval, d.cfg.Nodes)

	// Initial poll to establish baseline state.
	d.poll()

	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("proxkube-daemon: stopping")
			return ctx.Err()
		case <-ticker.C:
			d.poll()
		}
	}
}

// poll queries every configured node and detects changes.
func (d *Daemon) poll() {
	for _, node := range d.cfg.Nodes {
		containers, err := d.client.ListLXC(node)
		if err != nil {
			log.Printf("proxkube-daemon: error polling node %s: %v", node, err)
			continue
		}
		d.reconcile(node, containers)
	}
}

// reconcile compares the current container list against the last known
// state and emits change events for any differences.
func (d *Daemon) reconcile(node string, current []proxmox.LXCSummary) {
	d.mu.Lock()
	defer d.mu.Unlock()

	prev := d.state[node]
	if prev == nil {
		prev = make(map[int]proxmox.LXCSummary)
	}

	cur := make(map[int]proxmox.LXCSummary)
	for _, ct := range current {
		cur[ct.VMID] = ct
	}

	now := time.Now()

	// Detect additions and status changes.
	for vmid, ct := range cur {
		old, exists := prev[vmid]
		if !exists {
			d.emit(ChangeEvent{
				Type:      ChangeAdded,
				Node:      node,
				VMID:      vmid,
				Name:      ct.Name,
				Status:    ct.Status,
				Timestamp: now,
			})
		} else if old.Status != ct.Status {
			d.emit(ChangeEvent{
				Type:      ChangeStatus,
				Node:      node,
				VMID:      vmid,
				Name:      ct.Name,
				Status:    ct.Status,
				OldStatus: old.Status,
				Timestamp: now,
			})
		}
	}

	// Detect removals.
	for vmid, ct := range prev {
		if _, exists := cur[vmid]; !exists {
			d.emit(ChangeEvent{
				Type:      ChangeRemoved,
				Node:      node,
				VMID:      vmid,
				Name:      ct.Name,
				Status:    ct.Status,
				Timestamp: now,
			})
		}
	}

	d.state[node] = cur
}

// emit dispatches a change event.
func (d *Daemon) emit(event ChangeEvent) {
	log.Printf("proxkube-daemon: %s %s/%s (VMID %d, status: %s)",
		event.Type, event.Node, event.Name, event.VMID, event.Status)
	if d.cfg.OnChange != nil {
		d.cfg.OnChange(event)
	}
}

// Snapshot returns a copy of the current state for all monitored nodes.
func (d *Daemon) Snapshot() map[string][]proxmox.LXCSummary {
	d.mu.Lock()
	defer d.mu.Unlock()

	result := make(map[string][]proxmox.LXCSummary, len(d.state))
	for node, containers := range d.state {
		list := make([]proxmox.LXCSummary, 0, len(containers))
		for _, ct := range containers {
			list = append(list, ct)
		}
		result[node] = list
	}
	return result
}

// FormatEvent returns a human-readable representation of a change event.
func FormatEvent(e ChangeEvent) string {
	switch e.Type {
	case ChangeAdded:
		return fmt.Sprintf("[%s] ADDED %s/%s (VMID %d) status=%s",
			e.Timestamp.Format(time.RFC3339), e.Node, e.Name, e.VMID, e.Status)
	case ChangeRemoved:
		return fmt.Sprintf("[%s] REMOVED %s/%s (VMID %d)",
			e.Timestamp.Format(time.RFC3339), e.Node, e.Name, e.VMID)
	case ChangeStatus:
		return fmt.Sprintf("[%s] STATUS %s/%s (VMID %d) %s -> %s",
			e.Timestamp.Format(time.RFC3339), e.Node, e.Name, e.VMID, e.OldStatus, e.Status)
	default:
		return fmt.Sprintf("[%s] UNKNOWN event on %s/%s", e.Timestamp.Format(time.RFC3339), e.Node, e.Name)
	}
}
