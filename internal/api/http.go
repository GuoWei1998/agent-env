package api

import (
	"agent-env/internal/sandbox"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

// New returns the manager HTTP API. Handlers only translate between HTTP and
// sandbox.Service; they never reach into a provider directly.
func New(service sandbox.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/sandboxes", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Template string `json:"template"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			fail(w, 400, "invalid_json")
			return
		}
		record, err := service.Create(r.Context(), input.Template)
		if err != nil {
			failure(w, err)
			return
		}
		w.Header().Set("Location", "/v1/sandboxes/"+record.ID)
		reply(w, 201, record)
	})
	mux.HandleFunc("GET /v1/sandboxes/{id}", func(w http.ResponseWriter, r *http.Request) {
		record, err := service.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			failure(w, err)
			return
		}
		reply(w, 200, record)
	})
	mux.HandleFunc("DELETE /v1/sandboxes/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := service.Close(r.Context(), r.PathValue("id")); err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(204)
	})
	return mux
}

func failure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sandbox.ErrNotFound):
		fail(w, 404, "not_found")
	case errors.Is(err, sandbox.ErrTemplate):
		fail(w, 400, "unsupported_template")
	default:
		log.Print(err)
		fail(w, 500, "internal_error")
	}
}

func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]string{"error": msg})
}

func reply(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
