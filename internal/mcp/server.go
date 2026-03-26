// Package mcp implements the temenos MCP stdio server.
// It exposes a single "bash" tool that executes commands in the temenos
// sandbox by proxying requests to the temenos daemon over unix socket.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gosdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tta-lab/temenos/client"
)

// sandboxClient is the interface used by the bash tool handler to execute commands.
// It matches the methods of *client.Client used by this package.
type sandboxClient interface {
	Run(ctx context.Context, req client.RunRequest) (*client.RunResponse, error)
	RunBlock(ctx context.Context, req client.RunBlockRequest) (*client.RunBlockResponse, error)
}

const (
	// blockPrefix is the prefix used to identify multi-command block lines.
	blockPrefix = "§ "

	bashToolSchema = `{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "Single command, or a block of commands each prefixed with § on its own line"
			},
			"stop_on_error": {
				"type": "boolean",
				"description": "Stop on first non-zero exit for multi-command blocks (default: true)"
			},
			"timeout": {
				"type": "integer",
				"description": "Per-command timeout in seconds (default: 120)"
			}
		},
		"required": ["command"]
	}`
)

// bashInput is the parsed input for the bash tool.
type bashInput struct {
	Command     string `json:"command"`
	StopOnError *bool  `json:"stop_on_error,omitempty"`
	Timeout     int    `json:"timeout,omitempty"`
}

// Serve starts the temenos MCP server using stdio transport.
// It reads the working directory as the primary allowed path,
// and TEMENOS_WRITE=true to grant read-write access (default: read-only).
func Serve(version string) error {
	if !json.Valid([]byte(bashToolSchema)) {
		return fmt.Errorf("mcp: internal error: bashToolSchema is not valid JSON")
	}

	allowedPaths, err := resolveAllowedPaths()
	if err != nil {
		return fmt.Errorf("mcp: %w", err)
	}

	sandboxEnv := collectSandboxEnv()

	c, err := client.New("")
	if err != nil {
		return fmt.Errorf("mcp: cannot create temenos client: %w", err)
	}

	srv := gosdkmcp.NewServer(&gosdkmcp.Implementation{
		Name:    "temenos",
		Version: version,
	}, nil)

	srv.AddTool(&gosdkmcp.Tool{
		Name: "bash",
		Description: "Execute a command in a sandboxed environment. " +
			"For multiple commands, prefix each with § on its own line.",
		InputSchema: json.RawMessage(bashToolSchema),
	}, makeBashHandler(c, allowedPaths, sandboxEnv))

	if err := srv.Run(context.Background(), &gosdkmcp.StdioTransport{}); err != nil {
		return fmt.Errorf("mcp: server exited with error: %w", err)
	}
	return nil
}

// resolveAllowedPaths builds the sandbox allowed paths from env config:
//   - cwd: the working directory (read-write if TEMENOS_WRITE=true, else read-only)
//   - TEMENOS_PATHS: comma-separated list of additional paths (format: path or path:ro or path:rw)
//   - ~/.ttal/daemon.sock: ttal daemon socket for ttal commands inside sandbox
func resolveAllowedPaths() ([]client.AllowedPath, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot determine working directory: %w", err)
	}

	writeMode := os.Getenv("TEMENOS_WRITE") == "true"

	paths := []client.AllowedPath{
		{Path: cwd, ReadOnly: !writeMode},
	}

	// Parse TEMENOS_PATHS for additional allowed paths.
	paths = append(paths, parseTemenosPaths(os.Getenv("TEMENOS_PATHS"))...)

	// Mount ttal daemon socket for ttal commands (e.g. ttal ask) inside sandbox.
	// This is safe: daemon.sock has a scoped API, no filesystem escape.
	// Never mount temenos.sock — it accepts arbitrary allowed_paths (sandbox escape).
	ttalSocketPath, err := resolveTTalDaemonSocket()
	if err != nil {
		fmt.Fprintf(os.Stderr, "temenos mcp: skipping ttal socket mount: %v\n", err)
	} else {
		paths = append(paths, client.AllowedPath{Path: ttalSocketPath, ReadOnly: false})
	}

	// Add ancestor directories of all allowed paths as read-only entries.
	// Tools like git rev-parse --path-format=absolute stat every path component
	// up the tree, so /Users, /Users/neil, etc. must be accessible even if only
	// a leaf like /Users/neil/Code/project is explicitly allowed.
	paths = appendAncestorPaths(paths)

	return paths, nil
}

