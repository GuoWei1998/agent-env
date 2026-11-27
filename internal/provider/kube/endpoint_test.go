package kube

import (
	"agent-env/internal/route"
	"agent-env/internal/sandbox"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestWorkerRoute(t *testing.T) {
	p, _ := New("agent-env", "worker@sha256:example")
	p.EnableAgent("public-key")
	id := strings.Repeat("1", 32)
	spec := p.manifest(sandbox.Sandbox{ID: id, Template: "linux"})["spec"].(map[string]any)
	worker := spec["containers"].([]any)[0].(map[string]any)
	if worker["command"] != nil || worker["readinessProbe"] == nil || len(worker["env"].([]any)) != 3 {
		t.Fatal(worker)
	}
	for _, ready := range []bool{false, true} {
		p.run = func(context.Context, []byte, ...string) ([]byte, error) {
			condition := "False"
			if ready {
				condition = "True"
			}
			return []byte(fmt.Sprintf(`{"items":[{"metadata":{"uid":"pod-uid"},"status":{"phase":"Running","podIP":"10.0.1.5","conditions":[{"type":"Ready","status":"%s"}]}}]}`, condition)), nil
		}
		target, e := p.ResolveEndpoint(context.Background(), id)
		if !ready && !errors.Is(e, route.ErrPending) {
			t.Fatal(target, e)
		}
		if ready && (e != nil || target.URL != "http://10.0.1.5:8081" || target.Instance != "pod-uid") {
			t.Fatal(target, e)
		}
	}
}
