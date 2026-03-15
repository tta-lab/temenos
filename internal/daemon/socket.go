package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const (
	serverReadTimeout  = 30 * time.Second
	serverWriteTimeout = 120 * time.Second
)

type httpHandlers struct {
	run    func(ctx context.Context, req RunRequest) (*RunResponse, error)
	health func() HealthResponse
}

func newRouter(h httpHandlers) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Post("/run", handleHTTPRunValidating(h))
	r.Get("/health", handleHTTPHealth(h))
	return r
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

// listenHTTP starts an HTTP server on a unix socket.
// Errors from Serve are forwarded to Run() via the returned server's closeErr channel.
func listenHTTP(sockPath string, h httpHandlers) (*http.Server, <-chan error, error) {
	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		log.Printf("[daemon] warning: could not remove stale socket %s: %v", sockPath, err)
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, nil, err
	}
	if err := os.Chmod(sockPath, 0o600); err != nil {
		_ = ln.Close()
		return nil, nil, err
	}

	srv := &http.Server{
		Handler:      newRouter(h),
		ReadTimeout:  serverReadTimeout,
		WriteTimeout: serverWriteTimeout,
	}

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()
	return srv, serveErr, nil
}
