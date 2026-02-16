// Package proxmox provides a client for the Proxmox VE REST API,
// focused on LXC container lifecycle operations.
package proxmox

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client communicates with the Proxmox VE API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string // "PVEAPIToken=user@realm!tokenid=secret" or CSRFPreventionToken
	ticket     string // authentication ticket (cookie)
	csrf       string // CSRFPreventionToken header
}

// Config holds the configuration needed to create a Client.
type Config struct {
	// BaseURL is the Proxmox API endpoint, e.g. "https://proxmox.example.com:8006".
	BaseURL string
	// TokenID is the API token in the form "user@realm!tokenid".
	TokenID string
	// Secret is the API token secret.
	Secret string
	// Username for ticket-based authentication (alternative to token).
	Username string
	// Password for ticket-based authentication.
	Password string
	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool
}

// NewClient creates a new Proxmox API client from the given configuration.
func NewClient(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("proxmox: base URL is required")
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // User-controlled option
		},
	}
	c := &Client{
		baseURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}

	if cfg.TokenID != "" && cfg.Secret != "" {
		c.token = fmt.Sprintf("PVEAPIToken=%s=%s", cfg.TokenID, cfg.Secret)
		return c, nil
	}

	if cfg.Username != "" && cfg.Password != "" {
		if err := c.authenticate(cfg.Username, cfg.Password); err != nil {
			return nil, fmt.Errorf("proxmox: authentication failed: %w", err)
		}
		return c, nil
	}

	return nil, fmt.Errorf("proxmox: either token or username/password must be provided")
}

