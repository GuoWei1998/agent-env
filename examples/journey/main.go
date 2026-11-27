// Command journey runs one complete sandbox lifecycle against any agent-env
// manager, regardless of backend: the same client code works with a manager
// started with -provider docker or -provider kube. The backend only decides
// where the worker runs and which address the route returns.
//
// Usage:
//
//	go run ./examples/journey -manager http://127.0.0.1:8080
package main

import (
	"agent-env/pkg/client"
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"
)

func main() {
	manager := flag.String("manager", "http://127.0.0.1:8080", "agent-env manager URL")
	flag.Parse()

	c := client.Client{BaseURL: *manager}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 1. Create: the manager returns 201 once the sandbox object exists.
	sandbox, err := c.Create(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("sandbox %s created, status=%s", sandbox.ID, sandbox.Status)

	// Always release the sandbox on exit. Close is idempotent.
	cleanup, stop := context.WithTimeout(context.Background(), 90*time.Second)
	defer stop()
	defer func() {
		if err := c.Close(cleanup, sandbox.ID); err != nil {
			log.Printf("cleanup: %v", err)
		}
	}()

	// 2. WaitReady polls until the worker agent passes its readiness check.
	ready, err := c.WaitReady(ctx, sandbox.ID)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("sandbox %s is %s", ready.ID, ready.Status)

	// 3. Upload a small script into the remote workspace.
	script := "#!/bin/sh\nset -eu\necho \"hello from $(uname -s)\"\nprintf 'built-by-agent-env' > artifact.txt\n"
	if err := c.Upload(ctx, ready.ID, "job/run.sh", strings.NewReader(script)); err != nil {
		log.Fatal(err)
	}
	log.Print("uploaded job/run.sh")

	// 4. Execute it and stream events. Each execution has its own ID; output
	// frames carry sequence numbers and the stream ends with an exit event.
	execID, err := c.Exec(ctx, ready.ID, client.ExecRequest{
		Argv:           []string{"sh", "run.sh"},
		Cwd:            "job",
		TimeoutSeconds: 60,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := c.Events(ctx, ready.ID, execID, 0, func(e client.Event) error {
		switch e.Kind {
		case "stdout", "stderr":
			fmt.Printf("[%s] %s", e.Kind, e.Data)
		case "exit":
			fmt.Printf("[exit] %d\n", e.Exit)
		}
		return nil
	}); err != nil {
		log.Fatal(err)
	}

	// 5. Download a file the command produced.
	var artifact bytes.Buffer
	if err := c.Download(ctx, ready.ID, "job/artifact.txt", &artifact); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("downloaded job/artifact.txt: %q\n", artifact.String())

	log.Print("journey finished; sandbox released by deferred Close")
}
