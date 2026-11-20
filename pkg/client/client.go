package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}
type Sandbox struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func (c Client) request(ctx context.Context, method, path string, input []byte) (Sandbox, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+"/v1/sandboxes"+path, bytes.NewReader(input))
	if err != nil {
		return Sandbox{}, err
	}
	if len(input) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	transport := c.HTTP
	if transport == nil {
		transport = http.DefaultClient
	}
	resp, err := transport.Do(req)
	if err != nil {
		return Sandbox{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return Sandbox{}, fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	var record Sandbox
	if resp.StatusCode != 204 {
		err = json.NewDecoder(resp.Body).Decode(&record)
	}
	return record, err
}
func (c Client) Create(ctx context.Context) (Sandbox, error) {
	return c.request(ctx, "POST", "", []byte(`{"template":"linux"}`))
}
func (c Client) Get(ctx context.Context, id string) (Sandbox, error) {
	return c.request(ctx, "GET", "/"+url.PathEscape(id), nil)
}
func (c Client) Close(ctx context.Context, id string) error {
	_, err := c.request(ctx, "DELETE", "/"+url.PathEscape(id), nil)
	return err
}
func (c Client) WaitReady(ctx context.Context, id string) (Sandbox, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		r, err := c.Get(ctx, id)
		if err != nil {
			return Sandbox{}, err
		}
		switch r.Status {
		case "ready":
			return r, nil
		case "pending":
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
