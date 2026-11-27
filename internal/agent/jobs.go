package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type Event struct {
	Seq  int    `json:"seq"`
	Kind string `json:"kind"`
	Data []byte `json:"data,omitempty"`
	Exit int    `json:"exit_code,omitempty"`
}
type job struct {
	mu       sync.Mutex
	next     int
	buf      []Event
	done     bool
	notify   chan struct{}
	stop     context.CancelFunc
	finished time.Time
}

func (j *job) add(v Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.next++
	v.Seq = j.next
	if len(j.buf) >= 128 {
		j.buf = j.buf[1:]
	}
	j.buf = append(j.buf, v)
	if v.Kind == "exit" {
		j.done = true
		j.finished = time.Now()
	}
	close(j.notify)
	j.notify = make(chan struct{})
}

type output struct {
	j    *job
	kind string
}

func (o output) Write(b []byte) (int, error) {
	n := len(b)
	for len(b) > 0 {
		k := min(len(b), 4096)
		o.j.add(Event{Kind: o.kind, Data: append([]byte(nil), b[:k]...)})
		b = b[k:]
	}
	return n, nil
}
func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Argv    []string
		Cwd     string
		Timeout int `json:"timeout_seconds"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || dec.Decode(new(any)) != io.EOF || len(in.Argv) == 0 || in.Argv[0] == "" || in.Timeout < 0 || in.Timeout > 300 {
		http.Error(w, "invalid exec", 400)
		return
	}
	if in.Cwd == "" {
		in.Cwd = "."
	}
	if !filepath.IsLocal(in.Cwd) {
		http.Error(w, "invalid cwd", 400)
		return
	}
	dir, e := s.root.Open(in.Cwd)
	if e != nil {
		http.Error(w, "invalid cwd", 400)
		return
	}
	defer dir.Close()
	info, e := dir.Stat()
	if e != nil || !info.IsDir() {
		http.Error(w, "invalid cwd", 400)
		return
	}
	if in.Timeout == 0 {
		in.Timeout = 30
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Duration(in.Timeout)*time.Second)
	j := &job{notify: make(chan struct{}), stop: stop}
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = filepath.Join(s.root.Name(), in.Cwd)
	if runtime.GOOS == "linux" {
		cmd.Dir = "/proc/self/fd/" + strconv.FormatUint(uint64(dir.Fd()), 10)
	}
	cmd.Stdout = output{j, "stdout"}
	cmd.Stderr = output{j, "stderr"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		e := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(e, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return e
	}
	cmd.WaitDelay = time.Second
	id := randomID()
	s.mu.Lock()
	for k, old := range s.jobs {
		old.mu.Lock()
		expired := old.done && time.Since(old.finished) > time.Minute
		old.mu.Unlock()
		if expired {
			delete(s.jobs, k)
		}
	}
	if len(s.jobs) >= 32 {
		s.mu.Unlock()
		stop()
		http.Error(w, "exec table full", 429)
		return
	}
	if e = cmd.Start(); e != nil {
		s.mu.Unlock()
		stop()
		http.Error(w, "cannot start process", 422)
		return
	}
	s.jobs[id] = j
	s.mu.Unlock()
	go func() {
		err := cmd.Wait()
		code := cmd.ProcessState.ExitCode()
		if err != nil && ctx.Err() != nil {
			j.add(Event{Kind: ctx.Err().Error()})
		}
		stop()
		j.add(Event{Kind: "exit", Exit: code})
	}()
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}
func (s *Server) lookup(id string) *job { s.mu.Lock(); defer s.mu.Unlock(); return s.jobs[id] }
func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	j := s.lookup(r.PathValue("id"))
	if j == nil {
		http.NotFound(w, r)
		return
	}
	j.stop()
	w.WriteHeader(204)
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	j := s.lookup(r.PathValue("id"))
	if j == nil {
		http.NotFound(w, r)
		return
	}
	after := 0
	if raw := r.URL.Query().Get("after"); raw != "" {
		var e error
		after, e = strconv.Atoi(raw)
		if e != nil || after < 0 {
			http.Error(w, "invalid cursor", 400)
			return
		}
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	sent := false
	for {
		j.mu.Lock()
		if after > j.next || len(j.buf) > 0 && after < j.buf[0].Seq-1 {
			j.mu.Unlock()
			if sent {
				enc.Encode(Event{Kind: "cursor_expired"})
			} else {
				http.Error(w, "cursor unavailable", 410)
			}
			return
		}
		var batch []Event
		for _, v := range j.buf {
			if v.Seq > after {
				batch = append(batch, v)
			}
		}
		done, ch := j.done, j.notify
		j.mu.Unlock()
		for _, v := range batch {
			if enc.Encode(v) != nil {
				return
			}
			sent = true
			after = v.Seq
		}
		if len(batch) > 0 {
			http.NewResponseController(w).Flush()
		}
		if done {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ch:
		}
	}
}
