// Package operator provides the Kubernetes operator reconciler for ProxKubePod
// custom resources. It translates K8s CRD events into Proxmox LXC container
// operations via the proxkube controller.
//
// The reconciler watches ProxKubePod resources and drives the container lifecycle
// (create, start, stop, delete) on a Proxmox VE cluster, bridging Kubernetes
// declarative management with Proxmox infrastructure.
package operator

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/GothShoot/proxkube/pkg/api"
	"github.com/GothShoot/proxkube/pkg/controller"
)

// Event represents a Kubernetes watch event for a ProxKubePod resource.
type Event struct {
	Type   EventType
	Object *api.Pod
}

// EventType defines the type of Kubernetes event.
type EventType string

const (
	EventAdded    EventType = "ADDED"
	EventModified EventType = "MODIFIED"
	EventDeleted  EventType = "DELETED"
)

// ReconcileResult holds the outcome of a reconciliation cycle.
type ReconcileResult struct {
	// Requeue indicates whether the resource should be re-queued for
	// another reconciliation cycle.
	Requeue bool
	// RequeueAfter specifies the delay before re-queuing.
	RequeueAfter time.Duration
	// Error is set when reconciliation failed.
	Error error
}

// Reconciler handles ProxKubePod custom resource events and drives the
// Proxmox LXC lifecycle accordingly.
type Reconciler struct {
	ctrl *controller.PodController
}

// NewReconciler creates a reconciler backed by the given PodController.
func NewReconciler(ctrl *controller.PodController) *Reconciler {
	return &Reconciler{ctrl: ctrl}
}

// Reconcile processes a single ProxKubePod event. It is the core loop that
// a Kubernetes operator controller would call for each event.
func (r *Reconciler) Reconcile(event Event) ReconcileResult {
	switch event.Type {
	case EventAdded, EventModified:
		return r.reconcileApply(event.Object)
	case EventDeleted:
		return r.reconcileDelete(event.Object)
	default:
		return ReconcileResult{Error: fmt.Errorf("unknown event type: %s", event.Type)}
	}
}

func (r *Reconciler) reconcileApply(pod *api.Pod) ReconcileResult {
	result, err := r.ctrl.Apply(pod)
	if err != nil {
		return ReconcileResult{
			Requeue:      true,
			RequeueAfter: 30 * time.Second,
			Error:        fmt.Errorf("reconcile apply: %w", err),
		}
	}

	// Update the pod status from the apply result.
	pod.Status = result.Status
	return ReconcileResult{}
}

func (r *Reconciler) reconcileDelete(pod *api.Pod) ReconcileResult {
	if err := r.ctrl.Delete(pod); err != nil {
		// If not found, treat as already deleted — no requeue needed.
		if strings.Contains(err.Error(), "not found") {
			return ReconcileResult{}
		}
		return ReconcileResult{
			Requeue:      true,
			RequeueAfter: 15 * time.Second,
			Error:        fmt.Errorf("reconcile delete: %w", err),
		}
	}
	return ReconcileResult{}
}

// CRDManifest returns the CRD YAML for the ProxKubePod custom resource.
// This can be applied to a Kubernetes cluster with kubectl apply.
func CRDManifest() string {
	return crdYAML
}

// ProxKubePodFromJSON parses a ProxKubePod CR JSON (as received from the
// Kubernetes API) into a proxkube Pod. This handles the mapping from the
// CRD structure (apiVersion: proxkube.io/v1, kind: ProxKubePod) to the
// internal Pod model.
func ProxKubePodFromJSON(data []byte) (*api.Pod, error) {
	var raw struct {
		APIVersion string          `json:"apiVersion"`
		Kind       string          `json:"kind"`
		Metadata   json.RawMessage `json:"metadata"`
		Spec       json.RawMessage `json:"spec"`
		Status     json.RawMessage `json:"status,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse ProxKubePod: %w", err)
	}

	pod := &api.Pod{
		APIVersion: "proxkube/v1",
		Kind:       "Pod",
	}

	if err := json.Unmarshal(raw.Metadata, &pod.Metadata); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}
	if err := json.Unmarshal(raw.Spec, &pod.Spec); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	if raw.Status != nil && len(raw.Status) > 0 {
		_ = json.Unmarshal(raw.Status, &pod.Status)
	}

	return pod, nil
}

// PodToStatusJSON generates the JSON patch for updating the ProxKubePod
// status subresource in Kubernetes.
func PodToStatusJSON(pod *api.Pod) ([]byte, error) {
	status := map[string]interface{}{
		"status": map[string]interface{}{
			"phase": string(pod.Status.Phase),
			"vmid":  pod.Status.VMID,
			"node":  pod.Status.Node,
			"ip":    pod.Status.IP,
			"tags":  pod.Status.Tags,
			"pool":  pod.Status.Pool,
		},
	}
	return json.Marshal(status)
}

// crdYAML is the embedded CRD manifest — kept in sync with
// deploy/crds/proxkubepod-crd.yaml.
const crdYAML = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: proxkubepods.proxkube.io
spec:
  group: proxkube.io
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              required: [node, resources]
              properties:
                node: { type: string }
                image: { type: string }
                osTemplate: { type: string }
                resources:
                  type: object
                  required: [cpu, memory, disk, storage]
                  properties:
                    cpu: { type: integer }
                    memory: { type: integer }
                    disk: { type: integer }
                    storage: { type: string }
            status:
              type: object
              properties:
                phase: { type: string }
                vmid: { type: integer }
                node: { type: string }
                ip: { type: string }
      subresources:
        status: {}
  scope: Namespaced
  names:
    plural: proxkubepods
    singular: proxkubepod
    kind: ProxKubePod
    shortNames: [pkp]
`
