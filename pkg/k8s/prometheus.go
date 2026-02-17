package k8s

// PrometheusManifests returns the YAML manifests to deploy the Prometheus
// operator and a basic Prometheus instance in the proxkube namespace.
// These manifests install the operator CRDs and a scrape configuration
// that targets proxkube-exported metrics.
func PrometheusManifests(namespace string) string {
	if namespace == "" {
		namespace = "proxkube-system"
	}
	return `---
apiVersion: v1
kind: Namespace
metadata:
  name: ` + namespace + `
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: proxkube-prometheus
  namespace: ` + namespace + `
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: proxkube-prometheus
rules:
  - apiGroups: [""]
    resources: ["nodes", "services", "endpoints", "pods"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["monitoring.coreos.com"]
    resources: ["prometheuses", "servicemonitors", "podmonitors", "alertmanagers"]
    verbs: ["*"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: proxkube-prometheus
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: proxkube-prometheus
subjects:
  - kind: ServiceAccount
    name: proxkube-prometheus
    namespace: ` + namespace + `
---
apiVersion: v1
kind: Service
metadata:
  name: proxkube-monitor
  namespace: ` + namespace + `
  labels:
    app: proxkube-monitor
spec:
  ports:
    - name: metrics
      port: 9090
      targetPort: 9090
  selector:
    app: proxkube-monitor
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: proxkube-prometheus-config
  namespace: ` + namespace + `
data:
  prometheus.yml: |
    global:
      scrape_interval: 30s
      evaluation_interval: 30s
    scrape_configs:
      - job_name: proxkube-daemon
        static_configs:
          - targets: ["localhost:9100"]
        metrics_path: /metrics
`
}

// ServiceMonitorManifest returns a Prometheus ServiceMonitor resource
// that discovers and scrapes proxkube daemon metrics endpoints.
func ServiceMonitorManifest(namespace string) string {
	if namespace == "" {
		namespace = "proxkube-system"
	}
	return `apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: proxkube-monitor
  namespace: ` + namespace + `
  labels:
    app: proxkube
spec:
  selector:
    matchLabels:
      app: proxkube-monitor
  endpoints:
    - port: metrics
      interval: 30s
      path: /metrics
`
}
