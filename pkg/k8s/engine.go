// Package k8s manages a local Kubernetes engine for proxkube. It supports
// minikube for single-node setups and kubeadm for multi-node clusters.
// The engine is used to run the Prometheus operator and other monitoring
// workloads that observe the Proxmox infrastructure.
package k8s

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Mode represents the Kubernetes deployment mode.
type Mode string

const (
	// ModeMinikube uses minikube for single-node clusters.
	ModeMinikube Mode = "minikube"
	// ModeKubeadm uses kubeadm for multi-node clusters.
	ModeKubeadm Mode = "kubeadm"
)

// Config holds the configuration for the Kubernetes engine.
type Config struct {
	// Mode selects minikube or kubeadm.
	Mode Mode
	// KubeconfigPath is the path to the kubeconfig file.
	// Defaults to ~/.kube/config when empty.
	KubeconfigPath string
	// Namespace is the default namespace for proxkube workloads.
	Namespace string
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Mode:      ModeMinikube,
		Namespace: "proxkube-system",
	}
}

// Engine manages the lifecycle of a local Kubernetes cluster.
type Engine struct {
	cfg Config
}

// NewEngine creates a Kubernetes engine with the given configuration.
func NewEngine(cfg Config) *Engine {
	if cfg.Namespace == "" {
		cfg.Namespace = "proxkube-system"
	}
	return &Engine{cfg: cfg}
}

// Status represents the cluster state.
type Status struct {
	Running bool
	Mode    Mode
	Info    string
}

// IsAvailable returns true if the required binaries are present on the system.
func (e *Engine) IsAvailable() bool {
	switch e.cfg.Mode {
	case ModeMinikube:
		_, err := exec.LookPath("minikube")
		return err == nil
	case ModeKubeadm:
		_, err := exec.LookPath("kubeadm")
		return err == nil
	default:
		return false
	}
}

// Start initialises the Kubernetes cluster. For minikube this starts a
// single-node cluster; for kubeadm it runs kubeadm init.
func (e *Engine) Start() error {
	switch e.cfg.Mode {
	case ModeMinikube:
		return e.startMinikube()
	case ModeKubeadm:
		return e.startKubeadm()
	default:
		return fmt.Errorf("k8s: unsupported mode %q", e.cfg.Mode)
	}
}

// Stop stops the Kubernetes cluster.
func (e *Engine) Stop() error {
	switch e.cfg.Mode {
	case ModeMinikube:
		return runCmd("minikube", "stop")
	case ModeKubeadm:
		return runCmd("kubeadm", "reset", "--force")
	default:
		return fmt.Errorf("k8s: unsupported mode %q", e.cfg.Mode)
	}
}

// GetStatus returns the current cluster status.
func (e *Engine) GetStatus() (*Status, error) {
	switch e.cfg.Mode {
	case ModeMinikube:
		return e.minikubeStatus()
	case ModeKubeadm:
		return e.kubeadmStatus()
	default:
		return nil, fmt.Errorf("k8s: unsupported mode %q", e.cfg.Mode)
	}
}

// EnsureNamespace creates the proxkube namespace if it does not exist.
func (e *Engine) EnsureNamespace() error {
	args := []string{"get", "namespace", e.cfg.Namespace}
	if e.cfg.KubeconfigPath != "" {
		args = append([]string{"--kubeconfig", e.cfg.KubeconfigPath}, args...)
	}
	if err := runCmd("kubectl", args...); err == nil {
		return nil // namespace exists
	}
	createArgs := []string{"create", "namespace", e.cfg.Namespace}
	if e.cfg.KubeconfigPath != "" {
		createArgs = append([]string{"--kubeconfig", e.cfg.KubeconfigPath}, createArgs...)
	}
	return runCmd("kubectl", createArgs...)
}

// ApplyManifest applies a YAML manifest to the cluster.
func (e *Engine) ApplyManifest(manifest string) error {
	args := []string{"apply", "-f", "-"}
	if e.cfg.KubeconfigPath != "" {
		args = append([]string{"--kubeconfig", e.cfg.KubeconfigPath}, args...)
	}
	return runCmdStdin("kubectl", manifest, args...)
}

// Mode returns the configured engine mode.
func (e *Engine) GetMode() Mode {
	return e.cfg.Mode
}

func (e *Engine) startMinikube() error {
	return runCmd("minikube", "start", "--driver=docker", "--wait=all")
}

func (e *Engine) startKubeadm() error {
	return runCmd("kubeadm", "init", "--pod-network-cidr=10.244.0.0/16")
}

func (e *Engine) minikubeStatus() (*Status, error) {
	out, err := runCmdOutput("minikube", "status", "--format={{.Host}}")
	if err != nil {
		return &Status{Running: false, Mode: ModeMinikube, Info: "not running"}, nil
	}
	host := strings.TrimSpace(out)
	running := host == "Running"
	return &Status{
		Running: running,
		Mode:    ModeMinikube,
		Info:    "host=" + host,
	}, nil
}

func (e *Engine) kubeadmStatus() (*Status, error) {
	args := []string{"get", "nodes", "--no-headers"}
	if e.cfg.KubeconfigPath != "" {
		args = append([]string{"--kubeconfig", e.cfg.KubeconfigPath}, args...)
	}
	out, err := runCmdOutput("kubectl", args...)
	if err != nil {
		return &Status{Running: false, Mode: ModeKubeadm, Info: "cluster not reachable"}, nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return &Status{
		Running: len(lines) > 0 && lines[0] != "",
		Mode:    ModeKubeadm,
		Info:    fmt.Sprintf("%d node(s)", len(lines)),
	}, nil
}

// runCmd executes a command and returns an error if it fails.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...) //nolint:gosec // args from internal calls
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, stderr.String())
	}
	return nil
}

// runCmdOutput executes a command and returns its stdout.
func runCmdOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...) //nolint:gosec // args from internal calls
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

// runCmdStdin executes a command with the given string as stdin.
func runCmdStdin(name, stdin string, args ...string) error {
	cmd := exec.Command(name, args...) //nolint:gosec // args from internal calls
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, stderr.String())
	}
	return nil
}
