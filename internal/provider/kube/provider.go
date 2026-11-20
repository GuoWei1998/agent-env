package kube

import (
	"agent-env/internal/sandbox"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const owner = "agent-env.local/managed=v1"

var validID = regexp.MustCompile("^[a-f0-9]{32}$")

type runner func(context.Context, []byte, ...string) ([]byte, error)
type Provider struct {
	namespace, image string
	run              runner
}

func New(namespace, image string) (*Provider, error) {
	if len(namespace) > 63 || !regexp.MustCompile("^[a-z0-9]([-a-z0-9]*[a-z0-9])?$").MatchString(namespace) || strings.TrimSpace(image) == "" {
		return nil, fmt.Errorf("invalid namespace or image")
	}
	return &Provider{namespace, image, func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "kubectl", args...)
		cmd.Stdin = bytes.NewReader(input)
		out, err := cmd.Output()
		if e, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("kubectl: %w: %.512s", e, e.Stderr)
		}
		return out, err
	}}, nil
}
func (p *Provider) call(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	return p.run(ctx, input, append([]string{"--namespace", p.namespace, "--request-timeout=8s"}, args...)...)
}

type pod struct {
	Metadata struct {
		Name              string
		CreationTimestamp time.Time
		DeletionTimestamp *time.Time
	}
	Status struct {
		Phase      string
		Conditions []struct{ Type, Status string }
	}
}

func (item pod) record() sandbox.Sandbox {
	state := "pending"
	for _, c := range item.Status.Conditions {
		if item.Status.Phase == "Running" && c.Type == "Ready" && c.Status == "True" {
			state = "ready"
		}
	}
	if item.Status.Phase == "Failed" || item.Status.Phase == "Succeeded" {
		state = "terminated"
	}
	if item.Metadata.DeletionTimestamp != nil {
		state = "closing"
	}
	return sandbox.Sandbox{ID: strings.TrimPrefix(item.Metadata.Name, "ae-"), Template: "linux", Status: state, CreatedAt: item.Metadata.CreationTimestamp}
}
func (p *Provider) Create(ctx context.Context, s sandbox.Sandbox) (sandbox.Sandbox, error) {
	if !validID.MatchString(s.ID) {
		return sandbox.Sandbox{}, fmt.Errorf("invalid generated id")
	}
	spec := map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": "ae-" + s.ID, "labels": map[string]string{"agent-env.local/managed": "v1"}}, "spec": map[string]any{"restartPolicy": "Never", "automountServiceAccountToken": false, "activeDeadlineSeconds": 3600, "securityContext": map[string]any{"runAsNonRoot": true, "runAsUser": 65534, "seccompProfile": map[string]string{"type": "RuntimeDefault"}}, "containers": []any{map[string]any{"name": "worker", "image": p.image, "command": []string{"sleep", "3600"}, "securityContext": map[string]any{"allowPrivilegeEscalation": false, "capabilities": map[string]any{"drop": []string{"ALL"}}}, "resources": map[string]any{"limits": map[string]string{"cpu": "1", "memory": "128Mi"}}}}}}
	body, err := json.Marshal(spec)
	if err != nil {
		return sandbox.Sandbox{}, err
	}
	out, err := p.call(ctx, body, "create", "-f", "-", "-o", "json")
	if err != nil {
		return sandbox.Sandbox{}, err
	}
	var created pod
	if err = json.Unmarshal(out, &created); err != nil {
		return sandbox.Sandbox{}, err
	}
	return created.record(), nil
}
func selectors(id string) []string {
	return []string{"-l", owner, "--field-selector", "metadata.name=ae-" + id}
}
func (p *Provider) Get(ctx context.Context, id string) (sandbox.Sandbox, error) {
	if !validID.MatchString(id) {
		return sandbox.Sandbox{}, sandbox.ErrNotFound
	}
	args := append([]string{"get", "pods", "-o", "json"}, selectors(id)...)
	out, err := p.call(ctx, nil, args...)
	if err != nil {
		return sandbox.Sandbox{}, err
	}
	var list struct{ Items []pod }
	if err = json.Unmarshal(out, &list); err != nil {
		return sandbox.Sandbox{}, err
	}
	if len(list.Items) == 0 {
		return sandbox.Sandbox{}, sandbox.ErrNotFound
	}
	return list.Items[0].record(), nil
}
func (p *Provider) Close(ctx context.Context, id string) error {
	if !validID.MatchString(id) {
		return sandbox.ErrNotFound
	}
	args := append([]string{"delete", "pods", "--wait=false", "--ignore-not-found"}, selectors(id)...)
	_, err := p.call(ctx, nil, args...)
	return err
}
