package compose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GothShoot/proxkube/pkg/api"
)

const sampleCompose = `services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
    environment:
      - NGINX_HOST=localhost
    networks:
      - frontend
    depends_on:
      - db
    deploy:
      resources:
        limits:
          cpus: "2"
          memory: 1g
    restart: always

  db:
    image: postgres:16
    environment:
      POSTGRES_USER: myuser
      POSTGRES_DB: mydb
    volumes:
      - db_data:/var/lib/postgresql/data
    networks:
      - frontend
      - backend

networks:
  frontend:
    internal: false
  backend:
    internal: true
    ipam:
      config:
        - subnet: 10.10.0.0/24
          gateway: 10.10.0.1

volumes:
  db_data: {}
`

func writeTmpCompose(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadComposeFile(t *testing.T) {
	path := writeTmpCompose(t, sampleCompose)
	cf, err := LoadComposeFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cf.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(cf.Services))
	}
	if cf.Services["web"].Image != "nginx:latest" {
		t.Errorf("unexpected web image: %s", cf.Services["web"].Image)
	}
	if len(cf.Networks) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(cf.Networks))
	}
	if !cf.Networks["backend"].Internal {
		t.Error("expected backend network to be internal")
	}
}

func TestLoadComposeFileNotFound(t *testing.T) {
	_, err := LoadComposeFile("/nonexistent/compose.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadComposeFileEmpty(t *testing.T) {
	path := writeTmpCompose(t, "services: {}")
	_, err := LoadComposeFile(path)
	if err == nil {
		t.Fatal("expected error for empty services")
	}
}

func TestToStack(t *testing.T) {
	path := writeTmpCompose(t, sampleCompose)
	cf, err := LoadComposeFile(path)
	if err != nil {
		t.Fatal(err)
	}

	opts := DefaultConvertOptions()
	stack, err := cf.ToStack("myapp", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stack.Name != "myapp" {
		t.Errorf("expected stack name myapp, got %s", stack.Name)
	}
	if len(stack.Pods) != 2 {
		t.Fatalf("expected 2 pods, got %d", len(stack.Pods))
	}

	// Find web pod.
	var webPod, dbPod *api.Pod
	for i := range stack.Pods {
		switch stack.Pods[i].Metadata.Name {
		case "web":
			webPod = &stack.Pods[i]
		case "db":
			dbPod = &stack.Pods[i]
		}
	}

	if webPod == nil {
		t.Fatal("web pod not found")
	}
	if dbPod == nil {
		t.Fatal("db pod not found")
	}

	// Web pod checks.
	if webPod.Spec.Image != "nginx:latest" {
		t.Errorf("expected nginx image, got %s", webPod.Spec.Image)
	}
	if !webPod.Spec.Expose {
		t.Error("web pod should be exposed (has ports)")
	}
	if len(webPod.Spec.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(webPod.Spec.Ports))
	}
	if webPod.Spec.Ports[0].HostPort != 8080 || webPod.Spec.Ports[0].ContainerPort != 80 {
		t.Errorf("unexpected port mapping: %+v", webPod.Spec.Ports[0])
	}
	if webPod.Spec.Resources.CPU != 2 {
		t.Errorf("expected 2 CPUs, got %d", webPod.Spec.Resources.CPU)
	}
	if webPod.Spec.Resources.Memory != 1024 {
		t.Errorf("expected 1024 MB memory, got %d", webPod.Spec.Resources.Memory)
	}
	if !webPod.Spec.StartOnBoot {
		t.Error("expected startOnBoot for restart=always")
	}
	if len(webPod.Spec.DependsOn) != 1 || webPod.Spec.DependsOn[0] != "db" {
		t.Errorf("unexpected dependsOn: %v", webPod.Spec.DependsOn)
	}

	// DB pod checks.
	if dbPod.Spec.Image != "postgres:16" {
		t.Errorf("expected postgres image, got %s", dbPod.Spec.Image)
	}
	if dbPod.Spec.Environment["POSTGRES_USER"] != "myuser" {
		t.Errorf("unexpected env: %v", dbPod.Spec.Environment)
	}
	if len(dbPod.Spec.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(dbPod.Spec.Volumes))
	}
	if dbPod.Spec.Volumes[0].MountPath != "/var/lib/postgresql/data" {
		t.Errorf("unexpected mount: %s", dbPod.Spec.Volumes[0].MountPath)
	}

	// Network checks.
	if _, ok := stack.Networks["backend"]; !ok {
		t.Error("backend network missing from stack")
	}
	if stack.Networks["backend"].Subnet != "10.10.0.0/24" {
		t.Errorf("unexpected subnet: %s", stack.Networks["backend"].Subnet)
	}
}

