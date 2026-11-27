package agent

import (
	"agent-env/internal/route"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

const maxFile = 16 << 20

type Server struct {
	root *os.Root
	mu   sync.Mutex
	jobs map[string]*job
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func New(root *os.Root, key ed25519.PublicKey, id, instance string) http.Handler {
	s := &Server{root: root, jobs: make(map[string]*job)}
	m := http.NewServeMux()
	m.HandleFunc("POST /execs", s.start)
	m.HandleFunc("GET /execs/{id}/events", s.events)
	m.HandleFunc("DELETE /execs/{id}", s.cancel)
	m.HandleFunc("GET /files/{path...}", s.file)
	m.HandleFunc("PUT /files/{path...}", s.file)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/readyz" {
			w.WriteHeader(200)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !route.Valid(key, token, id, instance) {
			http.Error(w, "invalid route", 401)
			return
		}
		m.ServeHTTP(w, r)
	})
}
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("path")
	if !filepath.IsLocal(name) || name != path.Clean(name) || name == "." {
		http.Error(w, "invalid path", 400)
		return
	}
	if r.Method == "GET" {
		info, e := s.root.Stat(name)
		if e != nil || !info.Mode().IsRegular() || info.Size() > maxFile {
			http.Error(w, "file unavailable", 404)
			return
		}
		f, e := s.root.Open(name)
		if e != nil {
			http.Error(w, "open failed", 404)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		io.Copy(w, io.LimitReader(f, maxFile))
		return
	}
	dir := path.Dir(name)
	if e := s.root.MkdirAll(dir, 0700); e != nil {
		http.Error(w, "invalid directory", 400)
		return
	}
	tmp := path.Join(dir, ".upload-"+randomID())
	f, e := s.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		http.Error(w, "cannot create file", 400)
		return
	}
	defer s.root.Remove(tmp)
	n, e := io.Copy(f, io.LimitReader(r.Body, maxFile+1))
	closeErr := f.Close()
	if n > maxFile {
		http.Error(w, "too large", 413)
		return
	}
	if e != nil || closeErr != nil {
		http.Error(w, "upload incomplete", 400)
		return
	}
	if e = s.root.Rename(tmp, name); e != nil {
		http.Error(w, "rename failed", 400)
		return
	}
	w.WriteHeader(204)
}
