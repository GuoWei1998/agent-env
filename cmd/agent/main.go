package main

import (
	"agent-env/internal/agent"
	"crypto/ed25519"
	"encoding/base64"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	key, e := base64.RawURLEncoding.DecodeString(os.Getenv("AE_PUBLIC_KEY"))
	id, instance := os.Getenv("AE_SANDBOX"), os.Getenv("AE_INSTANCE")
	if instance == "" {
		instance, _ = os.Hostname()
	}
	if e != nil || len(key) != ed25519.PublicKeySize || id == "" || instance == "" {
		log.Fatal("missing agent identity")
	}
	root, e := os.OpenRoot("/workspace")
	if e != nil {
		log.Fatal(e)
	}
	defer root.Close()
	s := http.Server{Addr: ":8081", ReadHeaderTimeout: 5 * time.Second, Handler: agent.New(root, key, id, instance)}
	log.Fatal(s.ListenAndServe())
}
