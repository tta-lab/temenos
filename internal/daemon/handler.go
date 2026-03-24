package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"github.com/tta-lab/temenos/sandbox"
)

// RunRequest is the POST /run body.
type RunRequest struct {
	Command      string            `json:"command"`
	Env          map[string]string `json:"env,omitempty"`
	AllowedPaths []AllowedPath     `json:"allowed_paths,omitempty"`
	// Network is reserved for Phase 2.
	// MVP always includes network access (seatbelt_network.sbpl).
	// Phase 2: when false, buildPolicy skips seatbelt_network.sbpl.
	Network *bool `json:"network,omitempty"`
	Timeout int   `json:"timeout,omitempty"` // seconds, 0 = default
}

// AllowedPath specifies a filesystem mount for the sandbox.
type AllowedPath struct {
	Path     string `json:"path"`
	ReadOnly bool   `json:"read_only"`
}

// RunResponse is the POST /run response.
type RunResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// HealthResponse is the GET /health response.
type HealthResponse struct {
	OK       bool   `json:"ok"`
	Platform string `json:"platform"`
	Version  string `json:"version"`
}

// errHTTPValidation is the sentinel for 400-worthy request errors.
var errHTTPValidation = errors.New("validation error")

// validatePath rejects paths that are not absolute.
// filepath.Clean resolves all ".." components before the IsAbs check,
// so an absolute clean path is fully safe.
func validatePath(p string) error {
	if !filepath.IsAbs(filepath.Clean(p)) {
		return fmt.Errorf("path must be absolute: %q", p)
	}
	return nil
}

func handleRun(ctx context.Context, sbx sandbox.Sandbox, req RunRequest) (*RunResponse, error) {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
		defer cancel()
	}

	mounts := make([]sandbox.Mount, 0, len(req.AllowedPaths))
	for _, ap := range req.AllowedPaths {
		if err := validatePath(ap.Path); err != nil {
			return nil, fmt.Errorf("%w: %w", errHTTPValidation, err)
		}
		mounts = append(mounts, sandbox.Mount{
			Source:   filepath.Clean(ap.Path),
			Target:   filepath.Clean(ap.Path),
			ReadOnly: ap.ReadOnly,
		})
	}

	envSlice := make([]string, 0, len(req.Env))
	for k, v := range req.Env {
		envSlice = append(envSlice, k+"="+v)
	}

	execCfg := &sandbox.ExecConfig{
		Env:       envSlice,
		MountDirs: mounts,
	}
	if len(mounts) > 0 {
		execCfg.WorkingDir = mounts[0].Source
	}

	stdout, stderr, exitCode, err := sbx.Exec(ctx, req.Command, execCfg)
	if err != nil {
		return nil, err
	}

	return &RunResponse{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
	}, nil
}

func handleHealth(version string) HealthResponse {
	return HealthResponse{
		OK:       true,
		Platform: runtime.GOOS,
		Version:  version,
	}
}

// handleHTTPRunValidating decodes the request, enforces a 1 MiB body limit,
// and returns HTTP 400 for validation errors, 500 for sandbox errors.
func handleHTTPRunValidating(h httpHandlers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB limit
		var req RunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := h.run(r.Context(), req)
		if err != nil {
			if errors.Is(err, errHTTPValidation) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
