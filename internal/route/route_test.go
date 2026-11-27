package route

import (
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"
)

func TestIdentityAndExpiry(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(nil)
	token := Issue(key, "sandbox", Target{Instance: "first"}).Token
	if !Valid(pub, token, "sandbox", "first") || Valid(pub, token, "sandbox", "next") || Valid(pub, token+"x", "sandbox", "first") {
		t.Fatal("route verification failed")
	}
	b, _ := json.Marshal(claims{"sandbox", "first", time.Now().Unix() - 1})
	expired := encoding.EncodeToString(b) + "." + encoding.EncodeToString(ed25519.Sign(key, b))
	if Valid(pub, expired, "sandbox", "first") {
		t.Fatal("accepted expired token")
	}
}
