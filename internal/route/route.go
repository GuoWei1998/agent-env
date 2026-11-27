package route

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Target struct{ URL, Instance string }

var ErrPending = errors.New("agent not ready")

type Resolver interface {
	ResolveEndpoint(context.Context, string) (Target, error)
}
type Route struct {
	URL     string    `json:"base_url"`
	Token   string    `json:"token"`
	Expires time.Time `json:"expires_at"`
}
type claims struct {
	Sandbox, Instance string
	Expires           int64
}

var encoding = base64.RawURLEncoding

func Issue(key ed25519.PrivateKey, id string, target Target) Route {
	end := time.Now().Add(2 * time.Minute)
	body, _ := json.Marshal(claims{id, target.Instance, end.Unix()})
	return Route{target.URL, encoding.EncodeToString(body) + "." + encoding.EncodeToString(ed25519.Sign(key, body)), end}
}
func Valid(key ed25519.PublicKey, token, id, instance string) bool {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || len(token) > 2048 {
		return false
	}
	b, e := encoding.DecodeString(payload)
	s, f := encoding.DecodeString(sig)
	var c claims
	return e == nil && f == nil && ed25519.Verify(key, b, s) && json.Unmarshal(b, &c) == nil && c.Sandbox == id && c.Instance == instance && c.Expires > time.Now().Unix()
}
