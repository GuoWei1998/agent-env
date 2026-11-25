// Package kube runs each sandbox as one hardened Pod in a dedicated
// namespace, talking to the cluster through kubectl.
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

const (
	podPrefix     = "ae-"
	managedLabel  = "agent-env.local/managed"
	managedValue  = "v1"
	templateLabel = "agent-env.local/template"
	// owner selects only Pods created by this provider.
	owner = managedLabel + "=" + managedValue
	// lifetimeSeconds caps how long a sandbox may run.
	lifetimeSeconds = 3600
)

var (
	validID        = regexp.MustCompile("^[a-f0-9]{32}$")
	validNamespace = regexp.MustCompile("^[a-z0-9]([-a-z0-9]*[a-z0-9])?$")
)

// runner executes kubectl with the given stdin and arguments. Tests replace it.
type runner func(ctx context.Context, stdin []byte, args ...string) ([]byte, error)

type Provider struct {
	namespace, image string
	run              runner
}

func New(namespace, image string) (*Provider, error) {
	if len(namespace) > 63 || !validNamespace.MatchString(namespace) {
		return nil, fmt.Errorf("invalid namespace %q", namespace)
	}
	if strings.TrimSpace(image) == "" {
		return nil, fmt.Errorf("image must not be empty")
	}
	return &Provider{namespace: namespace, image: image, run: kubectl}, nil
}

func kubectl(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.Output()
	if e, ok := err.(*exec.ExitError); ok {
		return nil, fmt.Errorf("kubectl: %w: %.512s", e, e.Stderr)
	}
	return out, err
}

func (p *Provider) call(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	return p.run(ctx, stdin, append([]string{"--namespace", p.namespace, "--request-timeout=8s"}, args...)...)
}

// pod is the subset of the Pod object this provider reads.
type pod struct {
	Metadata struct {
		Name              string
		Labels            map[string]string
		CreationTimestamp time.Time
		DeletionTimestamp *time.Time
	}
	Status struct {
		Phase      string
		Conditions []struct{ Type, Status string }
	}
}

// record maps the observed Pod to the sandbox seen by API callers.
func (item pod) record() sandbox.Sandbox {
	template := item.Metadata.Labels[templateLabel]
	if template == "" {
		template = sandbox.TemplateLinux
	}
	return sandbox.Sandbox{
		ID:        strings.TrimPrefix(item.Metadata.Name, podPrefix),
		Template:  template,
		Status:    item.status(),
		CreatedAt: item.Metadata.CreationTimestamp,
	}
}

func (item pod) status() sandbox.Status {
	if item.Metadata.DeletionTimestamp != nil {
		return sandbox.StatusClosing
	}
	switch item.Status.Phase {
	case "Failed", "Succeeded":
		return sandbox.StatusTerminated
	case "Running":
		for _, c := range item.Status.Conditions {
			if c.Type == "Ready" && c.Status == "True" {
				return sandbox.StatusReady
			}
		}
	}
	return sandbox.StatusPending
}

// manifest builds the Pod for a sandbox. The security settings are the
// isolation baseline: non-root, no capabilities, no privilege escalation,
// default seccomp profile, no service account token, bounded CPU, memory
// and lifetime.
func (p *Provider) manifest(s sandbox.Sandbox) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":   podPrefix + s.ID,
			"labels": map[string]string{managedLabel: managedValue, templateLabel: s.Template},
		},
		"spec": map[string]any{
			"restartPolicy":                "Never",
			"automountServiceAccountToken": false,
			"activeDeadlineSeconds":        lifetimeSeconds,
			"securityContext": map[string]any{
				"runAsNonRoot":   true,
				"runAsUser":      65534,
				"seccompProfile": map[string]string{"type": "RuntimeDefault"},
			},
			"containers": []any{map[string]any{
				"name":    "worker",
				"image":   p.image,
				"command": []string{"sleep", fmt.Sprint(lifetimeSeconds)},
				"securityContext": map[string]any{
					"allowPrivilegeEscalation": false,
					"capabilities":             map[string]any{"drop": []string{"ALL"}},
				},
				"resources": map[string]any{
					"limits": map[string]string{"cpu": "1", "memory": "128Mi"},
				},
			}},
		},
	}
}

func (p *Provider) Create(ctx context.Context, s sandbox.Sandbox) (sandbox.Sandbox, error) {
	if !validID.MatchString(s.ID) {
		return sandbox.Sandbox{}, fmt.Errorf("invalid generated id")
	}
	body, err := json.Marshal(p.manifest(s))
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

// selectors restricts a command to the one Pod that both carries the
// managed label and has the name derived from id.
func selectors(id string) []string {
	return []string{"-l", owner, "--field-selector", "metadata.name=" + podPrefix + id}
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

// Close asks Kubernetes to delete the Pod and returns without waiting.
// An ID that cannot name a sandbox is treated like one that is already gone.
func (p *Provider) Close(ctx context.Context, id string) error {
	if !validID.MatchString(id) {
		return nil
	}
	args := append([]string{"delete", "pods", "--wait=false", "--ignore-not-found"}, selectors(id)...)
	_, err := p.call(ctx, nil, args...)
	return err
}
