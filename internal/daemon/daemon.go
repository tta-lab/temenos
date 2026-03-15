package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tta-lab/temenos/sandbox"
)

// DefaultSocketPath returns ~/.ttal/temenos.sock.
// Override via TEMENOS_SOCKET_PATH.
func DefaultSocketPath() string {
	if p := os.Getenv("TEMENOS_SOCKET_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ttal", "temenos.sock")
}

// Run starts the temenos daemon. Blocks until signal.
func Run(version string) error {
	sockPath := DefaultSocketPath()

	// Ensure parent dir exists
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		return err
	}

	sbx := sandbox.New(sandbox.Options{
		Timeout:          sandbox.Seconds(120),
		AllowUnsandboxed: false,
	})

	// Phase 2: tracker will be passed to handleRun for /ps and /kill support.
	// For now it's instantiated for the KillAll cleanup on shutdown.
	tracker := NewProcessTracker()
	defer tracker.KillAll()

	srv, err := listenHTTP(sockPath, httpHandlers{
		run: func(ctx context.Context, req RunRequest) (*RunResponse, error) {
			return handleRun(ctx, sbx, req)
		},
		health: func() HealthResponse { return handleHealth(version) },
	})
	if err != nil {
		return err
	}

	slog.Info("temenos daemon started", "socket", sockPath)

	// Wait for signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	slog.Info("temenos daemon shutting down")
	return srv.Shutdown(context.Background())
}