// parseTemenosPaths parses a comma-separated list of paths with optional :ro/:rw suffix.
// Format: path[:ro|:rw] — default is read-only. Example:
//
//	/home/.ttal:rw,/home/.task:rw,/home/.config/ttal:ro
//
// Path validation (absolute path check, .. filtering) is handled by the daemon's
// validatePath at the HTTP layer. This function only parses the format.
func parseTemenosPaths(raw string) []client.AllowedPath {
	if raw == "" {
		return nil
	}
	var paths []client.AllowedPath
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		readOnly := true
		if strings.HasSuffix(entry, ":rw") {
			readOnly = false
			entry = strings.TrimSuffix(entry, ":rw")
		} else if strings.HasSuffix(entry, ":ro") {
			entry = strings.TrimSuffix(entry, ":ro")
		}
		if entry != "" {
			paths = append(paths, client.AllowedPath{Path: entry, ReadOnly: readOnly})
		}
	}
	return paths
}

// appendAncestorPaths adds read-only entries for every ancestor directory of the
// existing allowed paths. This lets sandboxed processes stat parent directories
// (needed by e.g. git rev-parse --path-format=absolute) without granting read
// access to their contents. Paths already present in the list are not duplicated.
func appendAncestorPaths(paths []client.AllowedPath) []client.AllowedPath {
	existing := make(map[string]bool, len(paths))
	for _, p := range paths {
		existing[p.Path] = true
	}

	var ancestors []client.AllowedPath
	for _, p := range paths {
		dir := filepath.Dir(p.Path)
		for dir != "/" && dir != "." && dir != p.Path {
			if !existing[dir] {
				existing[dir] = true
				ancestors = append(ancestors, client.AllowedPath{Path: dir, ReadOnly: true})
			}
			dir = filepath.Dir(dir)
		}
	}
	return append(paths, ancestors...)
}

// collectSandboxEnv forwards all env vars from the MCP server process into the sandbox.
//
// This is MCP-specific: the MCP server is a long-lived intermediary that receives tool
// calls and proxies them to the daemon. It needs to forward its own process env (set by
// ttal-cli) because the daemon's Env field is opt-in — direct API callers set Env
// explicitly in their RunRequest, so they don't need automatic collection.
//
// The sandbox already constructs a clean base env (PATH, HOME, TERM) — these vars are
// appended. The MCP server's env is curated by its parent (ttal-cli), so everything
// present is intentionally set. The sandbox's security boundary is filesystem access,
// not env filtering.
//
// Security: this forwards the entire process env, which may include credential vars
// (API keys, tokens) inherited from the parent shell. The MCP server should be launched
// with a curated env (e.g. `env VAR=val ... temenos mcp`) rather than inheriting a full
// interactive shell environment. Network-capable sandboxed commands could exfiltrate env
// vars via the network, which the sandbox does not restrict.
func collectSandboxEnv() map[string]string {
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		key, val, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[key] = val
	}
	return env
}

// resolveTTalDaemonSocket returns the path to the ttal daemon socket.
// Falls back to ~/.ttal/daemon.sock if TTAL_SOCKET_PATH is not set.
// Returns an error if the path does not exist on disk (for either source).
func resolveTTalDaemonSocket() (string, error) {
	if p := os.Getenv("TTAL_SOCKET_PATH"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("TTAL_SOCKET_PATH %q does not exist: %w", p, err)
		}
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	socketPath := filepath.Join(home, ".ttal", "daemon.sock")
	if _, err := os.Stat(socketPath); err != nil {
		// Socket doesn't exist yet — soft-skip, not a startup failure.
		return "", fmt.Errorf("ttal daemon socket not found: %s", socketPath)
	}
	return socketPath, nil
}

