package memory

import (
	"agent-env/internal/sandbox"
	"context"
	"sync"
)

type Provider struct {
	mu      sync.RWMutex
	records map[string]sandbox.Sandbox
}

func New() *Provider { return &Provider{records: make(map[string]sandbox.Sandbox)} }
func (p *Provider) Create(ctx context.Context, record sandbox.Sandbox) (sandbox.Sandbox, error) {
	if err := ctx.Err(); err != nil {
		return sandbox.Sandbox{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	record.Status = "simulated"
	p.records[record.ID] = record
	return record, nil
}
func (p *Provider) Get(ctx context.Context, id string) (sandbox.Sandbox, error) {
	if err := ctx.Err(); err != nil {
		return sandbox.Sandbox{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	record, ok := p.records[id]
	if !ok {
		return sandbox.Sandbox{}, sandbox.ErrNotFound
	}
	return record, nil
}
func (p *Provider) Close(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.records, id)
	return nil
}
