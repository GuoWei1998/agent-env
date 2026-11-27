package agent_test

import (
	"agent-env/internal/agent"
	"agent-env/internal/api"
	"agent-env/internal/provider/memory"
	"agent-env/internal/route"
	"agent-env/internal/sandbox"
	"agent-env/pkg/client"
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type backed struct {
	*memory.Provider
	target route.Target
}

func (b backed) ResolveEndpoint(context.Context, string) (route.Target, error) { return b.target, nil }
func TestJourney(t *testing.T) {
	workspace := t.TempDir()
	root, e := os.OpenRoot(workspace)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	pub, key, _ := ed25519.GenerateKey(nil)
	worker := httptest.NewServer(agent.New(root, pub, "sandbox", "instance"))
	defer worker.Close()
	target := route.Target{URL: worker.URL, Instance: "instance"}
	mgr := httptest.NewServer(api.WithRoutes(sandbox.Service{Provider: backed{memory.New(), target}}, key))
	defer mgr.Close()
	c := client.Client{BaseURL: mgr.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	require := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	require(c.Upload(ctx, "sandbox", "dir/input", strings.NewReader("report\x00body")))
	id, e := c.Exec(ctx, "sandbox", client.ExecRequest{Argv: []string{"sh", "-c", "cp input output && echo done && exit 7"}, Cwd: "dir"})
	require(e)
	var stdout string
	var code int
	require(c.Events(ctx, "sandbox", id, 0, func(v client.Event) error {
		if v.Kind == "stdout" {
			stdout += string(v.Data)
		}
		code = v.Exit
		return nil
	}))
	if stdout != "done\n" || code != 7 {
		t.Fatal(stdout, code)
	}
	var data bytes.Buffer
	require(c.Download(ctx, "sandbox", "dir/output", &data))
	if data.String() != "report\x00body" {
		t.Fatal(data.String())
	}
	if e := c.Download(ctx, "other", "dir/output", io.Discard); e == nil {
		t.Fatal("cross sandbox access")
	}
	require(os.Symlink(t.TempDir(), filepath.Join(workspace, "outside")))
	if e = c.Upload(ctx, "sandbox", "outside/oops", strings.NewReader("x")); e == nil {
		t.Fatal("escaped workspace")
	}
	for _, test := range []struct {
		name    string
		timeout int
	}{{"cancel", 20}, {"timeout", 1}} {
		id, e := c.Exec(ctx, "sandbox", client.ExecRequest{Argv: []string{"sleep", "20"}, TimeoutSeconds: test.timeout})
		require(e)
		if test.name == "cancel" {
			require(c.Cancel(ctx, "sandbox", id))
		}
		var reason string
		require(c.Events(ctx, "sandbox", id, 0, func(v client.Event) error {
			if v.Kind != "exit" {
				reason = v.Kind
			} else if v.Exit == 0 {
				t.Error("expected failure")
			}
			return nil
		}))
		if reason == "" {
			t.Error("missing termination reason")
		}
	}
	signal := errors.New("disconnected")
	id, e = c.Exec(ctx, "sandbox", client.ExecRequest{Argv: []string{"sh", "-c", "echo first; sleep 0.1; echo second"}})
	require(e)
	cursor := 0
	e = c.Events(ctx, "sandbox", id, 0, func(v client.Event) error { cursor = v.Seq; return signal })
	if !errors.Is(e, signal) {
		t.Fatal(e)
	}
	require(c.Events(ctx, "sandbox", id, cursor, func(v client.Event) error {
		if v.Seq <= cursor {
			t.Error("replayed old event")
		}
		return nil
	}))
	resp, e := http.Get(worker.URL + "/files/dir/input")
	require(e)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal(resp.StatusCode)
	}
}
