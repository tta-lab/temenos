package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tta-lab/temenos/internal/config"
	"github.com/tta-lab/temenos/internal/parse"
	"github.com/tta-lab/temenos/internal/session"
	"github.com/tta-lab/temenos/sandbox"
)

type contextKey struct{}

var sessionKey = contextKey{}

const (
	defaultTimeout = 120
)

var tokenRegex = regexp.MustCompile(`^[a-f0-9]{64}$`)

// bashInput defines the JSON schema for the bash tool input.
type bashInput struct {
	Command     string `json:"command" jsonschema:"Block of commands, each prefixed with § on its own line"`
	StopOnError *bool  `json:"stop_on_error,omitempty" jsonschema:"Stop on first non-zero exit (default: true)"`
	Timeout     int    `json:"timeout,omitempty" jsonschema:"Per-command timeout in seconds (default: 120)"`
}

// CommandResult represents the result of a single command execution.
type CommandResult struct {
	Command  string `json:"command"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// getSession retrieves the session from the request context.
func getSession(ctx context.Context) *session.Session {
	if s, ok := ctx.Value(sessionKey).(*session.Session); ok {
		return s
	}
	return nil
}

// NewMCPHandler creates an HTTP handler that wraps an MCP StreamableHTTPHandler
// with token authentication middleware.
func NewMCPHandler(cfg *config.Config, store *session.Store, sbx sandbox.Sandbox) http.Handler {
	getServer := func(req *http.Request) *mcp.Server {
		ctx := req.Context()
		sess := getSession(ctx)

		srv := mcp.NewServer(&mcp.Implementation{
			Name:    "temenos",
			Version: "1.0.0",
		}, nil)

		registerBashTool(srv, cfg, sbx, sess)

		return srv
	}

	handler := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{})

	return tokenMiddleware(handler, store)
}

// tokenMiddleware wraps the handler with session token authentication.
func tokenMiddleware(next http.Handler, store *session.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Session-Token")

		if token == "" || !tokenRegex.MatchString(token) {
			slog.Warn("missing or malformed session token", "token", truncateToken(token))
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		sess := store.Get(token)
		if sess == nil {
			slog.Warn("session not found or expired", "token", truncateToken(token))
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		ctx := context.WithValue(r.Context(), sessionKey, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// truncateToken returns the first 8 characters of the token for logging.
func truncateToken(token string) string {
	if len(token) > 8 {
		return token[:8]
	}
	return token
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// registerBashTool registers the bash tool handler with the MCP server.
func registerBashTool(srv *mcp.Server, cfg *config.Config, sbx sandbox.Sandbox, sess *session.Session) {
	bashTool := &mcp.Tool{
		Name:        "bash",
		Description: "Execute shell commands in the sandboxed environment",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Block of commands, each prefixed with § on its own line",
				},
				"stop_on_error": map[string]any{
					"type":        "boolean",
					"description": "Stop on first non-zero exit (default: true)",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Per-command timeout in seconds (default: 120)",
				},
			},
			"required": []string{"command"},
		},
	}

	mcp.AddTool(srv, bashTool, bashHandler(cfg, sbx, sess))
}

// buildExecConfig constructs the sandbox ExecConfig for a bash tool invocation.
// Baseline mounts from config are prepended; rw-session WritePaths are appended
// as writable mounts; ancestor MetadataOnly mounts are injected for stat access.
func buildExecConfig(cfg *config.Config, sess *session.Session) *sandbox.ExecConfig {
	mounts := cfg.BaselineMounts()
	if sess != nil && sess.Access == "rw" {
		for _, p := range sess.WritePaths {
			mounts = append(mounts, sandbox.Mount{Source: p, Target: p, ReadOnly: false})
		}
	}
	mounts = sandbox.AddAncestorMounts(mounts)

	workDir := os.TempDir()
	if sess != nil && sess.Access == "rw" && len(sess.WritePaths) > 0 {
		workDir = sess.WritePaths[0]
	} else if len(cfg.AllowWrite) > 0 {
		workDir = cfg.AllowWrite[0]
	}
	return &sandbox.ExecConfig{MountDirs: mounts, WorkingDir: workDir}
}

// bashHandler returns the tool handler for the bash tool.
func bashHandler(cfg *config.Config, sbx sandbox.Sandbox, sess *session.Session) mcp.ToolHandlerFor[bashInput, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, input bashInput) (*mcp.CallToolResult, any, error) {
		// Validate command has at least one line starting with '§'
		if !hasValidPrefix(input.Command, "§") {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "command must use § prefix lines"}},
			}, struct{}{}, nil
		}

		execCfg := buildExecConfig(cfg, sess)

		// Parse commands
		cmds := parse.ParseBlock(input.Command, "§")

		stopOnError := true
		if input.StopOnError != nil {
			stopOnError = *input.StopOnError
		}

		timeout := defaultTimeout
		if input.Timeout > 0 {
			timeout = input.Timeout
		}

		var results []CommandResult
		for _, cmd := range cmds {
			// Create a timeout context for each command
			cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
			stdout, stderr, exitCode, err := sbx.Exec(cmdCtx, cmd.Args, execCfg)
			cancel()

			results = append(results, CommandResult{
				Command:  cmd.Args,
				Stdout:   stdout,
				Stderr:   stderr,
				ExitCode: exitCode,
			})

			if err != nil {
				results[len(results)-1].Stderr = err.Error()
				if stopOnError {
					break
				}
			} else if stopOnError && exitCode != 0 {
				break
			}
		}

		resultJSON, err := json.Marshal(results)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "internal error: failed to serialize results"}},
			}, struct{}{}, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(resultJSON)}},
		}, struct{}{}, nil
	}
}

// hasValidPrefix checks if the block contains at least one line with the given prefix.
func hasValidPrefix(block string, prefix string) bool { //nolint:unparam
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}
