package api

import (
	"testing"
)

func TestValidateSuccess(t *testing.T) {
	pod := &Pod{
		Metadata: Metadata{Name: "test-pod"},
		Spec: PodSpec{
			Node:       "pve",
			OSTemplate: "local:vztmpl/ubuntu-22.04-standard_22.04-1_amd64.tar.zst",
			Resources: Resources{
				CPU:     2,
				Memory:  512,
				Disk:    8,
				Storage: "local-lvm",
			},
		},
	}
	if err := pod.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateEmptyName(t *testing.T) {
	pod := &Pod{
		Metadata: Metadata{Name: ""},
		Spec: PodSpec{
			Node:       "pve",
			OSTemplate: "local:vztmpl/ubuntu.tar.zst",
			Resources:  Resources{CPU: 1, Memory: 256, Disk: 4, Storage: "local-lvm"},
		},
	}
	err := pod.Validate()
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "metadata.name" {
		t.Errorf("expected field metadata.name, got %s", ve.Field)
	}
}

func TestValidateEmptyNode(t *testing.T) {
	pod := &Pod{
		Metadata: Metadata{Name: "x"},
		Spec: PodSpec{
			Node:       "",
			OSTemplate: "local:vztmpl/ubuntu.tar.zst",
			Resources:  Resources{CPU: 1, Memory: 256, Disk: 4, Storage: "local-lvm"},
		},
	}
	err := pod.Validate()
	if err == nil {
		t.Fatal("expected error for empty node")
	}
	ve := err.(*ValidationError)
	if ve.Field != "spec.node" {
		t.Errorf("expected field spec.node, got %s", ve.Field)
	}
}

func TestValidateEmptyOSTemplate(t *testing.T) {
	pod := &Pod{
		Metadata: Metadata{Name: "x"},
		Spec: PodSpec{
			Node:       "pve",
			OSTemplate: "",
			Resources:  Resources{CPU: 1, Memory: 256, Disk: 4, Storage: "local-lvm"},
		},
	}
	err := pod.Validate()
	if err == nil {
		t.Fatal("expected error for empty osTemplate")
	}
	ve := err.(*ValidationError)
	if ve.Field != "spec.osTemplate" {
		t.Errorf("expected field spec.osTemplate, got %s", ve.Field)
	}
}

func TestValidateInvalidCPU(t *testing.T) {
	pod := &Pod{
		Metadata: Metadata{Name: "x"},
		Spec: PodSpec{
			Node:       "pve",
			OSTemplate: "local:vztmpl/ubuntu.tar.zst",
			Resources:  Resources{CPU: 0, Memory: 256, Disk: 4, Storage: "local-lvm"},
		},
	}
	err := pod.Validate()
	if err == nil {
		t.Fatal("expected error for zero CPU")
	}
	ve := err.(*ValidationError)
	if ve.Field != "spec.resources.cpu" {
		t.Errorf("expected field spec.resources.cpu, got %s", ve.Field)
	}
}

func TestValidateInvalidMemory(t *testing.T) {
	pod := &Pod{
		Metadata: Metadata{Name: "x"},
		Spec: PodSpec{
			Node:       "pve",
			OSTemplate: "local:vztmpl/ubuntu.tar.zst",
			Resources:  Resources{CPU: 1, Memory: 0, Disk: 4, Storage: "local-lvm"},
		},
	}
	err := pod.Validate()
	if err == nil {
		t.Fatal("expected error for zero memory")
	}
	ve := err.(*ValidationError)
	if ve.Field != "spec.resources.memory" {
		t.Errorf("expected field spec.resources.memory, got %s", ve.Field)
	}
}

func TestValidateInvalidDisk(t *testing.T) {
	pod := &Pod{
		Metadata: Metadata{Name: "x"},
		Spec: PodSpec{
			Node:       "pve",
			OSTemplate: "local:vztmpl/ubuntu.tar.zst",
			Resources:  Resources{CPU: 1, Memory: 256, Disk: 0, Storage: "local-lvm"},
		},
	}
	err := pod.Validate()
	if err == nil {
		t.Fatal("expected error for zero disk")
	}
	ve := err.(*ValidationError)
	if ve.Field != "spec.resources.disk" {
		t.Errorf("expected field spec.resources.disk, got %s", ve.Field)
	}
}

func TestValidateEmptyStorage(t *testing.T) {
	pod := &Pod{
		Metadata: Metadata{Name: "x"},
		Spec: PodSpec{
			Node:       "pve",
			OSTemplate: "local:vztmpl/ubuntu.tar.zst",
			Resources:  Resources{CPU: 1, Memory: 256, Disk: 4, Storage: ""},
		},
	}
	err := pod.Validate()
	if err == nil {
		t.Fatal("expected error for empty storage")
	}
	ve := err.(*ValidationError)
	if ve.Field != "spec.resources.storage" {
		t.Errorf("expected field spec.resources.storage, got %s", ve.Field)
	}
}

func TestPhaseConstants(t *testing.T) {
	phases := []Phase{PhasePending, PhaseRunning, PhaseStopped, PhaseFailed, PhaseUnknown}
	expected := []string{"Pending", "Running", "Stopped", "Failed", "Unknown"}
	for i, p := range phases {
		if string(p) != expected[i] {
			t.Errorf("expected %s, got %s", expected[i], p)
		}
	}
}
