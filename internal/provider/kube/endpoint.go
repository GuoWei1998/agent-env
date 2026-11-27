package kube

import (
	"agent-env/internal/route"
	"agent-env/internal/sandbox"
	"context"
	"net"
)

func (p *Provider) EnableAgent(key string) { p.publicKey = key }
func (p *Provider) ResolveEndpoint(ctx context.Context, id string) (route.Target, error) {
	v, e := p.find(ctx, id)
	if e != nil {
		return route.Target{}, e
	}
	if p.publicKey == "" || v.status() != sandbox.StatusReady || net.ParseIP(v.Status.PodIP) == nil || v.Metadata.UID == "" {
		return route.Target{}, route.ErrPending
	}
	return route.Target{URL: "http://" + net.JoinHostPort(v.Status.PodIP, "8081"), Instance: v.Metadata.UID}, nil
}
