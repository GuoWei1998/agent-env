package docker

import (
	"agent-env/internal/route"
	"agent-env/internal/sandbox"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

type Provider struct {
	cli        *http.Client
	image, key string
}

func New(socket, image, key string) *Provider {
	return &Provider{&http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}, image, key}
}
func (p *Provider) call(ctx context.Context, method, path string, body, result any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, "http://docker"+path, &buf)
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := p.cli.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return sandbox.ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("docker %s: %s", method, resp.Status)
	}
	if result != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(result)
	}
	return nil
}

const label = "agent-env.local/sandbox"

var valid = regexp.MustCompile("^[a-f0-9]{32}$")

type container struct {
	ID, Created string
	Config      struct {
		Labels   map[string]string
		Hostname string
	}
	State struct {
		Status string
		Health *struct{ Status string }
	}
	NetworkSettings struct {
		Ports map[string][]struct{ HostIP, HostPort string }
	}
}

func (p *Provider) find(ctx context.Context, id string) (container, error) {
	var c container
	if !valid.MatchString(id) {
		return c, sandbox.ErrNotFound
	}
	e := p.call(ctx, "GET", "/containers/ae-"+id+"/json", nil, &c)
	if e == nil && c.Config.Labels[label] != id {
		e = sandbox.ErrNotFound
	}
	return c, e
}
func (p *Provider) Create(ctx context.Context, s sandbox.Sandbox) (sandbox.Sandbox, error) {
	body := map[string]any{"Image": p.image, "User": "65534:65534", "Labels": map[string]string{label: s.ID}, "Env": []string{"AE_SANDBOX=" + s.ID, "AE_PUBLIC_KEY=" + p.key}, "Healthcheck": map[string]any{"Test": []string{"CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8081/readyz"}, "Interval": int64(time.Second), "Timeout": int64(time.Second), "Retries": 3}, "ExposedPorts": map[string]any{"8081/tcp": map[string]any{}}, "HostConfig": map[string]any{"PortBindings": map[string]any{"8081/tcp": []any{map[string]string{"HostIp": "127.0.0.1", "HostPort": ""}}}, "CapDrop": []string{"ALL"}, "SecurityOpt": []string{"no-new-privileges"}, "Memory": 128 << 20, "NanoCpus": 1e9, "PidsLimit": 128}}
	var created struct{ ID string }
	e := p.call(ctx, "POST", "/containers/create?name="+url.QueryEscape("ae-"+s.ID), body, &created)
	if errors.Is(e, sandbox.ErrNotFound) {
		return sandbox.Sandbox{}, fmt.Errorf("worker image unavailable; pull or build %s on Docker host", p.image)
	}
	if e != nil {
		return sandbox.Sandbox{}, e
	}
	if created.ID == "" {
		return sandbox.Sandbox{}, fmt.Errorf("missing container ID")
	}
	if e = p.call(ctx, "POST", "/containers/"+created.ID+"/start", nil, nil); e != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanErr := p.call(cleanup, "DELETE", "/containers/"+created.ID+"?force=1", nil, nil)
		return sandbox.Sandbox{}, errors.Join(e, cleanErr)
	}
	s.Status = sandbox.StatusPending
	return s, nil
}
func (c container) status() sandbox.Status {
	switch c.State.Status {
	case "dead", "exited":
		return sandbox.StatusTerminated
	case "removing":
		return sandbox.StatusClosing
	case "running":
		if c.State.Health != nil && c.State.Health.Status == "healthy" {
			return sandbox.StatusReady
		}
	}
	return sandbox.StatusPending
}
func (p *Provider) Get(ctx context.Context, id string) (sandbox.Sandbox, error) {
	v, e := p.find(ctx, id)
	if e != nil {
		return sandbox.Sandbox{}, e
	}
	created, _ := time.Parse(time.RFC3339Nano, v.Created)
	return sandbox.Sandbox{ID: id, Template: "linux", Status: v.status(), CreatedAt: created}, nil
}
func (p *Provider) Close(ctx context.Context, id string) error {
	v, e := p.find(ctx, id)
	if e == nil {
		e = p.call(ctx, "DELETE", "/containers/"+v.ID+"?force=1", nil, nil)
	}
	if errors.Is(e, sandbox.ErrNotFound) {
		return nil
	}
	return e
}
func (p *Provider) ResolveEndpoint(ctx context.Context, id string) (route.Target, error) {
	v, e := p.find(ctx, id)
	if e != nil {
		return route.Target{}, e
	}
	for _, port := range v.NetworkSettings.Ports["8081/tcp"] {
		if v.status() == sandbox.StatusReady && port.HostIP == "127.0.0.1" && port.HostPort != "" && v.Config.Hostname != "" {
			return route.Target{URL: "http://" + net.JoinHostPort(port.HostIP, port.HostPort), Instance: v.Config.Hostname}, nil
		}
	}
	return route.Target{}, route.ErrPending
}
