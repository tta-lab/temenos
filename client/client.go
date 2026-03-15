// Package client provides a Go client for the temenos sandbox daemon.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Client talks to the temenos daemon over unix socket.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// defaultSocketPath resolves the temenos socket path, mirroring daemon.DefaultSocketPath.
// TEMENOS_SOCKET_PATH overrides the default ~/.ttal/temenos.sock.
func defaultSocketPath() (string, error) {
	if p := os.Getenv("TEMENOS_SOCKET_PATH"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("temenos: cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".ttal", "temenos.sock"), nil
}

// New creates a client connected to the temenos daemon socket.
// If socketPath is empty, the default is resolved from TEMENOS_SOCKET_PATH or ~/.ttal/temenos.sock.
// Returns an error if the socket path cannot be resolved.
func New(socketPath string) (*Client, error) {
	if socketPath == "" {
		var err error
		socketPath, err = defaultSocketPath()
		if err != nil {
			return nil, err
		}
	}
	return &Client{
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
			Timeout: 120 * time.Second,
		},
		baseURL: "http://temenos",
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

// Run sends a command to the temenos daemon for sandboxed execution.
func (c *Client) Run(ctx context.Context, req RunRequest) (*RunResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("temenos: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/run", bytes.NewReader(body))
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
		return nil, fmt.Errorf("temenos: daemon returned HTTP %d", resp.StatusCode)
	}

	var result RunResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("temenos: decode response: %w", err)
	}
	return &result, nil
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
