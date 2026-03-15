package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tta-lab/temenos/sandbox"
)

const shutdownTimeout = 10 * time.Second

// DefaultSocketPath returns ~/.ttal/temenos.sock.
// Override via TEMENOS_SOCKET_PATH.
func DefaultSocketPath() (string, error) {
	if p := os.Getenv("TEMENOS_SOCKET_PATH"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("temenos: cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".ttal", "temenos.sock"), nil
}

// listenAddr resolves the daemon listen address.
// Priority:
//  1. TEMENOS_LISTEN_ADDR (e.g. ":8081" for TCP, "/path/to/sock" for unix)
//  2. TEMENOS_SOCKET_PATH (unix socket path, backward compat via DefaultSocketPath)
//  3. Default: ~/.ttal/temenos.sock
func listenAddr() (string, error) {
	if addr := os.Getenv("TEMENOS_LISTEN_ADDR"); addr != "" {
		return addr, nil
	}
	return DefaultSocketPath()
}

// Run starts the temenos daemon. Blocks until signal or server error.
func Run(version string) error {
	addr, err := listenAddr()
	if err != nil {
		return err
	}

	// Ensure parent dir exists (only matters for socket mode)
	network, resolvedAddr := parseListenAddr(addr)
	if network == networkUnix {
		if err := os.MkdirAll(filepath.Dir(resolvedAddr), 0o755); err != nil {
			return err
		}
	}

	sbx := sandbox.New(sandbox.Options{
		Timeout:          sandbox.Seconds(120),
		AllowUnsandboxed: false,
	})

	// Phase 2: tracker will be passed to handleRun for /ps and /kill support.
	// For now it's instantiated for the KillAll cleanup on shutdown.
	tracker := NewProcessTracker()
	defer tracker.KillAll()

	srv, serveErr, err := listenHTTP(addr, httpHandlers{
		run: func(ctx context.Context, req RunRequest) (*RunResponse, error) {
			return handleRun(ctx, sbx, req)
		},
		health: func() HealthResponse { return handleHealth(version) },
	})
	if err != nil {
		return err
	}

	slog.Info("temenos daemon started", "listen", resolvedAddr, "network", network)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sig:
		slog.Info("temenos daemon shutting down")
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("temenos: HTTP server failed: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(ctx)
}
