package daemon

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type httpHandlers struct {
	run    func(ctx context.Context, req RunRequest) (*RunResponse, error)
	health func() HealthResponse
}

func newRouter(h httpHandlers) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Post("/run", handleHTTPRun(h))
	r.Get("/health", handleHTTPHealth(h))
	return r
}

func handleHTTPRun(h httpHandlers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := h.run(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleHTTPHealth(h httpHandlers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, h.health())
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[daemon] writeJSON: failed to encode response: %v", err)
	}
}

func listenHTTP(sockPath string, h httpHandlers) (*http.Server, error) {
	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		log.Printf("[daemon] warning: could not remove stale socket %s: %v", sockPath, err)
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(sockPath, 0o600); err != nil {
		ln.Close()
		return nil, err
	}

	srv := &http.Server{Handler: newRouter(h)}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[daemon] HTTP server error: %v", err)
		}
	}()
	return srv, nil
}
