package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var ErrNotFound = errors.New("sandbox not found")
var ErrTemplate = errors.New("unsupported template")

type Sandbox struct {
	ID        string    `json:"id"`
	Template  string    `json:"template"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type Provider interface {
	Create(context.Context, Sandbox) (Sandbox, error)
	Get(context.Context, string) (Sandbox, error)
	Close(context.Context, string) error
}

type Service struct{ Provider Provider }

func (s Service) Create(ctx context.Context, template string) (Sandbox, error) {
	if template != "linux" {
		return Sandbox{}, ErrTemplate
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Sandbox{}, err
	}
	return s.Provider.Create(ctx, Sandbox{ID: hex.EncodeToString(id[:]), Template: template, CreatedAt: time.Now().UTC()})
}
