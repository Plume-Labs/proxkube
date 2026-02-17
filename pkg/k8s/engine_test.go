package k8s

import (
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Mode != ModeMinikube {
		t.Errorf("default mode = %q, want %q", cfg.Mode, ModeMinikube)
	}
	if cfg.Namespace != "proxkube-system" {
		t.Errorf("default namespace = %q, want %q", cfg.Namespace, "proxkube-system")
	}
}

func TestNewEngine(t *testing.T) {
	e := NewEngine(Config{Mode: ModeKubeadm})
	if e.GetMode() != ModeKubeadm {
		t.Errorf("mode = %q, want %q", e.GetMode(), ModeKubeadm)
	}
	if e.cfg.Namespace != "proxkube-system" {
		t.Errorf("namespace = %q, want %q", e.cfg.Namespace, "proxkube-system")
	}
}

func TestNewEngineCustomNamespace(t *testing.T) {
	e := NewEngine(Config{Mode: ModeMinikube, Namespace: "custom-ns"})
	if e.cfg.Namespace != "custom-ns" {
		t.Errorf("namespace = %q, want %q", e.cfg.Namespace, "custom-ns")
	}
}

func TestModeConstants(t *testing.T) {
	if ModeMinikube != "minikube" {
		t.Errorf("ModeMinikube = %q, want %q", ModeMinikube, "minikube")
	}
	if ModeKubeadm != "kubeadm" {
		t.Errorf("ModeKubeadm = %q, want %q", ModeKubeadm, "kubeadm")
	}
}

func TestIsAvailableUnsupportedMode(t *testing.T) {
	e := NewEngine(Config{Mode: "unknown"})
	if e.IsAvailable() {
		t.Error("IsAvailable should return false for unknown mode")
	}
}

func TestStartUnsupportedMode(t *testing.T) {
	e := NewEngine(Config{Mode: "unknown"})
	err := e.Start()
	if err == nil {
		t.Fatal("Start should fail for unknown mode")
	}
	if !strings.Contains(err.Error(), "unsupported mode") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "unsupported mode")
	}
}

func TestStopUnsupportedMode(t *testing.T) {
	e := NewEngine(Config{Mode: "unknown"})
	err := e.Stop()
	if err == nil {
		t.Fatal("Stop should fail for unknown mode")
	}
	if !strings.Contains(err.Error(), "unsupported mode") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "unsupported mode")
	}
}

func TestGetStatusUnsupportedMode(t *testing.T) {
	e := NewEngine(Config{Mode: "unknown"})
	_, err := e.GetStatus()
	if err == nil {
		t.Fatal("GetStatus should fail for unknown mode")
	}
}

func TestPrometheusManifests(t *testing.T) {
	manifest := PrometheusManifests("test-ns")
	if !strings.Contains(manifest, "namespace: test-ns") {
		t.Error("manifest should contain custom namespace")
	}
	if !strings.Contains(manifest, "proxkube-prometheus") {
		t.Error("manifest should contain proxkube-prometheus service account")
	}
	if !strings.Contains(manifest, "scrape_configs") {
		t.Error("manifest should contain scrape config")
	}
}

func TestPrometheusManifestsDefaultNamespace(t *testing.T) {
	manifest := PrometheusManifests("")
	if !strings.Contains(manifest, "namespace: proxkube-system") {
		t.Error("manifest should use default namespace when empty")
	}
}

func TestServiceMonitorManifest(t *testing.T) {
	manifest := ServiceMonitorManifest("test-ns")
	if !strings.Contains(manifest, "kind: ServiceMonitor") {
		t.Error("manifest should contain ServiceMonitor kind")
	}
	if !strings.Contains(manifest, "namespace: test-ns") {
		t.Error("manifest should contain custom namespace")
	}
}

func TestServiceMonitorManifestDefaultNamespace(t *testing.T) {
	manifest := ServiceMonitorManifest("")
	if !strings.Contains(manifest, "namespace: proxkube-system") {
		t.Error("manifest should use default namespace when empty")
	}
}
