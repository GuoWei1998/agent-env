// Command manager serves the sandbox management API.
package main

import (
	"agent-env/internal/api"
	"agent-env/internal/provider/kube"
	"agent-env/internal/provider/memory"
	"agent-env/internal/sandbox"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:8080", "manager listen address")
	backend := flag.String("provider", "memory", "memory or kube")
	namespace := flag.String("namespace", "agent-env", "dedicated Kubernetes namespace")
	image := flag.String("image", "busybox:1.36.1", "Linux worker image with sleep on PATH")
	flag.Parse()

	provider, err := newProvider(*backend, *namespace, *image)
	if err != nil {
		log.Fatal(err)
	}
	server := http.Server{
		Addr:              *addr,
		Handler:           api.New(sandbox.Service{Provider: provider}),
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

func newProvider(backend, namespace, image string) (sandbox.Provider, error) {
	switch backend {
	case "memory":
		return memory.New(), nil
	case "kube":
		return kube.New(namespace, image)
	default:
		return nil, fmt.Errorf("unknown provider %q: use memory or kube", backend)
	}
}
