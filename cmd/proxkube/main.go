// proxkube orchestrates Proxmox LXC containers using a Kubernetes-like pod
// abstraction. It reads YAML manifests and drives the Proxmox API to create,
// start, stop, list, and delete containers. Supports both traditional LXC
// templates and Proxmox 9 OCI images, Docker Compose files, Kubernetes
// operators (CRD), and Helm charts.
//
// Usage:
//
//	proxkube apply    -f pod.yaml        Create or update a pod
//	proxkube get      -f pod.yaml        Show pod status
//	proxkube delete   -f pod.yaml        Delete a pod
//	proxkube list     --node <node>      List all pods on a node
//	proxkube compose  up   -f compose.yaml   Deploy a stack
//	proxkube compose  down -f compose.yaml   Tear down a stack
//	proxkube compose  ps   -f compose.yaml   Show stack status
//	proxkube helm     install <release> -f values.yaml   Deploy from Helm values
//	proxkube helm     template <release> -f values.yaml  Render manifests
//	proxkube helm     uninstall <release> --node <node>  Remove a release
//	proxkube operator crd                  Print the CRD manifest
//	proxkube exec     <vmid> -- <command>  Execute a command inside a container
//	proxkube plugin   install              Install the PVE dashboard plugin
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/GothShoot/proxkube/pkg/api"
	"github.com/GothShoot/proxkube/pkg/compose"
	"github.com/GothShoot/proxkube/pkg/controller"
	helmPkg "github.com/GothShoot/proxkube/pkg/helm"
	"github.com/GothShoot/proxkube/pkg/hypervisor"
	"github.com/GothShoot/proxkube/pkg/operator"
	"github.com/GothShoot/proxkube/pkg/proxmox"
)

