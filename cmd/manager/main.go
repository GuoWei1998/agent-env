// Command manager serves the sandbox management API.
package main

import (
	"agent-env/internal/api"
	"agent-env/internal/provider/docker"
	"agent-env/internal/provider/kube"
	"agent-env/internal/provider/memory"
	"agent-env/internal/sandbox"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:8080", "manager listen address")
	backend := flag.String("provider", "memory", "memory, kube or docker")
	socket := flag.String("docker-socket", "/var/run/docker.sock", "local Engine socket")
	namespace := flag.String("namespace", "agent-env", "dedicated Kubernetes namespace")
	image := flag.String("image", "agent-env-worker:v2", "OCI worker image containing agent")
	flag.Parse()

	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Fatal(err)
	}
	if seed := os.Getenv("AE_SIGNING_SEED"); seed != "" {
		b, e := base64.RawURLEncoding.DecodeString(seed)
		if e != nil || len(b) != ed25519.SeedSize {
			log.Fatal("invalid AE_SIGNING_SEED")
		}
		key = ed25519.NewKeyFromSeed(b)
		pub = key.Public().(ed25519.PublicKey)
	}
	provider, err := newProvider(*backend, *namespace, *image, *socket, base64.RawURLEncoding.EncodeToString(pub))
	if err != nil {
		log.Fatal(err)
	}
	server := http.Server{
		Addr:              *addr,
		Handler:           api.WithRoutes(sandbox.Service{Provider: provider}, key),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	// On Ctrl-C or SIGTERM, stop accepting requests and let running ones finish.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("manager listening on %s (%s)", *addr, *backend)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func newProvider(backend, namespace, image, socket, key string) (sandbox.Provider, error) {
	switch backend {
	case "memory":
		return memory.New(), nil
	case "kube":
		p, e := kube.New(namespace, image)
		if e == nil {
			p.EnableAgent(key)
		}
		return p, e
	case "docker":
		return docker.New(socket, image, key), nil
	default:
		return nil, fmt.Errorf("unknown provider %q", backend)
	}
}
