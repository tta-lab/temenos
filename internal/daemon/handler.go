package daemon

import (
	"context"
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

func handleRun(ctx context.Context, sbx sandbox.Sandbox, req RunRequest) (*RunResponse, error) {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
		defer cancel()
	}

	var mounts []sandbox.Mount
	for _, ap := range req.AllowedPaths {
		mounts = append(mounts, sandbox.Mount{
			Source:   ap.Path,
			Target:   ap.Path,
			ReadOnly: ap.ReadOnly,
		})
	}

	var envSlice []string
	for k, v := range req.Env {
		envSlice = append(envSlice, k+"="+v)
	}

	execCfg := &sandbox.ExecConfig{
		Env:       envSlice,
		MountDirs: mounts,
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
