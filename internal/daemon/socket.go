package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/tta-lab/temenos/internal/config"
	"github.com/tta-lab/temenos/internal/session"
)

const (
	serverReadTimeout  = 30 * time.Second
	serverWriteTimeout = 120 * time.Second
	networkUnix        = "unix"
)

type httpHandlers struct {
	cfg    *config.Config
	run    func(ctx context.Context, req RunRequest) (*RunResponse, error)
	health func() HealthResponse
	store  *session.Store
}

func newRouter(h httpHandlers) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Post("/run", handleHTTPRunValidating(h))

	r.Get("/health", handleHTTPHealth(h))
	if h.store != nil {
		r.Post("/session/register", handleHTTPSessionRegister(h.store))
		r.Delete("/session/{token}", handleHTTPSessionDelete(h.store))
		r.Get("/session/list", handleHTTPSessionList(h.store))
	}
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

// listenHTTP starts an HTTP server on a unix socket or TCP address.
// addr format:
//   - Unix socket: starts with "/" or "." (absolute/relative path)
//   - TCP: anything else, e.g. ":8081", "0.0.0.0:8081"
//
// Security note: unix sockets are protected with 0o600 filesystem permissions.
// TCP mode has no authentication — access control is delegated to the network layer
// (e.g. Kubernetes NetworkPolicy). Do not expose the TCP port outside a trusted network.
//
// Errors from Serve are forwarded to Run() via the returned server's closeErr channel.
func listenHTTP(addr string, h httpHandlers) (*http.Server, <-chan error, error) {
	network, listenAddr := parseListenAddr(addr)

	if network == networkUnix {
		if err := os.Remove(listenAddr); err != nil && !os.IsNotExist(err) {
			log.Printf("[daemon] warning: could not remove stale socket %s: %v", listenAddr, err)
		}
	}

	ln, err := net.Listen(network, listenAddr)
	if err != nil {
		return nil, nil, err
	}

	if network == networkUnix {
		if err := os.Chmod(listenAddr, 0o600); err != nil {
			_ = ln.Close()
			return nil, nil, err
		}
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

// listenTCP starts an HTTP server on a TCP address.
// Unlike listenHTTP, this function does not handle unix sockets and
// does not apply special permissions (e.g. chmod 0o600).
//
// Security: Always bind to localhost (127.0.0.1) or a loopback interface.
// Do not bind to 0.0.0.0 without network-level access control.
func listenTCP(addr string, handler http.Handler) (*http.Server, <-chan error, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	srv := &http.Server{
		Handler:      handler,
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

// parseListenAddr determines network type from address format.
// Paths (starting with / or .) → unix socket. Everything else → TCP.
func parseListenAddr(addr string) (network, listenAddr string) {
	if strings.HasPrefix(addr, "/") || strings.HasPrefix(addr, ".") {
		return networkUnix, addr
	}
	return "tcp", addr
}
