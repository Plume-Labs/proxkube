package helm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleValues = `global:
  node: pve
  storage: local-lvm
  bridge: vmbr0
  pool: web-pool
  tags:
    - staging

pods:
  web:
    image: nginx:latest
    expose: true
    startOnBoot: true
    ports:
      - hostPort: 8080
        containerPort: 80
    networks:
      - name: frontend
        bridge: vmbr0
        ip: dhcp
    resources:
      cpu: 2
      memory: 1024
      disk: 10
    labels:
      app: nginx
    dependsOn:
      - api

  api:
    image: node:20-slim
    ports:
      - hostPort: 3000
        containerPort: 3000
    environment:
      NODE_ENV: production
    resources:
      cpu: 2
      memory: 512
      disk: 8
    dependsOn:
      - db

  db:
    image: postgres:16
    tags:
      - database
    environment:
      POSTGRES_USER: myuser
      POSTGRES_DB: mydb
    mountPoints:
      - storage: local-lvm
        size: 20
        mountPath: /var/lib/postgresql/data
        backup: true
    resources:
      cpu: 2
      memory: 1024
      disk: 8
`

func writeTmpValues(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadValues(t *testing.T) {
	path := writeTmpValues(t, sampleValues)
	vals, err := LoadValues(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vals.Pods) != 3 {
		t.Fatalf("expected 3 pods, got %d", len(vals.Pods))
	}
	if vals.Pods["web"].Image != "nginx:latest" {
		t.Errorf("unexpected web image: %s", vals.Pods["web"].Image)
	}
	if vals.Global.Pool != "web-pool" {
		t.Errorf("unexpected global pool: %s", vals.Global.Pool)
	}
}

func TestLoadValuesNotFound(t *testing.T) {
	_, err := LoadValues("/nonexistent/values.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadValuesEmpty(t *testing.T) {
	path := writeTmpValues(t, "global: {}")
	_, err := LoadValues(path)
	if err == nil {
		t.Fatal("expected error for empty pods")
	}
}

func TestParseValues(t *testing.T) {
	vals, err := ParseValues([]byte(sampleValues))
	if err != nil {
		t.Fatal(err)
	}
	if len(vals.Pods) != 3 {
		t.Fatalf("expected 3 pods, got %d", len(vals.Pods))
	}
}

func TestToStack(t *testing.T) {
	vals, err := ParseValues([]byte(sampleValues))
	if err != nil {
		t.Fatal(err)
	}

	stack, err := vals.ToStack("myrelease")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stack.Name != "myrelease" {
		t.Errorf("expected stack name myrelease, got %s", stack.Name)
	}
	if len(stack.Pods) != 3 {
		t.Fatalf("expected 3 pods, got %d", len(stack.Pods))
	}

	// Find pods by name.
	podMap := make(map[string]int)
	for i, p := range stack.Pods {
		podMap[p.Metadata.Name] = i
	}

	// Check web pod.
	webIdx, ok := podMap["myrelease-web"]
	if !ok {
		t.Fatal("myrelease-web pod not found")
	}
	web := stack.Pods[webIdx]
	if web.Spec.Image != "nginx:latest" {
		t.Errorf("expected nginx image, got %s", web.Spec.Image)
	}
	if web.Spec.Resources.CPU != 2 {
		t.Errorf("expected 2 CPUs, got %d", web.Spec.Resources.CPU)
	}
	if !web.Spec.Expose {
		t.Error("expected web to be exposed")
	}
	if web.Spec.Pool != "web-pool" {
		t.Errorf("expected pool web-pool, got %s", web.Spec.Pool)
	}

	// Check dependsOn prefixed with release name.
	if len(web.Spec.DependsOn) != 1 || web.Spec.DependsOn[0] != "myrelease-api" {
		t.Errorf("unexpected dependsOn: %v", web.Spec.DependsOn)
	}

	// Check tags include global + release.
	foundStaging := false
	foundRelease := false
	for _, tag := range web.Spec.Tags {
		if tag == "staging" {
			foundStaging = true
		}
		if tag == "helm-release=myrelease" {
			foundRelease = true
		}
	}
	if !foundStaging {
		t.Error("expected 'staging' global tag")
	}
	if !foundRelease {
		t.Error("expected 'helm-release=myrelease' tag")
	}

	// Check db pod mount points.
	dbIdx, ok := podMap["myrelease-db"]
	if !ok {
		t.Fatal("myrelease-db pod not found")
	}
	db := stack.Pods[dbIdx]
	if len(db.Spec.MountPoints) != 1 {
		t.Fatalf("expected 1 mount point, got %d", len(db.Spec.MountPoints))
	}
	if db.Spec.MountPoints[0].Storage != "local-lvm" || db.Spec.MountPoints[0].Size != 20 {
		t.Errorf("unexpected mount point: %+v", db.Spec.MountPoints[0])
	}
	// DB pod should have "database" tag.
	foundDB := false
	for _, tag := range db.Spec.Tags {
		if tag == "database" {
			foundDB = true
		}
	}
	if !foundDB {
		t.Error("expected 'database' tag on db pod")
	}
}

func TestRenderTemplate(t *testing.T) {
	vals, err := ParseValues([]byte(sampleValues))
	if err != nil {
		t.Fatal(err)
	}

	output, err := RenderTemplate("myapp", vals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(output, "myapp-web") {
		t.Error("expected myapp-web in output")
	}
	if !strings.Contains(output, "nginx:latest") {
		t.Error("expected nginx image in output")
	}
}

func TestCoalesce(t *testing.T) {
	if coalesce("", "", "c") != "c" {
		t.Error("expected c")
	}
	if coalesce("a", "b") != "a" {
		t.Error("expected a")
	}
	if coalesce() != "" {
		t.Error("expected empty")
	}
}

func TestCoalesceInt(t *testing.T) {
	if coalesceInt(0, 0, 3) != 3 {
		t.Error("expected 3")
	}
	if coalesceInt(5, 2) != 5 {
		t.Error("expected 5")
	}
	if coalesceInt() != 0 {
		t.Error("expected 0")
	}
}

func TestLoadChart(t *testing.T) {
	dir := t.TempDir()
	chartPath := filepath.Join(dir, "Chart.yaml")
	content := `apiVersion: v2
name: my-chart
version: 1.0.0
appVersion: "1.0"
`
	if err := os.WriteFile(chartPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	chart, err := LoadChart(chartPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chart.Name != "my-chart" {
		t.Errorf("expected my-chart, got %s", chart.Name)
	}
	if chart.Version != "1.0.0" {
		t.Errorf("expected 1.0.0, got %s", chart.Version)
	}
}
