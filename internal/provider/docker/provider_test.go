package docker

import (
	"agent-env/internal/sandbox"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEngineContract(t *testing.T) {
	id := strings.Repeat("a", 32)
	created, started, deleted := false, false, false
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/containers/create":
			var cfg struct {
				Image      string
				Env        []string
				HostConfig struct {
					PortBindings map[string][]struct{ HostIP string }
				}
			}
			if json.NewDecoder(r.Body).Decode(&cfg) != nil || cfg.Image != "img" || len(cfg.Env) != 2 || cfg.HostConfig.PortBindings["8081/tcp"][0].HostIP != "127.0.0.1" {
				t.Error("wrong worker config")
			}
			created = true
			fmt.Fprint(w, `{"Id":"engine-id"}`)
		case "/containers/engine-id/start":
			started = true
		case "/containers/ae-" + id + "/json":
			if deleted {
				http.NotFound(w, r)
				return
			}
			fmt.Fprintf(w, `{"Id":"engine-id","Config":{"Hostname":"inst","Labels":{"%s":"%s"}},"State":{"Status":"running","Health":{"Status":"healthy"}},"NetworkSettings":{"Ports":{"8081/tcp":[{"HostIp":"127.0.0.1","HostPort":"31111"}]}}}`, label, id)
		case "/containers/engine-id":
			if r.Method != "DELETE" {
				t.Error("expected DELETE")
			}
			deleted = true
		default:
			http.NotFound(w, r)
		}
	}))
	defer daemon.Close()
	p := New("", "img", "pub")
	p.cli = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", daemon.Listener.Addr().String())
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, e := p.Create(ctx, sandbox.Sandbox{ID: id}); e != nil {
		t.Fatal(e)
	}
	target, e := p.ResolveEndpoint(ctx, id)
	if e != nil || target.URL != "http://127.0.0.1:31111" || target.Instance != "inst" {
		t.Fatal(target, e)
	}
	if err := p.Close(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(ctx, id); err != nil {
		t.Fatal(err)
	}
	if !created || !started || !deleted {
		t.Fatal("incomplete lifecycle")
	}
}