func TestParsePort(t *testing.T) {
	tests := []struct {
		in   string
		host int
		ct   int
		prot string
	}{
		{"8080:80", 8080, 80, "tcp"},
		{"443:443/udp", 443, 443, "udp"},
		{"3000", 3000, 3000, "tcp"},
	}
	for _, tc := range tests {
		pm, err := parsePort(tc.in)
		if err != nil {
			t.Errorf("parsePort(%q) error: %v", tc.in, err)
			continue
		}
		if pm.HostPort != tc.host || pm.ContainerPort != tc.ct || pm.Protocol != tc.prot {
			t.Errorf("parsePort(%q) = %+v, want host=%d ct=%d proto=%s",
				tc.in, pm, tc.host, tc.ct, tc.prot)
		}
	}
}

func TestParsePortInvalid(t *testing.T) {
	_, err := parsePort("abc:def")
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
}

func TestParseEnvironmentList(t *testing.T) {
	env := parseEnvironment([]interface{}{"FOO=bar", "BAZ=qux"})
	if env["FOO"] != "bar" || env["BAZ"] != "qux" {
		t.Errorf("unexpected env: %v", env)
	}
}

func TestParseEnvironmentMap(t *testing.T) {
	env := parseEnvironment(map[string]interface{}{"KEY": "val"})
	if env["KEY"] != "val" {
		t.Errorf("unexpected env: %v", env)
	}
}

func TestParseEnvironmentNil(t *testing.T) {
	env := parseEnvironment(nil)
	if env != nil {
		t.Errorf("expected nil, got %v", env)
	}
}

func TestParseMemoryMB(t *testing.T) {
	tests := []struct {
		in  string
		out int
	}{
		{"512m", 512},
		{"1g", 1024},
		{"256", 256},
		{"2gb", 2048},
		{"512mb", 512},
	}
	for _, tc := range tests {
		got := parseMemoryMB(tc.in, 512)
		if got != tc.out {
			t.Errorf("parseMemoryMB(%q) = %d, want %d", tc.in, got, tc.out)
		}
	}

	// Unparseable value should return the provided default.
	if got := parseMemoryMB("invalid", 1024); got != 1024 {
		t.Errorf("expected default 1024, got %d", got)
	}
}

func TestParseCommand(t *testing.T) {
	if got := parseCommand("echo hello"); got != "echo hello" {
		t.Errorf("expected 'echo hello', got %q", got)
	}
	if got := parseCommand([]interface{}{"echo", "hello"}); got != "echo hello" {
		t.Errorf("expected 'echo hello', got %q", got)
	}
	if got := parseCommand(nil); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestParseDependsOnList(t *testing.T) {
	deps := parseDependsOn([]interface{}{"a", "b"})
	if len(deps) != 2 || deps[0] != "a" || deps[1] != "b" {
		t.Errorf("unexpected deps: %v", deps)
	}
}

func TestParseDependsOnMap(t *testing.T) {
	deps := parseDependsOn(map[string]interface{}{
		"svc1": map[string]interface{}{"condition": "service_started"},
	})
	if len(deps) != 1 || deps[0] != "svc1" {
		t.Errorf("unexpected deps: %v", deps)
	}
}

func TestParseVolume(t *testing.T) {
	vm := parseVolume("data:/var/data:ro")
	if vm.Name != "data" || vm.MountPath != "/var/data" || !vm.ReadOnly {
		t.Errorf("unexpected volume: %+v", vm)
	}

	vm2 := parseVolume("./src:/app")
	if vm2.Name != "./src" || vm2.MountPath != "/app" || vm2.ReadOnly {
		t.Errorf("unexpected volume: %+v", vm2)
	}
}

func TestToStackWithPoolAndTags(t *testing.T) {
	path := writeTmpCompose(t, sampleCompose)
	cf, err := LoadComposeFile(path)
	if err != nil {
		t.Fatal(err)
	}

	opts := DefaultConvertOptions()
	opts.DefaultPool = "web-pool"
	opts.DefaultTags = []string{"staging"}
	stack, err := cf.ToStack("myapp", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, pod := range stack.Pods {
		if pod.Spec.Pool != "web-pool" {
			t.Errorf("pod %s: expected pool 'web-pool', got %s", pod.Metadata.Name, pod.Spec.Pool)
		}
		foundStaging := false
		foundName := false
		for _, tag := range pod.Spec.Tags {
			if tag == "staging" {
				foundStaging = true
			}
			if tag == pod.Metadata.Name {
				foundName = true
			}
		}
		if !foundStaging {
			t.Errorf("pod %s: expected 'staging' tag", pod.Metadata.Name)
		}
		if !foundName {
			t.Errorf("pod %s: expected service name as tag", pod.Metadata.Name)
		}
	}
}