// makeBashHandler returns the MCP ToolHandler for the bash tool.
// Single commands route to /run; multi-command blocks (§ prefix) route to /run-block.
func makeBashHandler(
	c sandboxClient, allowedPaths []client.AllowedPath, sandboxEnv map[string]string,
) gosdkmcp.ToolHandler {
	return func(ctx context.Context, req *gosdkmcp.CallToolRequest) (*gosdkmcp.CallToolResult, error) {
		if req.Params == nil {
			return nil, fmt.Errorf("bash: missing tool call parameters")
		}
		var input bashInput
		if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
			return nil, fmt.Errorf("bash: invalid arguments: %w", err)
		}
		if input.Command == "" {
			return nil, fmt.Errorf("bash: command must not be empty")
		}

		if isBlockCommand(input.Command) {
			return runBlock(ctx, c, input, allowedPaths, sandboxEnv)
		}
		return runSingle(ctx, c, input, allowedPaths, sandboxEnv)
	}
}

// isBlockCommand returns true if the command string contains any § -prefixed lines.
// Uses strings.Contains for a fast early check before iterating lines.
func isBlockCommand(command string) bool {
	if !strings.Contains(command, blockPrefix) {
		return false
	}
	for _, line := range strings.Split(command, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), blockPrefix) {
			return true
		}
	}
	return false
}

// runSingle executes a single command via the daemon /run endpoint.
func runSingle(
	ctx context.Context,
	c sandboxClient,
	input bashInput,
	allowedPaths []client.AllowedPath,
	sandboxEnv map[string]string,
) (*gosdkmcp.CallToolResult, error) {
	resp, err := c.Run(ctx, client.RunRequest{
		Command:      input.Command,
		Timeout:      input.Timeout,
		AllowedPaths: allowedPaths,
		Env:          sandboxEnv,
	})
	if err != nil {
		return nil, fmt.Errorf("bash: sandbox execution failed: %w", err)
	}

	text := formatSingleOutput(resp.Stdout, resp.Stderr, resp.ExitCode)
	return &gosdkmcp.CallToolResult{
		Content: []gosdkmcp.Content{&gosdkmcp.TextContent{Text: text}},
	}, nil
}

// runBlock executes a multi-command block via the daemon /run-block endpoint.
func runBlock(
	ctx context.Context,
	c sandboxClient,
	input bashInput,
	allowedPaths []client.AllowedPath,
	sandboxEnv map[string]string,
) (*gosdkmcp.CallToolResult, error) {
	resp, err := c.RunBlock(ctx, client.RunBlockRequest{
		Block:        input.Command,
		Prefix:       blockPrefix,
		StopOnError:  input.StopOnError,
		Timeout:      input.Timeout,
		AllowedPaths: allowedPaths,
		Env:          sandboxEnv,
	})
	if err != nil {
		return nil, fmt.Errorf("bash: sandbox block execution failed: %w", err)
	}

	text := formatBlockOutput(resp.Results)
	return &gosdkmcp.CallToolResult{
		Content: []gosdkmcp.Content{&gosdkmcp.TextContent{Text: text}},
	}, nil
}

// formatSingleOutput combines stdout and stderr into a single string and
// appends the exit code as a footer line. The exit code is informational —
// non-zero exits are not treated as tool-level errors.
func formatSingleOutput(stdout, stderr string, exitCode int) string {
	var b strings.Builder
	appendOutputFooter(&b, combineOutput(stdout, stderr), exitCode)
	return b.String()
}

// formatBlockOutput formats the results of a multi-command block execution.
// Each command is shown as a § header followed by its combined output and exit code.
func formatBlockOutput(results []client.CommandResult) string {
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "§ %s\n", r.Command)
		appendOutputFooter(&b, combineOutput(r.Stdout, r.Stderr), r.ExitCode)
	}
	return b.String()
}

// appendOutputFooter writes the combined output followed by the [exit_code: N] footer
// to b. It ensures a trailing newline before the footer if output is non-empty.
func appendOutputFooter(b *strings.Builder, output string, exitCode int) {
	if output != "" {
		b.WriteString(output)
		if !strings.HasSuffix(output, "\n") {
			b.WriteByte('\n')
		}
	}
	fmt.Fprintf(b, "[exit_code: %d]", exitCode)
}

// combineOutput concatenates stdout and stderr with a newline separator if both are non-empty.
func combineOutput(stdout, stderr string) string {
	switch {
	case stdout == "":
		return stderr
	case stderr == "":
		return stdout
	default:
		if strings.HasSuffix(stdout, "\n") {
			return stdout + stderr
		}
		return stdout + "\n" + stderr
	}
}
