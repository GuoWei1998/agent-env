package main

import (
	"agent-env/pkg/client"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"
)

func run(c client.Client) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s, err := c.Create(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 90*time.Second)
		defer stop()
		if e := c.Close(cleanup, s.ID); e != nil {
			err = errors.Join(err, e)
			return
		}
		for {
			_, e := c.Get(cleanup, s.ID)
			if errors.Is(e, client.ErrNotFound) {
				return
			}
			if e != nil {
				err = errors.Join(err, e)
				return
			}
			select {
			case <-cleanup.Done():
				err = errors.Join(err, cleanup.Err())
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}()
	if _, err = c.WaitReady(ctx, s.ID); err != nil {
		return err
	}
	if err = c.Upload(ctx, s.ID, "project/test.sh", strings.NewReader("set -eu\ntest \"$(printf abc | wc -c)\" -eq 3\nprintf PASS > report\n")); err != nil {
		return err
	}
	job, err := c.Exec(ctx, s.ID, client.ExecRequest{Argv: []string{"sh", "test.sh"}, Cwd: "project"})
	if err != nil {
		return err
	}
	if err = c.Events(ctx, s.ID, job, 0, func(e client.Event) error {
		if e.Kind == "exit" && e.Exit != 0 {
			return fmt.Errorf("test exited %d", e.Exit)
		}
		return nil
	}); err != nil {
		return err
	}
	var data bytes.Buffer
	if err = c.Download(ctx, s.ID, "project/report", &data); err != nil {
		return err
	}
	if data.String() != "PASS" {
		return fmt.Errorf("report mismatch")
	}
	return nil
}
func main() {
	addr := flag.String("manager", "http://127.0.0.1:8080", "manager URL")
	flag.Parse()
	if err := run(client.Client{BaseURL: *addr}); err != nil {
		log.Fatal(err)
	}
	log.Print("sandbox journey passed")
}
