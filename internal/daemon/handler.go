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

	"github.com/tta-lab/temenos/internal/parse"
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

// RunBlockRequest is the POST /run-block body.
type RunBlockRequest struct {
	Block        string            `json:"block"`
	Prefix       string            `json:"prefix"`
	StopOnError  *bool             `json:"stop_on_error,omitempty"` // default true
	Env          map[string]string `json:"env,omitempty"`
	AllowedPaths []AllowedPath     `json:"allowed_paths,omitempty"`
	Network      *bool             `json:"network,omitempty"`
	Timeout      int               `json:"timeout,omitempty"` // per-command timeout in seconds (matches /run semantics)
}

// CommandResult is one command's execution result within a block.
type CommandResult struct {
	Command  string `json:"command"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// RunBlockResponse is the POST /run-block response.
type RunBlockResponse struct {
	Results []CommandResult `json:"results"`
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

// buildMounts converts AllowedPath slice into sandbox.Mount slice, validating each path.
func buildMounts(paths []AllowedPath) ([]sandbox.Mount, error) {
	mounts := make([]sandbox.Mount, 0, len(paths))
	for _, ap := range paths {
		if err := validatePath(ap.Path); err != nil {
			return nil, fmt.Errorf("%w: %w", errHTTPValidation, err)
		}
		mounts = append(mounts, sandbox.Mount{
			Source:   filepath.Clean(ap.Path),
			Target:   filepath.Clean(ap.Path),
			ReadOnly: ap.ReadOnly,
		})
	}
	return mounts, nil
}

// buildEnvSlice converts a map of env vars to a KEY=VALUE slice.
func buildEnvSlice(env map[string]string) []string {
	s := make([]string, 0, len(env))
	for k, v := range env {
		s = append(s, k+"="+v)
	}
	return s
}

// buildExecConfig constructs an ExecConfig from env and mounts.
// WorkingDir is set to the first mount's source path if any mounts are present.
func buildExecConfig(envSlice []string, mounts []sandbox.Mount) *sandbox.ExecConfig {
	cfg := &sandbox.ExecConfig{
		Env:       envSlice,
		MountDirs: mounts,
	}
	if len(mounts) > 0 {
		cfg.WorkingDir = mounts[0].Source
	}
	return cfg
}

func handleRun(ctx context.Context, sbx sandbox.Sandbox, req RunRequest) (*RunResponse, error) {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
		defer cancel()
	}

	mounts, err := buildMounts(req.AllowedPaths)
	if err != nil {
		return nil, err
	}

	execCfg := buildExecConfig(buildEnvSlice(req.Env), mounts)

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

func handleRunBlock(ctx context.Context, sbx sandbox.Sandbox, req RunBlockRequest) (*RunBlockResponse, error) {
	if req.Block == "" {
		return nil, fmt.Errorf("%w: block must not be empty", errHTTPValidation)
	}
	if req.Prefix == "" {
		return nil, fmt.Errorf("%w: prefix must not be empty", errHTTPValidation)
	}

	stopOnError := true
	if req.StopOnError != nil {
		stopOnError = *req.StopOnError
	}

	mounts, err := buildMounts(req.AllowedPaths)
	if err != nil {
		return nil, err
	}

	execCfg := buildExecConfig(buildEnvSlice(req.Env), mounts)
	cmds := parse.ParseBlock(req.Block, req.Prefix)
	results := make([]CommandResult, 0, len(cmds))

	for _, cmd := range cmds {
		if ctx.Err() != nil {
			break
		}

		cmdCtx := ctx
		var cancel context.CancelFunc
		if req.Timeout > 0 {
			cmdCtx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
		}

		stdout, stderr, exitCode, execErr := sbx.Exec(cmdCtx, cmd.Args, execCfg)
		if cancel != nil {
			cancel()
		}
		if execErr != nil {
			return nil, execErr
		}

		results = append(results, CommandResult{
			Command:  cmd.Args,
			Stdout:   stdout,
			Stderr:   stderr,
			ExitCode: exitCode,
		})

		if stopOnError && exitCode != 0 {
			break
		}
	}

	return &RunBlockResponse{Results: results}, nil
}

func handleHealth(version string) HealthResponse {
	return HealthResponse{
		OK:       true,
		Platform: runtime.GOOS,
		Version:  version,
	}
}

// handleHTTPValidating is a generic HTTP handler factory. It decodes a JSON
// request body (1 MiB limit), calls fn, and writes a JSON response.
// Validation errors (errHTTPValidation) → HTTP 400; other errors → HTTP 500.
func handleHTTPValidating[Req any, Resp any](fn func(context.Context, Req) (*Resp, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req Req
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := fn(r.Context(), req)
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

// handleHTTPRunValidating decodes the request, enforces a 1 MiB body limit,
// and returns HTTP 400 for validation errors, 500 for sandbox errors.
func handleHTTPRunValidating(h httpHandlers) http.HandlerFunc {
	return handleHTTPValidating(h.run)
}

// handleHTTPRunBlockValidating decodes the run-block request, enforces a 1 MiB
// body limit, and returns HTTP 400 for validation errors, 500 for sandbox errors.
func handleHTTPRunBlockValidating(h httpHandlers) http.HandlerFunc {
	return handleHTTPValidating(h.runBlock)
}
