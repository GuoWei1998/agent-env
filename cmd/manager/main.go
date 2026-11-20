package main

import (
	"agent-env/internal/api"
	"agent-env/internal/provider/kube"
	"agent-env/internal/provider/memory"
	"agent-env/internal/sandbox"
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:8080", "manager listen address")
	backend := flag.String("provider", "memory", "memory or kube")
	namespace := flag.String("namespace", "agent-env", "dedicated Kubernetes namespace")
	image := flag.String("image", "busybox:1.36.1", "Linux worker image with sleep on PATH")
	flag.Parse()
	var provider sandbox.Provider = memory.New()
	switch *backend {
	case "kube":
		p, err := kube.New(*namespace, *image)
		if err != nil {
			log.Fatal(err)
		}
		provider = p
	case "memory":
	default:
		log.Fatal("unknown provider")
	}
	server := http.Server{Addr: *addr, Handler: api.New(sandbox.Service{Provider: provider}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	log.Printf("manager listening on %s (%s)", *addr, *backend)
	log.Fatal(server.ListenAndServe())
}