func (c *Client) authenticate(username, password string) error {
	data := url.Values{
		"username": {username},
		"password": {password},
	}
	resp, err := c.httpClient.PostForm(c.baseURL+"/api2/json/access/ticket", data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("authentication returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Ticket              string `json:"ticket"`
			CSRFPreventionToken string `json:"CSRFPreventionToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	c.ticket = result.Data.Ticket
	c.csrf = result.Data.CSRFPreventionToken
	return nil
}

// do executes an HTTP request against the Proxmox API.
func (c *Client) do(method, path string, body io.Reader) (*http.Response, error) {
	reqURL := c.baseURL + "/api2/json" + path
	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, err
	}

	if c.token != "" {
		req.Header.Set("Authorization", c.token)
	} else if c.ticket != "" {
		req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: c.ticket})
		if c.csrf != "" && (method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete) {
			req.Header.Set("CSRFPreventionToken", c.csrf)
		}
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// decodeResponse reads a Proxmox API JSON response envelope.
func decodeResponse(resp *http.Response, target interface{}) error {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("proxmox API error (HTTP %d): %s", resp.StatusCode, string(b))
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

// LXCConfig holds the parameters for creating an LXC container.
type LXCConfig struct {
	Node         string
	VMID         int
	OSTemplate   string
	IsOCI        bool // true when OSTemplate is an OCI image reference
	Hostname     string
	Cores        int
	Memory       int // MB
	Swap         int // MB
	RootfsDisk   int // GB
	Storage      string
	NetBridge    string
	NetIP        string
	NetGateway   string
	NetFirewall  bool
	Password     string
	SSHPublicKey string
	Unprivileged bool
	StartOnBoot  bool
	Nameserver   string
	SearchDomain string
	// Networks allows specifying multiple named network interfaces (net0, net1, ...).
	Networks []LXCNetConfig
	// Environment variables for OCI containers.
	Environment map[string]string
}

// LXCNetConfig describes a single network interface for an LXC container.
type LXCNetConfig struct {
	Name     string // interface name inside container (e.g. "eth0")
	Bridge   string
	IP       string
	Gateway  string
	Firewall bool
}

// CreateLXC creates an LXC container on the given node.
func (c *Client) CreateLXC(cfg LXCConfig) (string, error) {
	params := map[string]interface{}{
		"ostemplate": cfg.OSTemplate,
		"cores":      cfg.Cores,
		"memory":     cfg.Memory,
		"rootfs":     fmt.Sprintf("%s:%d", cfg.Storage, cfg.RootfsDisk),
	}
	if cfg.VMID > 0 {
		params["vmid"] = cfg.VMID
	}
	if cfg.Hostname != "" {
		params["hostname"] = cfg.Hostname
	}
	if cfg.Swap > 0 {
		params["swap"] = cfg.Swap
	}
	if cfg.Password != "" {
		params["password"] = cfg.Password
	}
	if cfg.SSHPublicKey != "" {
		params["ssh-public-keys"] = cfg.SSHPublicKey
	}
	if cfg.Unprivileged {
		params["unprivileged"] = 1
	}
	if cfg.StartOnBoot {
		params["onboot"] = 1
	}
	if cfg.Nameserver != "" {
		params["nameserver"] = cfg.Nameserver
	}
	if cfg.SearchDomain != "" {
		params["searchdomain"] = cfg.SearchDomain
	}

	// Build network interfaces. Prefer the Networks slice; fall back to
	// the legacy single-interface fields.
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
			params[fmt.Sprintf("net%d", i)] = strings.Join(parts, ",")
		}
	} else {
		netParts := []string{"name=eth0"}
		if cfg.NetBridge != "" {
			netParts = append(netParts, "bridge="+cfg.NetBridge)
		}
		if cfg.NetIP != "" {
			netParts = append(netParts, "ip="+cfg.NetIP)
		}
		if cfg.NetGateway != "" {
			netParts = append(netParts, "gw="+cfg.NetGateway)
		}
		if cfg.NetFirewall {
			netParts = append(netParts, "firewall=1")
		}
		if len(netParts) > 1 {
			params["net0"] = strings.Join(netParts, ",")
		}
	}

	body, err := json.Marshal(params)
	if err != nil {
		return "", err
	}

	path := fmt.Sprintf("/nodes/%s/lxc", cfg.Node)
	resp, err := c.do(http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	var taskID string
	if err := decodeResponse(resp, &taskID); err != nil {
		return "", fmt.Errorf("create LXC: %w", err)
	}
	return taskID, nil
}

// StartLXC starts the LXC container identified by vmid on the given node.
func (c *Client) StartLXC(node string, vmid int) (string, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%d/status/start", node, vmid)
	resp, err := c.do(http.MethodPost, path, nil)
	if err != nil {
		return "", err
	}
	var taskID string
	if err := decodeResponse(resp, &taskID); err != nil {
		return "", fmt.Errorf("start LXC: %w", err)
	}
	return taskID, nil
}

// StopLXC stops the LXC container identified by vmid on the given node.
func (c *Client) StopLXC(node string, vmid int) (string, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%d/status/stop", node, vmid)
	resp, err := c.do(http.MethodPost, path, nil)
	if err != nil {
		return "", err
	}
	var taskID string
	if err := decodeResponse(resp, &taskID); err != nil {
		return "", fmt.Errorf("stop LXC: %w", err)
	}
	return taskID, nil
}

// DeleteLXC destroys the LXC container identified by vmid on the given node.
func (c *Client) DeleteLXC(node string, vmid int) (string, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%d", node, vmid)
	resp, err := c.do(http.MethodDelete, path, nil)
	if err != nil {
		return "", err
	}
	var taskID string
	if err := decodeResponse(resp, &taskID); err != nil {
		return "", fmt.Errorf("delete LXC: %w", err)
	}
	return taskID, nil
}

// LXCStatus represents the current status of an LXC container.
type LXCStatus struct {
	VMID   int    `json:"vmid"`
	Status string `json:"status"` // "running", "stopped"
	Name   string `json:"name"`
	CPU    float64 `json:"cpu"`
	Mem    int64  `json:"mem"`
	MaxMem int64  `json:"maxmem"`
	Disk   int64  `json:"disk"`
	MaxDisk int64 `json:"maxdisk"`
	Uptime int64  `json:"uptime"`
}

// GetLXCStatus returns the current status of the container.
func (c *Client) GetLXCStatus(node string, vmid int) (*LXCStatus, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%d/status/current", node, vmid)
	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var status LXCStatus
	if err := decodeResponse(resp, &status); err != nil {
		return nil, fmt.Errorf("get LXC status: %w", err)
	}
	return &status, nil
}

// LXCSummary represents a summary listing entry for an LXC container.
type LXCSummary struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ListLXC returns a list of all LXC containers on the given node.
func (c *Client) ListLXC(node string) ([]LXCSummary, error) {
	path := fmt.Sprintf("/nodes/%s/lxc", node)
	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var list []LXCSummary
	if err := decodeResponse(resp, &list); err != nil {
		return nil, fmt.Errorf("list LXC: %w", err)
	}
	return list, nil
}

// LXCInterface represents a network interface inside a container.
type LXCInterface struct {
	Name       string `json:"name"`
	HWAddr     string `json:"hwaddr"`
	Inet       string `json:"inet"`
	Inet6      string `json:"inet6"`
}

// GetLXCInterfaces returns the network interfaces of the container.
func (c *Client) GetLXCInterfaces(node string, vmid int) ([]LXCInterface, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%d/interfaces", node, vmid)
	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var ifaces []LXCInterface
	if err := decodeResponse(resp, &ifaces); err != nil {
		return nil, fmt.Errorf("get LXC interfaces: %w", err)
	}
	return ifaces, nil
}

// NextID returns the next available VMID on the cluster.
func (c *Client) NextID() (int, error) {
	resp, err := c.do(http.MethodGet, "/cluster/nextid", nil)
	if err != nil {
		return 0, err
	}
	var raw json.RawMessage
	if err := decodeResponse(resp, &raw); err != nil {
		return 0, fmt.Errorf("next ID: %w", err)
	}
	// The API returns the ID either as a number or a string.
	s := strings.Trim(string(raw), `"`)
	return strconv.Atoi(s)
}

// WaitForTask polls a task until it completes or the timeout is reached.
func (c *Client) WaitForTask(node, taskID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		path := fmt.Sprintf("/nodes/%s/tasks/%s/status", node, taskID)
		resp, err := c.do(http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		var ts struct {
			Status string `json:"status"`
			ExitStatus string `json:"exitstatus"`
		}
		if err := decodeResponse(resp, &ts); err != nil {
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
