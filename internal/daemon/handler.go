package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tta-lab/temenos/internal/auth"
	"github.com/tta-lab/temenos/sandbox"
)

// RunRequest is the POST /run body.
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
	// CallerID is an opaque identifier assigned by the caller (e.g. Lenos session ID).
	// Used to filter jobs by caller. Not interpreted by Temenos.
	CallerID  string `json:"caller_id,omitempty"`
	AuthToken string `json:"auth_token,omitempty"` // SA JWT for k8s auth
}

// AllowedPath specifies a filesystem mount for the sandbox.
type AllowedPath struct {
	Path         string `json:"path"`
	ReadOnly     bool   `json:"read_only"`
	MetadataOnly bool   `json:"metadata_only,omitempty"`
}

// RunResponse is the POST /run response.
type RunResponse struct {
	Stdout          string   `json:"stdout"`
	Stderr          string   `json:"stderr"`
	ExitCode        int      `json:"exit_code"`
	StrippedEnvKeys []string `json:"stripped_env_keys,omitempty"`
	// JobID is set when a command was moved to background.
	JobID string `json:"job_id,omitempty"`
	// Status is "background" when the command is still running as a job.
	Status string `json:"status,omitempty"`
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

// buildMounts prepends baseline mounts, converts AllowedPath slice into sandbox.Mount
// slice (with validation), then appends ancestor directories of all non-MetadataOnly
// mounts as MetadataOnly mounts. This lets sandboxed processes stat parent directories
// (e.g. git rev-parse --path-format=absolute walks up the tree) without granting broader
// access. Ancestors are appended AFTER explicit mounts to preserve mounts[0].Source as
// the working directory in buildExecConfig. Root (/) is excluded.
func buildMounts(baseline []sandbox.Mount, paths []AllowedPath) ([]sandbox.Mount, error) {
	// Start with baseline mounts (from config).
	mounts := make([]sandbox.Mount, len(baseline))
	copy(mounts, baseline)

	// Append mounts from the request AllowedPaths.
	for _, ap := range paths {
		if err := validatePath(ap.Path); err != nil {
			return nil, fmt.Errorf("%w: %w", errHTTPValidation, err)
		}
		mounts = append(mounts, sandbox.Mount{
			Source:       filepath.Clean(ap.Path),
			Target:       filepath.Clean(ap.Path),
			ReadOnly:     ap.ReadOnly,
			MetadataOnly: ap.MetadataOnly,
		})
	}

	return sandbox.AddAncestorMounts(mounts), nil
}

// buildExecConfig constructs a sandbox ExecConfig from environment and mounts.
// WorkingDir is derived from the first per-request path if present (normalized
// with filepath.Clean), otherwise falls back to os.TempDir(). Baseline mounts
// from config contribute to MountDirs but do not affect WorkingDir.
func buildExecConfig(envSlice []string, mounts []sandbox.Mount, requestPaths []AllowedPath) *sandbox.ExecConfig {
	cfg := &sandbox.ExecConfig{Env: envSlice, MountDirs: mounts}
	if len(requestPaths) > 0 {
		cfg.WorkingDir = filepath.Clean(requestPaths[0].Path)
	} else {
		cfg.WorkingDir = os.TempDir()
	}
	return cfg
}

// isValidEnvName returns true if s is a valid POSIX env var name:
// [a-zA-Z_][a-zA-Z0-9_]*. Leading digits and glob characters are rejected —
// validateEnv validates literal env var names, not allow_env patterns.
func isValidEnvName(s string) bool {
	if len(s) == 0 {
		return false
	}
	// First char must be a letter or underscore.
	if !isAlpha(s[0]) && s[0] != '_' {
		return false
	}
	// Subsequent chars must be alphanumeric or underscore.
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !isAlpha(c) && !isDigit(c) && c != '_' {
			return false
		}
	}
	return true
}

