package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tta-lab/temenos/internal/auth"
	"github.com/tta-lab/temenos/sandbox"
)

const shutdownTimeout = 10 * time.Second

// DefaultSocketPath returns ~/.temenos/daemon.sock.
// Override via TEMENOS_SOCKET_PATH.
func DefaultSocketPath() (string, error) {
	if p := os.Getenv("TEMENOS_SOCKET_PATH"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("temenos: cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".temenos", "daemon.sock"), nil
}

// listenAddr resolves the daemon listen address.
// Priority:
//  1. TEMENOS_LISTEN_ADDR (e.g. ":8081" for TCP, "/path/to/sock" for unix)
//  2. TEMENOS_SOCKET_PATH (unix socket path, backward compat via DefaultSocketPath)
//  3. Default: ~/.temenos/daemon.sock
func listenAddr() (string, error) {
	if addr := os.Getenv("TEMENOS_LISTEN_ADDR"); addr != "" {
		return addr, nil
	}
	return DefaultSocketPath()
}

// Run starts the temenos daemon. Blocks until signal or server error.
// cgroupv2MemoryLimitMB is the memory limit in MB per sandbox execution.
// When > 0, the daemon attempts to set up cgroup v2 init-leaf migration and fails
// fast if setup fails (e.g. not in a k8s pod, cgroup v2 unavailable).
func Run(version string, cgroupv2MemoryLimitMB int) error {
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

	// Fail fast: if memory limit is requested, cgroup v2 setup must succeed.
	if cgroupv2MemoryLimitMB > 0 {
		if err := sandbox.SetupCgroupV2(); err != nil {
			return fmt.Errorf("temenos: cgroup v2 setup failed (is this a k8s pod with cgroup v2 delegation?): %w", err)
		}
		slog.Info("temenos: cgroup v2 memory limits enabled", "limit_mb", cgroupv2MemoryLimitMB)
	}

	// Load config first — sandbox options depend on kubernetes.enabled.
	cfg, err := sandbox.Load("")
	if err != nil {
		slog.Error("failed to load config — no baseline mounts will be applied", "err", err)
		cfg = &sandbox.Config{}
	}

	opts := sandbox.Options{
		Timeout:          sandbox.DefaultTimeout,
		AllowUnsandboxed: false,
		MemoryLimitMB:    cgroupv2MemoryLimitMB,
	}
	if cfg.Kubernetes.Enabled {
		opts.KubernetesMode = true
		slog.Info("temenos: kubernetes mode enabled")
	}
	sbx := sandbox.New(opts)

	tracker := NewProcessTracker()
	defer tracker.KillAll()

	// Initialize background job manager.
	jobMgr := NewBackgroundJobManager()

	// Wire cached token validator when k8s auth is enabled.
	var tokenValidator *auth.CachedTokenValidator
	if cfg.Kubernetes.Enabled && cfg.Kubernetes.RequireServiceAccount != "" {
		tokenValidator = auth.NewCachedTokenValidator(5 * time.Minute)
		slog.Info("temenos: token review cache enabled", "ttl", "5m")
	}

	srv, serveErr, err := listenHTTP(addr, httpHandlers{
		cfg: cfg,
		run: func(ctx context.Context, req RunRequest) (*RunResponse, error) {
			return handleRun(ctx, cfg, sbx, jobMgr, tokenValidator, req)
		},
		health: func() HealthResponse { return handleHealth(version) },
		jobMgr: jobMgr,
	})
	if err != nil {
		return err
	}

	slog.Info("temenos daemon started",
		"admin", resolvedAddr,
		"admin_network", network,
	)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	return waitAndShutdown(sig, serveErr, srv)
}

// waitAndShutdown blocks until a signal or server error, then shuts down the server.
func waitAndShutdown(
	sig <-chan os.Signal,
	serveErr <-chan error,
	srv *http.Server,
) error {
	select {
	case <-sig:
		slog.Info("temenos daemon shutting down")
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("temenos: admin server failed: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Warn("admin server shutdown error", "err", err)
	}
	return nil
}
