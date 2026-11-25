// Package sandbox defines the sandbox model, the Provider contract every
// backend implements, and the Service the API layer talks to.
package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("sandbox not found")
	ErrTemplate = errors.New("unsupported template")
)

// Status is the lifecycle state reported to API callers.
type Status string

const (
	// StatusSimulated marks a record of the memory backend: nothing runs behind it.
	StatusSimulated Status = "simulated"
	// StatusPending means the workload exists but cannot run commands yet.
	StatusPending Status = "pending"
	// StatusReady means the workload is running and passed its readiness check.
	StatusReady Status = "ready"
	// StatusClosing means deletion was accepted and resources are being reclaimed.
	StatusClosing Status = "closing"
	// StatusTerminated means the workload exited on its own; it never becomes ready again.
	StatusTerminated Status = "terminated"
)

// TemplateLinux is the only template so far.
const TemplateLinux = "linux"

type Sandbox struct {
	ID        string    `json:"id"`
	Template  string    `json:"template"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// Provider is a backend that owns the real (or simulated) workload.
//
// Create receives a record with ID, Template and CreatedAt filled in and
// returns it with the observed Status. Get returns ErrNotFound for unknown
// IDs. Close is idempotent: closing an unknown ID succeeds.
type Provider interface {
	Create(context.Context, Sandbox) (Sandbox, error)
	Get(context.Context, string) (Sandbox, error)
	Close(context.Context, string) error
}

// Service is the single entry point for callers such as the HTTP API.
// Validation and ID generation live here so that providers stay thin.
type Service struct{ Provider Provider }

func (s Service) Create(ctx context.Context, template string) (Sandbox, error) {
	if template != TemplateLinux {
		return Sandbox{}, ErrTemplate
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Sandbox{}, err
	}
	return s.Provider.Create(ctx, Sandbox{
		ID:        hex.EncodeToString(id[:]),
		Template:  template,
		CreatedAt: time.Now().UTC(),
	})
}

func (s Service) Get(ctx context.Context, id string) (Sandbox, error) {
	return s.Provider.Get(ctx, id)
}

func (s Service) Close(ctx context.Context, id string) error {
	return s.Provider.Close(ctx, id)
}
