// Package hypervisor provides low-level communication with the Proxmox
// hypervisor, bypassing the REST API for operations that benefit from
// direct host access. It talks to LXC containers via the pct CLI, reads
// cgroup metrics from /sys, and communicates with the PVE daemon through
// its Unix socket (/var/run/pvedaemon/socket).
package hypervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/GothShoot/proxkube/pkg/proxmox"
)

// Client provides low-level access to the Proxmox hypervisor. It
// implements the controller.ProxmoxAPI interface so it can be used as a
// drop-in replacement for the REST client on the Proxmox host itself.
type Client struct {
	// socketPath is the path to the PVE daemon Unix socket.
	socketPath string
	// httpClient is preconfigured to talk over the Unix socket.
	httpClient *http.Client
	// pctPath is the path to the pct binary.
	pctPath string
	// node is the local Proxmox node name.
	node string
}

// Config holds the configuration for a hypervisor Client.
type Config struct {
	// SocketPath is the PVE daemon Unix socket path.
	// Defaults to "/var/run/pvedaemon/socket" when empty.
	SocketPath string
	// PctPath is the path to the pct binary.
	// Defaults to "pct" (found via $PATH) when empty.
	PctPath string
	// Node is the local Proxmox node name.
	// Defaults to the hostname when empty.
	Node string
}

// NewClient creates a hypervisor Client that communicates with PVE via
// its Unix domain socket and the pct CLI.
func NewClient(cfg Config) (*Client, error) {
	if cfg.SocketPath == "" {
		cfg.SocketPath = "/var/run/pvedaemon/socket"
	}
	if cfg.PctPath == "" {
		cfg.PctPath = "pct"
	}
	if cfg.Node == "" {
		out, err := exec.Command("hostname", "-s").Output()
		if err != nil {
			return nil, fmt.Errorf("hypervisor: cannot determine hostname: %w", err)
		}
		cfg.Node = strings.TrimSpace(string(out))
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			deadline, ok := ctx.Deadline()
			if ok {
				return net.DialTimeout("unix", cfg.SocketPath, time.Until(deadline))
			}
			return net.Dial("unix", cfg.SocketPath)
		},
	}

	return &Client{
		socketPath: cfg.SocketPath,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		pctPath: cfg.PctPath,
		node:    cfg.Node,
	}, nil
}

// Node returns the local node name.
func (c *Client) Node() string {
	return c.node
}

// ---- controller.ProxmoxAPI implementation ----

// NextID returns the next available VMID by calling the PVE daemon socket.
func (c *Client) NextID() (int, error) {
	var raw json.RawMessage
	if err := c.socketRequest("GET", "/api2/json/cluster/nextid", nil, &raw); err != nil {
		return 0, fmt.Errorf("next ID: %w", err)
	}
	s := strings.Trim(string(raw), `"`)
	return strconv.Atoi(s)
}

// CreateLXC creates an LXC container using the pct command-line tool.
func (c *Client) CreateLXC(cfg proxmox.LXCConfig) (string, error) {
	args := []string{
		"create", strconv.Itoa(cfg.VMID), cfg.OSTemplate,
		"--hostname", cfg.Hostname,
		"--cores", strconv.Itoa(cfg.Cores),
		"--memory", strconv.Itoa(cfg.Memory),
		"--rootfs", fmt.Sprintf("%s:%d", cfg.Storage, cfg.RootfsDisk),
	}

	if cfg.Swap > 0 {
		args = append(args, "--swap", strconv.Itoa(cfg.Swap))
	}
	if cfg.Unprivileged {
		args = append(args, "--unprivileged", "1")
	}
	if cfg.StartOnBoot {
		args = append(args, "--onboot", "1")
	}
	if cfg.Password != "" {
		args = append(args, "--password", cfg.Password)
	}
	if cfg.SSHPublicKey != "" {
		args = append(args, "--ssh-public-keys", cfg.SSHPublicKey)
	}
	if cfg.Nameserver != "" {
		args = append(args, "--nameserver", cfg.Nameserver)
	}
	if cfg.SearchDomain != "" {
		args = append(args, "--searchdomain", cfg.SearchDomain)
	}
	if cfg.Tags != "" {
		args = append(args, "--tags", cfg.Tags)
	}
	if cfg.Pool != "" {
		args = append(args, "--pool", cfg.Pool)
	}
	if cfg.Description != "" {
		args = append(args, "--description", cfg.Description)
	}

	// Network interfaces.
	if len(cfg.Networks) > 0 {
		for i, net := range cfg.Networks {
			ifName := net.Name
			if ifName == "" {
				ifName = fmt.Sprintf("eth%d", i)
			}
			parts := []string{"name=" + ifName}
			if net.Bridge != "" {
				parts = append(parts, "bridge="+net.Bridge)
			}
			if net.IP != "" {
				parts = append(parts, "ip="+net.IP)
			}
			if net.Gateway != "" {
				parts = append(parts, "gw="+net.Gateway)
			}
			if net.Firewall {
				parts = append(parts, "firewall=1")
			}
			args = append(args, fmt.Sprintf("--net%d", i), strings.Join(parts, ","))
		}
	}

	// Mount points.
	for i, mp := range cfg.MountPoints {
		mpVal := fmt.Sprintf("%s:%d,mp=%s", mp.Storage, mp.Size, mp.MountPath)
		if mp.ReadOnly {
			mpVal += ",ro=1"
		}
		if mp.Backup {
			mpVal += ",backup=1"
		}
		args = append(args, fmt.Sprintf("--mp%d", i), mpVal)
	}

	out, err := c.runPct(args...)
	if err != nil {
		return "", fmt.Errorf("pct create: %w: %s", err, out)
	}

	// pct create outputs a task ID (UPID) on success.
	taskID := strings.TrimSpace(out)
	if taskID == "" {
		taskID = fmt.Sprintf("UPID:%s:pct-create:%d", c.node, cfg.VMID)
	}
	return taskID, nil
}

