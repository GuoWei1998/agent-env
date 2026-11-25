// Package client is the Go client of the agent-env manager API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Status values returned by the manager.
const (
	StatusSimulated  = "simulated"
	StatusPending    = "pending"
	StatusReady      = "ready"
	StatusClosing    = "closing"
	StatusTerminated = "terminated"
)

// ErrNotFound matches (via errors.Is) any error for a sandbox that does not exist.
var ErrNotFound = errors.New("sandbox not found")

// APIError is a non-2xx answer from the manager.
type APIError struct {
	StatusCode int
	Code       string // machine-readable code from the response body, e.g. "not_found"
}

func (e *APIError) Error() string {
	return fmt.Sprintf("manager returned %d %s", e.StatusCode, e.Code)
}

func (e *APIError) Is(target error) bool {
	return target == ErrNotFound && e.StatusCode == http.StatusNotFound
}

type Client struct {
	BaseURL string
	HTTP    *http.Client // nil means http.DefaultClient
}

type Sandbox struct {
	ID        string    `json:"id"`
	Template  string    `json:"template"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (c Client) request(ctx context.Context, method, path string, input []byte) (Sandbox, error) {
	target := strings.TrimRight(c.BaseURL, "/") + "/v1/sandboxes" + path
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(input))
	if err != nil {
		return Sandbox{}, err
	}
	if len(input) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Sandbox{}, err
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, 1<<20)
	if resp.StatusCode/100 != 2 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(body).Decode(&failure)
		return Sandbox{}, &APIError{StatusCode: resp.StatusCode, Code: failure.Error}
	}
	var record Sandbox
	if resp.StatusCode != http.StatusNoContent {
		err = json.NewDecoder(body).Decode(&record)
	}
	return record, err
}

// Create asks for a new sandbox. The result is usually still pending;
// call WaitReady before using it.
func (c Client) Create(ctx context.Context) (Sandbox, error) {
	return c.request(ctx, "POST", "", []byte(`{"template":"linux"}`))
}

func (c Client) Get(ctx context.Context, id string) (Sandbox, error) {
	return c.request(ctx, "GET", "/"+url.PathEscape(id), nil)
}

// Close releases the sandbox. It is safe to call more than once.
func (c Client) Close(ctx context.Context, id string) error {
	_, err := c.request(ctx, "DELETE", "/"+url.PathEscape(id), nil)
	return err
}

// WaitReady polls until the sandbox is ready. It fails as soon as the
// sandbox reaches any state other than pending or ready, and otherwise
// waits until ctx ends, so pass a context with a deadline.
func (c Client) WaitReady(ctx context.Context, id string) (Sandbox, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		r, err := c.Get(ctx, id)
		if err != nil {
			return Sandbox{}, err
		}
		switch r.Status {
		case StatusReady:
			return r, nil
		case StatusPending:
		default:
			return r, fmt.Errorf("sandbox %s: %s", id, r.Status)
		}
		select {
		case <-ctx.Done():
			return Sandbox{}, ctx.Err()
		case <-ticker.C:
		}
	}
}