const usage = `proxkube - orchestrate Proxmox LXC containers as Kubernetes-like pods

Usage:
  proxkube apply  -f <file>          Create or update a pod from a YAML manifest
  proxkube get    -f <file>          Get current status of a pod
  proxkube delete -f <file>          Delete a pod
  proxkube list   --node <node>      List all pods on a node

  proxkube compose up   -f <compose.yaml>  Deploy a stack from a Compose file
  proxkube compose down -f <compose.yaml>  Tear down a stack
  proxkube compose ps   -f <compose.yaml>  Show stack pod status

  proxkube helm install   <release> -f <values.yaml>  Deploy from Helm values
  proxkube helm template  <release> -f <values.yaml>  Render pod manifests
  proxkube helm uninstall <release> --node <node>      Remove a Helm release

  proxkube operator crd              Print the ProxKubePod CRD manifest

  proxkube exec <vmid> -- <command>  Execute a command inside a container (local mode)
  proxkube plugin install            Install the PVE dashboard plugin

Environment variables:
  PROXMOX_URL        Proxmox API URL (e.g. https://proxmox:8006)
  PROXMOX_TOKEN_ID   API token ID (e.g. root@pam!mytoken)
  PROXMOX_SECRET     API token secret
  PROXMOX_USER       Username for ticket auth (alternative to token)
  PROXMOX_PASSWORD   Password for ticket auth
  PROXMOX_INSECURE   Set to "true" to skip TLS verification

  PROXMOX_NODE       Default Proxmox node (default: pve)
  PROXMOX_STORAGE    Default storage (default: local-lvm)
  PROXMOX_BRIDGE     Default network bridge (default: vmbr0)
  PROXMOX_POOL       Default resource pool for containers
  PROXMOX_TAGS       Comma-separated default tags for containers

  PROXMOX_LOCAL      Set to "true" to use low-level hypervisor communication
                     (Unix socket + pct CLI) instead of the REST API.
                     Only works when running directly on the Proxmox host.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "apply", "get", "delete":
		runPodCommand(command, os.Args[2:])
	case "list":
		runList(os.Args[2:])
	case "compose":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "error: compose requires a subcommand (up, down, ps)")
			os.Exit(1)
		}
		runCompose(os.Args[2], os.Args[3:])
	case "helm":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "error: helm requires a subcommand (install, template, uninstall)")
			os.Exit(1)
		}
		runHelm(os.Args[2], os.Args[3:])
	case "operator":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "error: operator requires a subcommand (crd)")
			os.Exit(1)
		}
		runOperator(os.Args[2], os.Args[3:])
	case "exec":
		runExec(os.Args[2:])
	case "plugin":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "error: plugin requires a subcommand (install, uninstall)")
			os.Exit(1)
		}
		runPlugin(os.Args[2])
	case "help", "--help", "-h":
		fmt.Print(usage)
	case "version", "--version":
		fmt.Println("proxkube v0.4.0")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n%s", command, usage)
		os.Exit(1)
	}
}

func runPodCommand(command string, args []string) {
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	filePath := fs.String("f", "", "Path to pod YAML manifest")
	outputJSON := fs.Bool("json", false, "Output as JSON")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *filePath == "" {
		fmt.Fprintf(os.Stderr, "error: -f <file> is required for %s\n", command)
		os.Exit(1)
	}

	pod, err := loadPod(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading manifest: %v\n", err)
		os.Exit(1)
	}

	ctrl, err := buildController()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	switch command {
	case "apply":
		result, err := ctrl.Apply(pod)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "pod/%s applied (VMID %d, phase %s)\n",
			result.Metadata.Name, result.Status.VMID, result.Status.Phase)
		printPod(result, *outputJSON)

	case "get":
		result, err := ctrl.Get(pod)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printPod(result, *outputJSON)

	case "delete":
		if err := ctrl.Delete(pod); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "pod/%s deleted\n", pod.Metadata.Name)
	}
}

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	node := fs.String("node", "", "Proxmox node name")
	outputJSON := fs.Bool("json", false, "Output as JSON")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *node == "" {
		fmt.Fprintln(os.Stderr, "error: --node <node> is required")
		os.Exit(1)
	}

	ctrl, err := buildController()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	pods, err := ctrl.List(*node)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *outputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(pods)
		return
	}

	fmt.Printf("%-20s %-8s %-10s %-16s %-30s\n", "NAME", "VMID", "STATUS", "IP", "TAGS")
	for _, p := range pods {
		ip := p.Status.IP
		if ip == "" {
			ip = "<none>"
		}
		tags := p.Status.Tags
		if tags == "" {
			tags = "<none>"
		}
		fmt.Printf("%-20s %-8d %-10s %-16s %-30s\n",
			p.Metadata.Name, p.Status.VMID, p.Status.Phase, ip, tags)
	}
}

func loadPod(path string) (*api.Pod, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pod api.Pod
	if err := yaml.Unmarshal(data, &pod); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	return &pod, nil
}

func runCompose(subcmd string, args []string) {
	fs := flag.NewFlagSet("compose "+subcmd, flag.ExitOnError)
	filePath := fs.String("f", "compose.yaml", "Path to compose.yaml")
	stackName := fs.String("name", "", "Stack name (default: directory name)")
	outputJSON := fs.Bool("json", false, "Output as JSON")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cf, err := compose.LoadComposeFile(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	name := *stackName
	if name == "" {
		dir := filepath.Dir(*filePath)
		abs, err := filepath.Abs(dir)
		if err == nil {
			name = filepath.Base(abs)
		} else {
			name = "stack"
		}
	}

	opts := composeOptions()
	stack, err := cf.ToStack(name, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	ctrl, err := buildController()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	switch subcmd {
	case "up":
		result, err := ctrl.ApplyStack(stack)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "stack/%s deployed (%d pods)\n", result.Name, len(result.Pods))
		printStack(result, *outputJSON)

	case "down":
		if err := ctrl.DeleteStack(stack); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "stack/%s removed\n", stack.Name)

	case "ps":
		pods, err := ctrl.List(opts.Node)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		// Filter to pods in this stack.
		stackPodNames := make(map[string]bool)
		for _, p := range stack.Pods {
			stackPodNames[p.Metadata.Name] = true
		}
		fmt.Printf("Stack: %s\n", stack.Name)
		fmt.Printf("%-20s %-8s %-10s %-16s %-30s\n", "NAME", "VMID", "STATUS", "IP", "TAGS")
		for _, p := range pods {
			if stackPodNames[p.Metadata.Name] {
				ip := p.Status.IP
				if ip == "" {
					ip = "<none>"
				}
				tags := p.Status.Tags
				if tags == "" {
					tags = "<none>"
				}
				fmt.Printf("%-20s %-8d %-10s %-16s %-30s\n",
					p.Metadata.Name, p.Status.VMID, p.Status.Phase, ip, tags)
			}
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown compose subcommand: %s (use up, down, or ps)\n", subcmd)
		os.Exit(1)
	}
}

func composeOptions() compose.ConvertOptions {
	opts := compose.DefaultConvertOptions()
	if node := os.Getenv("PROXMOX_NODE"); node != "" {
		opts.Node = node
	}
	if storage := os.Getenv("PROXMOX_STORAGE"); storage != "" {
		opts.Storage = storage
	}
	if bridge := os.Getenv("PROXMOX_BRIDGE"); bridge != "" {
		opts.DefaultBridge = bridge
	}
	if pool := os.Getenv("PROXMOX_POOL"); pool != "" {
		opts.DefaultPool = pool
	}
	if tags := os.Getenv("PROXMOX_TAGS"); tags != "" {
		for _, t := range strings.Split(tags, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				opts.DefaultTags = append(opts.DefaultTags, t)
			}
		}
	}
	return opts
}

func printStack(stack *api.Stack, asJSON bool) {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(stack)
		return
	}
	fmt.Printf("%-20s %-8s %-10s %-16s %-30s\n", "NAME", "VMID", "STATUS", "IP", "TAGS")
	for _, p := range stack.Pods {
		ip := p.Status.IP
		if ip == "" {
			ip = "<none>"
		}
		tags := p.Status.Tags
		if tags == "" {
			tags = "<none>"
		}
		fmt.Printf("%-20s %-8d %-10s %-16s %-30s\n",
			p.Metadata.Name, p.Status.VMID, p.Status.Phase, ip, tags)
	}
}

func buildController() (*controller.PodController, error) {
	// When PROXMOX_LOCAL=true, use the low-level hypervisor backend
	// (Unix socket + pct CLI) for direct host communication.
	if os.Getenv("PROXMOX_LOCAL") == "true" {
		hvCfg := hypervisor.Config{
			Node: os.Getenv("PROXMOX_NODE"),
		}
		hv, err := hypervisor.NewClient(hvCfg)
		if err != nil {
			return nil, fmt.Errorf("connect to hypervisor: %w", err)
		}
		return controller.NewPodController(hv), nil
	}

	cfg := proxmox.Config{
		BaseURL:            os.Getenv("PROXMOX_URL"),
		TokenID:            os.Getenv("PROXMOX_TOKEN_ID"),
		Secret:             os.Getenv("PROXMOX_SECRET"),
		Username:           os.Getenv("PROXMOX_USER"),
		Password:           os.Getenv("PROXMOX_PASSWORD"),
		InsecureSkipVerify: os.Getenv("PROXMOX_INSECURE") == "true",
	}

	client, err := proxmox.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to Proxmox: %w", err)
	}
	return controller.NewPodController(client), nil
}

func printPod(pod *api.Pod, asJSON bool) {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(pod)
		return
	}
	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(2)
	enc.Encode(pod)
}

func runHelm(subcmd string, args []string) {
	switch subcmd {
	case "install":
		runHelmInstall(args)
	case "template":
		runHelmTemplate(args)
	case "uninstall":
		runHelmUninstall(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown helm subcommand: %s (use install, template, or uninstall)\n", subcmd)
		os.Exit(1)
	}
}

func runHelmInstall(args []string) {
	fs := flag.NewFlagSet("helm install", flag.ExitOnError)
	filePath := fs.String("f", "values.yaml", "Path to values.yaml")
	outputJSON := fs.Bool("json", false, "Output as JSON")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: release name is required (proxkube helm install <release> -f values.yaml)")
		os.Exit(1)
	}
	releaseName := fs.Arg(0)

	vals, err := helmPkg.LoadValues(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	stack, err := vals.ToStack(releaseName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	ctrl, err := buildController()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	result, err := ctrl.ApplyStack(stack)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "release/%s installed (%d pods)\n", releaseName, len(result.Pods))
	printStack(result, *outputJSON)
}

func runHelmTemplate(args []string) {
	fs := flag.NewFlagSet("helm template", flag.ExitOnError)
	filePath := fs.String("f", "values.yaml", "Path to values.yaml")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: release name is required (proxkube helm template <release> -f values.yaml)")
		os.Exit(1)
	}
	releaseName := fs.Arg(0)

	vals, err := helmPkg.LoadValues(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	output, err := helmPkg.RenderTemplate(releaseName, vals)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(output)
}

func runHelmUninstall(args []string) {
	fs := flag.NewFlagSet("helm uninstall", flag.ExitOnError)
	node := fs.String("node", "", "Proxmox node name")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: release name is required (proxkube helm uninstall <release> --node <node>)")
		os.Exit(1)
	}
	releaseName := fs.Arg(0)

	if *node == "" {
		*node = os.Getenv("PROXMOX_NODE")
		if *node == "" {
			*node = "pve"
		}
	}

	ctrl, err := buildController()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// List all pods and find those tagged with this release.
	pods, err := ctrl.List(*node)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	releaseTag := "helm-release=" + releaseName
	var toDelete []api.Pod
	for _, p := range pods {
		if strings.Contains(p.Status.Tags, releaseTag) ||
			strings.HasPrefix(p.Metadata.Name, releaseName+"-") {
			toDelete = append(toDelete, p)
		}
	}

	if len(toDelete) == 0 {
		fmt.Fprintf(os.Stderr, "no pods found for release %q\n", releaseName)
		return
	}

	stack := &api.Stack{Name: releaseName, Pods: toDelete}
	if err := ctrl.DeleteStack(stack); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "release/%s uninstalled (%d pods removed)\n", releaseName, len(toDelete))
}

func runOperator(subcmd string, args []string) {
	switch subcmd {
	case "crd":
		fmt.Print(operator.CRDManifest())
	default:
		fmt.Fprintf(os.Stderr, "unknown operator subcommand: %s (use crd)\n", subcmd)
		os.Exit(1)
	}
}

func runExec(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "error: exec requires a VMID (proxkube exec <vmid> -- <command>)")
		os.Exit(1)
	}

	vmid, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid VMID %q: %v\n", args[0], err)
		os.Exit(1)
	}

	// Find the "--" separator.
	var command []string
	for i, arg := range args {
		if arg == "--" && i+1 < len(args) {
			command = args[i+1:]
			break
		}
	}

	if len(command) == 0 {
		fmt.Fprintln(os.Stderr, "error: exec requires a command after -- (proxkube exec <vmid> -- <command>)")
		os.Exit(1)
	}

	hvCfg := hypervisor.Config{
		Node: os.Getenv("PROXMOX_NODE"),
	}
	hv, err := hypervisor.NewClient(hvCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	out, err := hv.Exec(vmid, command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(out)
}

const pluginInstallScript = `#!/bin/sh
set -e