// StartLXC starts a container using pct.
func (c *Client) StartLXC(node string, vmid int) (string, error) {
	out, err := c.runPct("start", strconv.Itoa(vmid))
	if err != nil {
		return "", fmt.Errorf("pct start: %w: %s", err, out)
	}
	return fmt.Sprintf("UPID:%s:pct-start:%d", c.node, vmid), nil
}

// StopLXC stops a container using pct.
func (c *Client) StopLXC(node string, vmid int) (string, error) {
	out, err := c.runPct("stop", strconv.Itoa(vmid))
	if err != nil {
		return "", fmt.Errorf("pct stop: %w: %s", err, out)
	}
	return fmt.Sprintf("UPID:%s:pct-stop:%d", c.node, vmid), nil
}

// DeleteLXC destroys a container using pct.
func (c *Client) DeleteLXC(node string, vmid int) (string, error) {
	out, err := c.runPct("destroy", strconv.Itoa(vmid), "--purge")
	if err != nil {
		return "", fmt.Errorf("pct destroy: %w: %s", err, out)
	}
	return fmt.Sprintf("UPID:%s:pct-destroy:%d", c.node, vmid), nil
}

// GetLXCStatus returns the current status by querying the PVE daemon socket.
func (c *Client) GetLXCStatus(node string, vmid int) (*proxmox.LXCStatus, error) {
	var status proxmox.LXCStatus
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/status/current", node, vmid)
	if err := c.socketRequest("GET", path, nil, &status); err != nil {
		return nil, fmt.Errorf("get LXC status: %w", err)
	}
	return &status, nil
}

// ListLXC lists all LXC containers via the PVE daemon socket.
func (c *Client) ListLXC(node string) ([]proxmox.LXCSummary, error) {
	var list []proxmox.LXCSummary
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc", node)
	if err := c.socketRequest("GET", path, nil, &list); err != nil {
		return nil, fmt.Errorf("list LXC: %w", err)
	}
	return list, nil
}

// GetLXCInterfaces returns the network interfaces of a container via the PVE
// daemon socket.
func (c *Client) GetLXCInterfaces(node string, vmid int) ([]proxmox.LXCInterface, error) {
	var ifaces []proxmox.LXCInterface
	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/interfaces", node, vmid)
	if err := c.socketRequest("GET", path, nil, &ifaces); err != nil {
		return nil, fmt.Errorf("get LXC interfaces: %w", err)
	}
	return ifaces, nil
}

// WaitForTask waits for a task to complete. For pct-originated tasks the
// operation is already synchronous, so this is a no-op for those. For real
// UPIDs it polls the PVE daemon socket.
func (c *Client) WaitForTask(node, taskID string, timeout time.Duration) error {
	// pct commands are synchronous — if we generated the UPID ourselves
	// (containing "pct-") the operation has already completed.
	if strings.Contains(taskID, "pct-") {
		return nil
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		path := fmt.Sprintf("/api2/json/nodes/%s/tasks/%s/status", node, taskID)
		var ts struct {
			Status     string `json:"status"`
			ExitStatus string `json:"exitstatus"`
		}
		if err := c.socketRequest("GET", path, nil, &ts); err != nil {
			return err
		}
		if ts.Status == "stopped" {
			if ts.ExitStatus == "OK" {
				return nil
			}
			return fmt.Errorf("task %s failed: %s", taskID, ts.ExitStatus)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("task %s timed out after %s", taskID, timeout)
}

// ---- Low-level helpers ----

// Exec runs a command inside a running container via pct exec.
func (c *Client) Exec(vmid int, command []string) (string, error) {
	args := append([]string{"exec", strconv.Itoa(vmid), "--"}, command...)
	out, err := c.runPct(args...)
	if err != nil {
		return "", fmt.Errorf("pct exec: %w: %s", err, out)
	}
	return out, nil
}

// Push copies a file into the container via pct push.
func (c *Client) Push(vmid int, src, dst string) error {
	out, err := c.runPct("push", strconv.Itoa(vmid), src, dst)
	if err != nil {
		return fmt.Errorf("pct push: %w: %s", err, out)
	}
	return nil
}

// Pull copies a file from the container via pct pull.
func (c *Client) Pull(vmid int, src, dst string) error {
	out, err := c.runPct("pull", strconv.Itoa(vmid), src, dst)
	if err != nil {
		return fmt.Errorf("pct pull: %w: %s", err, out)
	}
	return nil
}

// Enter opens a console to the container (pct enter). Returns an error
// because this should be called from a terminal.
func (c *Client) Enter(vmid int) error {
	cmd := exec.Command(c.pctPath, "enter", strconv.Itoa(vmid)) //nolint:gosec // VMID is int
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// runPct executes a pct command and returns the combined output.
func (c *Client) runPct(args ...string) (string, error) {
	cmd := exec.Command(c.pctPath, args...) //nolint:gosec // args from internal calls
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// socketRequest sends an HTTP request to the PVE daemon Unix socket and
// decodes the standard Proxmox API envelope.
func (c *Client) socketRequest(method, path string, body io.Reader, target interface{}) error {
	// The Unix-socket HTTP transport uses "localhost" as a placeholder host.
	reqURL := "http://localhost" + path
	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("socket request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("socket API error (HTTP %d): %s", resp.StatusCode, string(b))
	}

	if target == nil {
		return nil
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	return json.Unmarshal(envelope.Data, target)
}
