package client

import (
	"agent-env/internal/route"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type ExecRequest struct {
	Argv           []string `json:"argv"`
	Cwd            string   `json:"cwd,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}
type Event struct {
	Seq  int    `json:"seq"`
	Kind string `json:"kind"`
	Data []byte `json:"data,omitempty"`
	Exit int    `json:"exit_code"`
}

func (c Client) send(ctx context.Context, method, target, token string, input io.Reader) (io.ReadCloser, error) {
	req, e := http.NewRequestWithContext(ctx, method, target, input)
	if e != nil {
		return nil, e
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	safe := *hc
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, e := safe.Do(req)
	if e != nil {
		return nil, e
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, &APIError{StatusCode: resp.StatusCode, Code: strings.TrimSpace(string(b))}
	}
	return resp.Body, nil
}
func (c Client) data(ctx context.Context, id, method, path string, input io.Reader) (io.ReadCloser, error) {
	b, e := c.send(ctx, "POST", strings.TrimRight(c.BaseURL, "/")+"/v1/sandboxes/"+url.PathEscape(id)+"/route", "", nil)
	if e != nil {
		return nil, e
	}
	defer b.Close()
	var descriptor route.Route
	if e = json.NewDecoder(b).Decode(&descriptor); e != nil {
		return nil, e
	}
	return c.send(ctx, method, descriptor.URL+path, descriptor.Token, input)
}
func (c Client) Exec(ctx context.Context, id string, req ExecRequest) (string, error) {
	payload, e := json.Marshal(req)
	if e != nil {
		return "", e
	}
	b, e := c.data(ctx, id, "POST", "/execs", bytes.NewReader(payload))
	if e != nil {
		return "", e
	}
	defer b.Close()
	var res struct{ ID string }
	e = json.NewDecoder(b).Decode(&res)
	return res.ID, e
}
func (c Client) Events(ctx context.Context, id, execID string, after int, visit func(Event) error) error {
	b, e := c.data(ctx, id, "GET", "/execs/"+url.PathEscape(execID)+"/events?after="+strconv.Itoa(after), nil)
	if e != nil {
		return e
	}
	defer b.Close()
	dec := json.NewDecoder(b)
	for {
		var v Event
		if e = dec.Decode(&v); e != nil {
			if errors.Is(e, io.EOF) {
				return io.ErrUnexpectedEOF
			}
			return e
		}
		if v.Kind == "cursor_expired" {
			return fmt.Errorf("output cursor expired")
		}
		if e = visit(v); e != nil {
			return e
		}
		if v.Kind == "exit" {
			return nil
		}
	}
}
func (c Client) Cancel(ctx context.Context, id, execID string) error {
	b, e := c.data(ctx, id, "DELETE", "/execs/"+url.PathEscape(execID), nil)
	if e == nil {
		b.Close()
	}
	return e
}
func filePath(path string) string {
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return "/files/" + strings.Join(parts, "/")
}
func (c Client) Upload(ctx context.Context, id, path string, input io.Reader) error {
	b, e := c.data(ctx, id, "PUT", filePath(path), input)
	if e == nil {
		b.Close()
	}
	return e
}
func (c Client) Download(ctx context.Context, id, path string, out io.Writer) error {
	b, e := c.data(ctx, id, "GET", filePath(path), nil)
	if e != nil {
		return e
	}
	defer b.Close()
	_, e = io.Copy(out, b)
	return e
}
