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

	"github.com/tta-lab/temenos/internal/config"
	temenosmcp "github.com/tta-lab/temenos/internal/mcp"
	"github.com/tta-lab/temenos/internal/session"
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

	memLimitMB, err := parseMemoryLimitMB()
	if err != nil {
		return err
	}

	sbx := sandbox.New(sandbox.Options{
		Timeout:          sandbox.Seconds(120),
		AllowUnsandboxed: false,
		MemoryLimitMB:    memLimitMB,
		RequireCgroup:    parseRequireCgroup(),
	})

	// Phase 2: tracker will be passed to handleRun for /ps and /kill support.
	// For now it's instantiated for the KillAll cleanup on shutdown.
	tracker := NewProcessTracker()
	defer tracker.KillAll()

	// Set up session store with persistence.
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("temenos: cannot determine home directory: %w", err)
	}
	sessionsPath := filepath.Join(home, ".temenos", "sessions.json")
	store := session.NewStore(sessionsPath)
	if err := store.LoadFromDisk(); err != nil {
		slog.Warn("failed to load sessions from disk", "path", sessionsPath, "err", err)
	}
	store.PruneStale()

	// Load config for baseline mounts.
	cfg, err := config.Load("")
	if err != nil {
		slog.Error("failed to load config — no baseline mounts will be applied", "err", err)
		cfg = &config.Config{MCPPort: 9783}
	}

	srv, serveErr, err := listenHTTP(addr, httpHandlers{
		cfg: cfg,
		run: func(ctx context.Context, req RunRequest) (*RunResponse, error) {
			return handleRun(ctx, cfg, sbx, req)
		},
		runBlock: func(ctx context.Context, req RunBlockRequest) (*RunBlockResponse, error) {
			return handleRunBlock(ctx, cfg, sbx, req)
		},
		health: func() HealthResponse { return handleHealth(version) },
		store:  store,
	})
	if err != nil {
		return err
	}

	// Create MCP handler and start TCP listener on localhost.
	mcpHandler := temenosmcp.NewMCPHandler(cfg, store, sbx)
	mcpAddr := fmt.Sprintf("127.0.0.1:%d", cfg.MCPPort)
	mcpSrv, mcpServeErr, err := listenTCP(mcpAddr, mcpHandler)
	if err != nil {
		return err
	}

	slog.Info("temenos daemon started",
		"admin", resolvedAddr,
		"admin_network", network,
		"mcp", mcpAddr,
	)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	return waitAndShutdown(sig, serveErr, mcpServeErr, srv, mcpSrv)
}

// waitAndShutdown blocks until a signal or server error, then shuts down both servers.
func waitAndShutdown(
	sig <-chan os.Signal,
	serveErr, mcpServeErr <-chan error,
	srv, mcpSrv *http.Server,
) error {
	select {
	case <-sig:
		slog.Info("temenos daemon shutting down")
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("temenos: admin server failed: %w", err)
		}
	case err := <-mcpServeErr:
		if err != nil {
			return fmt.Errorf("temenos: MCP server failed: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Warn("admin server shutdown error", "err", err)
	}
	return mcpSrv.Shutdown(ctx)
}