PVE_SHARE="/usr/share/pve-manager"
PLUGIN_DIR="$PVE_SHARE/proxkube"
PERL_DIR="/usr/share/perl5/PVE/API2"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_DIR="$SCRIPT_DIR/deploy/pve-plugin"

if [ ! -d "$DEPLOY_DIR" ]; then
    DEPLOY_DIR="$(dirname "$0")/../deploy/pve-plugin"
fi

echo "==> Installing ProxKube dashboard plugin"
install -d "$PLUGIN_DIR"
install -m 0644 "$DEPLOY_DIR/ProxKubePanel.js" "$PLUGIN_DIR/"
install -m 0644 "$DEPLOY_DIR/proxkube.css"     "$PLUGIN_DIR/"
install -m 0644 "$DEPLOY_DIR/ProxKube.pm"      "$PERL_DIR/ProxKube.pm"
echo "==> Restarting PVE services"
systemctl restart pvedaemon pveproxy
echo "==> ProxKube plugin installed. Reload the web interface."
`

func runPlugin(subcmd string) {
	switch subcmd {
	case "install":
		fmt.Println("To install the ProxKube PVE dashboard plugin, run the following on your Proxmox host:")
		fmt.Println()
		fmt.Println("  cd /path/to/proxkube")
		fmt.Println("  make -C deploy/pve-plugin install")
		fmt.Println()
		fmt.Println("Or manually:")
		fmt.Println("  cp deploy/pve-plugin/ProxKubePanel.js /usr/share/pve-manager/proxkube/")
		fmt.Println("  cp deploy/pve-plugin/proxkube.css      /usr/share/pve-manager/proxkube/")
		fmt.Println("  cp deploy/pve-plugin/ProxKube.pm       /usr/share/perl5/PVE/API2/")
		fmt.Println("  systemctl restart pvedaemon pveproxy")
	case "uninstall":
		fmt.Println("To uninstall the ProxKube PVE dashboard plugin:")
		fmt.Println()
		fmt.Println("  make -C deploy/pve-plugin uninstall")
	default:
		fmt.Fprintf(os.Stderr, "unknown plugin subcommand: %s (use install or uninstall)\n", subcmd)
		os.Exit(1)
	}
}
