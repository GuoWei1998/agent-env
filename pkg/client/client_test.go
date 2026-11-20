package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestWaitReady(t *testing.T) {
	for _, state := range []string{"ready", "pending", "terminated"} {
		t.Run(state, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			c := Client{"http://manager", &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"` + state + `"}`))}, nil
			})}}
			result, err := c.WaitReady(ctx, "id")
			switch state {
			case "ready":
				if err != nil || result.Status != state {
					t.Fatal(result, err)
				}
			case "pending":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			default:
				if err == nil {
					t.Fatal("accepted terminal state")
				}
			}
		})
	}
}
