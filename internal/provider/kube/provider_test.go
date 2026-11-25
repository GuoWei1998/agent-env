package kube

import (
	"agent-env/internal/sandbox"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestKubeLifecycleContract(t *testing.T) {
	p, _ := New("learn", "busybox:1.36.1")
	id := strings.Repeat("a", 32)
	name := "ae-" + id
	item := `{"metadata":{"name":"` + name + `"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`
	p.run = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
		if !slices.Equal(args[:3], []string{"--namespace", "learn", "--request-timeout=8s"}) {
			t.Fatal(args)
		}
		switch args[3] {
		case "create":
			var m struct {
				Metadata struct {
					Name   string
					Labels map[string]string
				}
				Spec struct {
					AutomountServiceAccountToken *bool
					Containers                   []struct{ Image string }
				}
			}
			if json.Unmarshal(input, &m) != nil || m.Metadata.Name != name || m.Metadata.Labels["agent-env.local/managed"] != "v1" || m.Spec.AutomountServiceAccountToken == nil || *m.Spec.AutomountServiceAccountToken || m.Spec.Containers[0].Image != "busybox:1.36.1" {
				t.Fatalf("invalid manifest %s", input)
			}
			return []byte(`{"metadata":{"name":"` + name + `"}}`), nil
		case "get", "delete":
			if !slices.Equal(args[len(args)-4:], []string{"-l", owner, "--field-selector", "metadata.name=" + name}) {
				t.Fatal(args)
			}
			return []byte(`{"items":[` + item + `]}`), nil
		default:
			t.Fatal(args)
			return nil, nil
		}
	}
	ctx := context.Background()
	created, err := p.Create(ctx, sandbox.Sandbox{ID: id})
	if err != nil || created.Status != "pending" || created.ID != id {
		t.Fatal(created, err)
	}
	ready, err := p.Get(ctx, id)
	if err != nil || ready.Status != "ready" {
		t.Fatal(ready, err)
	}
	if err = p.Close(ctx, id); err != nil {
		t.Fatal(err)
	}
	p.run = func(context.Context, []byte, ...string) ([]byte, error) { return []byte(`{"items":[]}`), nil }
	if _, err = p.Get(ctx, id); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestStatusMapping(t *testing.T) {
	cases := map[string]sandbox.Status{
		`{"status":{"phase":"Pending"}}`: sandbox.StatusPending,
		`{"status":{"phase":"Running","conditions":[{"type":"Ready","status":"False"}]}}`: sandbox.StatusPending,
		`{"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`:  sandbox.StatusReady,
		`{"status":{"phase":"Failed"}}`:    sandbox.StatusTerminated,
		`{"status":{"phase":"Succeeded"}}`: sandbox.StatusTerminated,
		`{"metadata":{"deletionTimestamp":"2026-01-01T00:00:00Z"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`: sandbox.StatusClosing,
	}
	for body, want := range cases {
		var item pod
		if err := json.Unmarshal([]byte(body), &item); err != nil {
			t.Fatal(err)
		}
		if got := item.status(); got != want {
			t.Errorf("%s: got %s, want %s", body, got, want)
		}
	}
}

func TestInvalidIDNeverReachesCluster(t *testing.T) {
	p, _ := New("learn", "busybox:1.36.1")
	p.run = func(context.Context, []byte, ...string) ([]byte, error) {
		t.Fatal("kubectl called for an id that cannot name a sandbox")
		return nil, nil
	}
	ctx := context.Background()
	if _, err := p.Get(ctx, "../other"); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatal(err)
	}
	// Close stays idempotent, matching the memory provider.
	if err := p.Close(ctx, "../other"); err != nil {
		t.Fatal(err)
	}
}
