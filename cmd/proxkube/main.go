// proxkube orchestrates Proxmox LXC containers using a Kubernetes-like pod
// abstraction. It reads YAML manifests and drives the Proxmox API to create,
// start, stop, list, and delete containers.
//
// Usage:
//
//	proxkube apply  -f pod.yaml      Create or update a pod
//	proxkube get    -f pod.yaml      Show pod status
//	proxkube delete -f pod.yaml      Delete a pod
//	proxkube list   --node <node>    List all pods on a node
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/GothShoot/proxkube/pkg/api"
	"github.com/GothShoot/proxkube/pkg/controller"
	"github.com/GothShoot/proxkube/pkg/proxmox"
)

const usage = `proxkube - orchestrate Proxmox LXC containers as Kubernetes-like pods

Usage:
  proxkube apply  -f <file>          Create or update a pod from a YAML manifest
  proxkube get    -f <file>          Get current status of a pod
  proxkube delete -f <file>          Delete a pod
  proxkube list   --node <node>      List all pods on a node

Environment variables:
  PROXMOX_URL        Proxmox API URL (e.g. https://proxmox:8006)
  PROXMOX_TOKEN_ID   API token ID (e.g. root@pam!mytoken)
  PROXMOX_SECRET     API token secret
  PROXMOX_USER       Username for ticket auth (alternative to token)
  PROXMOX_PASSWORD   Password for ticket auth
  PROXMOX_INSECURE   Set to "true" to skip TLS verification
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
	case "help", "--help", "-h":
		fmt.Print(usage)
	case "version", "--version":
		fmt.Println("proxkube v0.1.0")
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

	fmt.Printf("%-20s %-8s %-10s %-16s\n", "NAME", "VMID", "STATUS", "IP")
	for _, p := range pods {
		ip := p.Status.IP
		if ip == "" {
			ip = "<none>"
		}
		fmt.Printf("%-20s %-8d %-10s %-16s\n",
			p.Metadata.Name, p.Status.VMID, p.Status.Phase, ip)
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

func buildController() (*controller.PodController, error) {
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