func isAlpha(c byte) bool { return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// validateEnv checks environment variable names (POSIX naming) and values
// (no NUL, LF, or CR). In Go, exec.Cmd.Env passes entries as separate strings
// to execve, so control characters in values aren't a security boundary break
// but are rejected as defense-in-depth mirroring the original session.ValidateEnv.
func validateEnv(env map[string]string) error {
	for key, val := range env {
		if !isValidEnvName(key) {
			return fmt.Errorf("invalid environment variable name: %s", key)
		}
		if strings.ContainsAny(val, "\x00\n\r") {
			return fmt.Errorf("env value for key %q contains NUL, LF, or CR", key)
		}
	}
	return nil
}

// envMapToSlice converts a map to KEY=VALUE slice for exec.
func envMapToSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

const (
	defaultRunTimeout = 20 * time.Minute
	jsonErrKey        = "error"
)

func handleRun(
	ctx context.Context, cfg *sandbox.Config, sbx sandbox.Sandbox,
	jobMgr *BackgroundJobManager, req RunRequest,
) (*RunResponse, error) {
	// When Kubernetes SA JWT auth is configured, validate the token before
	// executing the command.
	if cfg.Kubernetes.Enabled && cfg.Kubernetes.RequireServiceAccount != "" {
		if req.AuthToken == "" {
			return nil, &runError{status: http.StatusUnauthorized, msg: "authorization required"}
		}
		username, err := auth.ValidateToken(ctx, req.AuthToken, cfg.Kubernetes.TokenReviewURL)
		if err != nil {
			slog.Warn("temenos: auth token validation failed", "err", err)
			return nil, &runError{status: http.StatusForbidden, msg: "access denied — token validation failed"}
		}
		requiredSAs := strings.Split(cfg.Kubernetes.RequireServiceAccount, ",")
		for i := range requiredSAs {
			requiredSAs[i] = strings.TrimSpace(requiredSAs[i])
		}
		if !auth.IsRequiredSA(username, requiredSAs) {
			slog.Warn("temenos: auth caller not in required service accounts",
				"caller", username, "required", requiredSAs)
			return nil, &runError{status: http.StatusForbidden, msg: "access denied — unauthorized service account"}
		}
	}

	autoBackground := cfg.AutoBackgroundAfter
	if autoBackground == 0 {
		autoBackground = sandbox.DefaultAutoBackgroundAfter
	}

	runTimeout := defaultRunTimeout
	if req.Timeout > 0 {
		runTimeout = time.Duration(req.Timeout) * time.Second
	}
	jobCtx, cancel := context.WithTimeout(context.Background(), runTimeout)

	resp, err := handleRunAutoBackground(ctx, jobCtx, cancel, cfg, sbx, jobMgr, req, autoBackground)
	if err != nil || resp.JobID == "" {
		cancel()
	}
	return resp, err
}

// handleRunAutoBackground starts the command as a background job and polls
// for up to autoBackgroundAfter seconds. If the command finishes in time,
// it returns a normal RunResponse. Otherwise it returns a job_id with
// status "background".
func handleRunAutoBackground(
	requestCtx context.Context,
	jobCtx context.Context,
	jobDone context.CancelFunc,
	cfg *sandbox.Config,
	sbx sandbox.Sandbox,
	jobMgr *BackgroundJobManager,
	req RunRequest,
	autoBackgroundAfter int,
) (*RunResponse, error) {
	mounts, err := buildMounts(cfg.BaselineMounts(), req.AllowedPaths)
	if err != nil {
		return nil, err
	}

	if err := validateEnv(req.Env); err != nil {
		return nil, err
	}

	allowedEnv, stripped := cfg.FilterEnv(req.Env)
	if len(stripped) > 0 {
		slog.Debug("temenos: stripped disallowed env keys from RunRequest",
			"keys", stripped)
	}
	execCfg := buildExecConfig(envMapToSlice(allowedEnv), mounts, req.AllowedPaths)

	// Start process locally — not in registry yet.
	job := newBackgroundJob(jobCtx, req.CallerID, req.Command, sbx, execCfg, jobDone)

	threshold := time.Duration(autoBackgroundAfter) * time.Second
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(threshold)
	defer timeout.Stop()

	for {
		select {
		case <-ticker.C:
			if job.IsDone() {
				// Finished within threshold — never touched registry.
				info := job.snapshot(true)
				return &RunResponse{
					Stdout:          info.Stdout,
					Stderr:          info.Stderr,
					ExitCode:        info.ExitCode,
					StrippedEnvKeys: stripped,
				}, nil
			}
		case <-timeout.C:
			// Threshold exceeded — now register in the job registry.
			if err := jobMgr.Add(job); err != nil {
				job.cancel()
				return nil, err
			}
			return &RunResponse{
				JobID:  job.id,
				Status: "background",
			}, nil
		case <-requestCtx.Done():
			job.cancel()
			return nil, requestCtx.Err()
		}
	}
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
			var re *runError
			if errors.As(err, &re) {
				writeJSON(w, re.status, map[string]string{jsonErrKey: re.msg})
				return
			}
			if errors.Is(err, errHTTPValidation) {
				writeJSON(w, http.StatusBadRequest, map[string]string{jsonErrKey: err.Error()})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{jsonErrKey: err.Error()})
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

// handleHTTPJobList handles GET /jobs.
func handleHTTPJobList(jobMgr *BackgroundJobManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := normalizeJobStatus(r.URL.Query().Get("status"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{jsonErrKey: err.Error()})
			return
		}
		callerID := r.URL.Query().Get("caller_id")
		jobs := jobMgr.List(status, callerID)
		if jobs == nil {
			jobs = []JobInfo{}
		}
		writeJSON(w, http.StatusOK, jobs)
	}
}

func normalizeJobStatus(status string) (JobStatus, error) {
	switch status {
	case "", "all":
		return "", nil
	case string(JobStatusRunning), string(JobStatusCompleted), string(JobStatusKilled):
		return JobStatus(status), nil
	default:
		return "", fmt.Errorf("invalid job status %q", status)
	}
}

// handleHTTPJobGet handles GET /jobs/{id}.
func handleHTTPJobGet(jobMgr *BackgroundJobManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		job := jobMgr.Get(id)
		if job == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{jsonErrKey: "job not found"})
			return
		}
		writeJSON(w, http.StatusOK, job.snapshot(true))
	}
}

// handleHTTPJobKill handles DELETE /jobs/{id}.
func handleHTTPJobKill(jobMgr *BackgroundJobManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		job := jobMgr.Get(id)
		if job == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{jsonErrKey: "job not found"})
			return
		}
		if job.IsDone() {
			writeJSON(w, http.StatusConflict, map[string]string{jsonErrKey: "job already completed"})
			return
		}
		if !jobMgr.Kill(id) {
			writeJSON(w, http.StatusConflict, map[string]string{jsonErrKey: "job already completed"})
			return
		}
		writeJSON(w, http.StatusOK, job.snapshot(true))
	}
}

type runError struct {
	status int
	msg    string
}

func (e *runError) Error() string { return e.msg }
