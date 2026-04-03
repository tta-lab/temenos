// Package client provides a Go client for the temenos sandbox daemon.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client talks to the temenos daemon over unix socket or TCP.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// defaultSocketPath resolves the temenos socket path, mirroring daemon.DefaultSocketPath.
// TEMENOS_SOCKET_PATH overrides the default ~/.temenos/daemon.sock.
func defaultSocketPath() (string, error) {
	if p := os.Getenv("TEMENOS_SOCKET_PATH"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("temenos: cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".temenos", "daemon.sock"), nil
}

// resolveAddr resolves the daemon address from environment.
// Priority: TEMENOS_LISTEN_ADDR → TEMENOS_SOCKET_PATH → default socket.
func resolveAddr() (string, error) {
	if addr := os.Getenv("TEMENOS_LISTEN_ADDR"); addr != "" {
		return addr, nil
	}
	return defaultSocketPath()
}

// defaultHTTPTimeout is the client timeout for all transport types.
// NOTE: address-format detection (path prefix → unix, everything else → tcp) is
// intentionally duplicated in daemon/socket.go parseListenAddr. Both packages
// use the same rule; a shared internal/addrutil package would be cleaner but adds
// module complexity. Keep in sync if the rule ever changes.
const defaultHTTPTimeout = 120 * time.Second

// New creates a client connected to the temenos daemon.
// addr formats:
//   - Empty string: resolve from TEMENOS_LISTEN_ADDR → TEMENOS_SOCKET_PATH → default socket
//   - Starts with "/" or ".": unix socket path
//   - Starts with "http://": HTTP base URL (TCP)
//   - Otherwise (e.g. ":8081", "localhost:8081"): TCP, auto-prefixed with http://
//
// HTTPS is not supported — the daemon serves plain HTTP only.
func New(addr string) (*Client, error) {
	if addr == "" {
		var err error
		addr, err = resolveAddr()
		if err != nil {
			return nil, err
		}
	}

	if strings.HasPrefix(addr, "https://") {
		return nil, fmt.Errorf("temenos: HTTPS is not supported; use http:// or a bare host:port")
	}

	if strings.HasPrefix(addr, "http://") {
		return &Client{
			httpClient: &http.Client{Timeout: defaultHTTPTimeout},
			baseURL:    strings.TrimSuffix(addr, "/"),
		}, nil
	}

	if strings.HasPrefix(addr, "/") || strings.HasPrefix(addr, ".") {
		return &Client{
			httpClient: &http.Client{
				Transport: &http.Transport{
					DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
						return net.Dial("unix", addr)
					},
				},
				Timeout: defaultHTTPTimeout,
			},
			baseURL: "http://temenos",
		}, nil
	}

	// Bare host:port — treat as TCP
	return &Client{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		baseURL:    "http://" + addr,
	}, nil
}

// RunRequest is the body for POST /run.
type RunRequest struct {
	Command      string            `json:"command"`
	Env          map[string]string `json:"env,omitempty"`
	AllowedPaths []AllowedPath     `json:"allowed_paths,omitempty"`
	Network      *bool             `json:"network,omitempty"`
	Timeout      int               `json:"timeout,omitempty"` // seconds, 0 = default
}

// AllowedPath specifies a filesystem path allowed in the sandbox.
type AllowedPath struct {
	Path     string `json:"path"`
	ReadOnly bool   `json:"read_only"`
}

// RunResponse is the response from POST /run.
type RunResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// postJSON marshals req as JSON, POSTs it to path, and decodes the response into Resp.
// It handles the 1 MiB body limit, content-type header, and error wrapping uniformly.
func postJSON[Req any, Resp any](ctx context.Context, c *Client, path string, req Req) (*Resp, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("temenos: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("temenos: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("temenos: daemon unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(errBody))
		if msg != "" {
			return nil, fmt.Errorf("temenos: daemon returned HTTP %d: %s", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("temenos: daemon returned HTTP %d", resp.StatusCode)
	}

	var result Resp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("temenos: decode response: %w", err)
	}
	return &result, nil
}

// Run sends a command to the temenos daemon for sandboxed execution.
func (c *Client) Run(ctx context.Context, req RunRequest) (*RunResponse, error) {
	return postJSON[RunRequest, RunResponse](ctx, c, "/run", req)
}

// Health checks if the daemon is running and returns any error if not.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("temenos: daemon unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("temenos: health check returned HTTP %d", resp.StatusCode)
	}
	return nil
}
