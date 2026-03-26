package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	gosdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tta-lab/temenos/client"
)

// stubClient is a test double for sandboxClient.
// If runFunc or runBlockFunc is nil and the corresponding method is called,
// it panics with a clear message rather than a nil function dereference.
type stubClient struct {
	runFunc      func(ctx context.Context, req client.RunRequest) (*client.RunResponse, error)
	runBlockFunc func(ctx context.Context, req client.RunBlockRequest) (*client.RunBlockResponse, error)
}

func (s *stubClient) Run(ctx context.Context, req client.RunRequest) (*client.RunResponse, error) {
	if s.runFunc == nil {
		panic("stubClient.Run called but runFunc is not set")
	}
	return s.runFunc(ctx, req)
}

func (s *stubClient) RunBlock(ctx context.Context, req client.RunBlockRequest) (*client.RunBlockResponse, error) {
	if s.runBlockFunc == nil {
		panic("stubClient.RunBlock called but runBlockFunc is not set")
	}
	return s.runBlockFunc(ctx, req)
}

// callTool is a test helper that invokes the bash tool handler with a JSON-encoded command.
func callTool(t *testing.T, handler gosdkmcp.ToolHandler, input bashInput) (*gosdkmcp.CallToolResult, error) {
	t.Helper()
	args, err := json.Marshal(input)
	require.NoError(t, err)
	req := &gosdkmcp.CallToolRequest{
		Params: &gosdkmcp.CallToolParamsRaw{
			Arguments: args,
		},
	}
	return handler(context.Background(), req)
}

func TestIsBlockCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"single command", "ls -la", false},
		{"empty", "", false},
		{"block with prefix", "§ ls\n§ pwd", true},
		{"block with mixed lines", "# comment\n§ ls -la\nsome text", true},
		{"prefix with leading whitespace", "  § ls -la", true},
		{"section symbol without space", "§ls", false},
		{"partial prefix", "§", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isBlockCommand(tt.command))
		})
	}
}

func TestFormatSingleOutput_StdoutOnly(t *testing.T) {
	result := formatSingleOutput("hello\n", "", 0)
	assert.Equal(t, "hello\n[exit_code: 0]", result)
}

func TestFormatSingleOutput_StderrOnly(t *testing.T) {
	result := formatSingleOutput("", "error msg\n", 1)
	assert.Equal(t, "error msg\n[exit_code: 1]", result)
}

func TestFormatSingleOutput_Both(t *testing.T) {
	result := formatSingleOutput("out", "err", 0)
	assert.Equal(t, "out\nerr\n[exit_code: 0]", result)
}

func TestFormatSingleOutput_Empty(t *testing.T) {
	result := formatSingleOutput("", "", 0)
	assert.Equal(t, "[exit_code: 0]", result)
}

func TestFormatSingleOutput_NonZeroExit(t *testing.T) {
	result := formatSingleOutput("output", "", 2)
	assert.Equal(t, "output\n[exit_code: 2]", result)
}

func TestFormatBlockOutput_Single(t *testing.T) {
	results := []client.CommandResult{
		{Command: "ls", Stdout: "file.txt\n", Stderr: "", ExitCode: 0},
	}
	got := formatBlockOutput(results)
	assert.Equal(t, "§ ls\nfile.txt\n[exit_code: 0]", got)
}

func TestFormatBlockOutput_Multiple(t *testing.T) {
	results := []client.CommandResult{
		{Command: "make fmt", Stdout: "formatting...\n", Stderr: "", ExitCode: 0},
		{Command: "make test", Stdout: "", Stderr: "FAIL\n", ExitCode: 1},
	}
	got := formatBlockOutput(results)
	expected := "§ make fmt\nformatting...\n[exit_code: 0]\n§ make test\nFAIL\n[exit_code: 1]"
	assert.Equal(t, expected, got)
}

func TestFormatBlockOutput_Empty(t *testing.T) {
	got := formatBlockOutput(nil)
	assert.Equal(t, "", got)
}

func TestCombineOutput(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		stderr string
		want   string
	}{
		{"both empty", "", "", ""},
		{"stdout only", "out\n", "", "out\n"},
		{"stderr only", "", "err\n", "err\n"},
		{"both with newline", "out\n", "err\n", "out\nerr\n"},
		{"both without newline", "out", "err", "out\nerr"},
		{"stdout with newline", "out\n", "err", "out\nerr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, combineOutput(tt.stdout, tt.stderr))
		})
	}
}

func TestBashHandler_SingleCommand(t *testing.T) {
	stub := &stubClient{
		runFunc: func(_ context.Context, req client.RunRequest) (*client.RunResponse, error) {
			assert.Equal(t, "echo hello", req.Command)
			return &client.RunResponse{Stdout: "hello\n", Stderr: "", ExitCode: 0}, nil
		},
	}
	handler := makeBashHandler(stub, nil, nil)
	result, err := callTool(t, handler, bashInput{Command: "echo hello"})
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	text := result.Content[0].(*gosdkmcp.TextContent).Text
	assert.Equal(t, "hello\n[exit_code: 0]", text)
	assert.False(t, result.IsError)
}

func TestBashHandler_BlockCommand(t *testing.T) {
	stub := &stubClient{
		runBlockFunc: func(_ context.Context, req client.RunBlockRequest) (*client.RunBlockResponse, error) {
			assert.Equal(t, blockPrefix, req.Prefix)
			return &client.RunBlockResponse{
				Results: []client.CommandResult{
					{Command: "ls", Stdout: "file.txt\n", Stderr: "", ExitCode: 0},
					{Command: "pwd", Stdout: "/tmp\n", Stderr: "", ExitCode: 0},
				},
			}, nil
		},
	}
	handler := makeBashHandler(stub, nil, nil)
	result, err := callTool(t, handler, bashInput{Command: "§ ls\n§ pwd"})
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	text := result.Content[0].(*gosdkmcp.TextContent).Text
	assert.Contains(t, text, "§ ls\n")
	assert.Contains(t, text, "§ pwd\n")
	assert.Contains(t, text, "[exit_code: 0]")
}

func TestBashHandler_NonZeroExitNotAnError(t *testing.T) {
	stub := &stubClient{
		runFunc: func(_ context.Context, req client.RunRequest) (*client.RunResponse, error) {
			return &client.RunResponse{Stdout: "", Stderr: "command not found\n", ExitCode: 127}, nil
		},
	}
	handler := makeBashHandler(stub, nil, nil)
	result, err := callTool(t, handler, bashInput{Command: "nonexistent"})
	require.NoError(t, err)
	// Non-zero exit code is NOT a tool-level error.
	assert.False(t, result.IsError)
	text := result.Content[0].(*gosdkmcp.TextContent).Text
	assert.Contains(t, text, "[exit_code: 127]")
}

func TestBashHandler_EmptyCommand(t *testing.T) {
	handler := makeBashHandler(&stubClient{}, nil, nil)
	_, err := callTool(t, handler, bashInput{Command: ""})
	assert.Error(t, err)
}

func TestBashHandler_NilParams(t *testing.T) {
	handler := makeBashHandler(&stubClient{}, nil, nil)
	// Simulate malformed client that sends nil Params.
	_, err := handler(context.Background(), &gosdkmcp.CallToolRequest{Params: nil})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing tool call parameters")
}

func TestBashHandler_AllowedPathsPassedThrough(t *testing.T) {
	paths := []client.AllowedPath{{Path: "/project", ReadOnly: false}}
	stub := &stubClient{
		runFunc: func(_ context.Context, req client.RunRequest) (*client.RunResponse, error) {
			assert.Equal(t, paths, req.AllowedPaths)
			return &client.RunResponse{ExitCode: 0}, nil
		},
	}
	handler := makeBashHandler(stub, paths, nil)
	_, err := callTool(t, handler, bashInput{Command: "ls"})
	require.NoError(t, err)
}

func TestBashHandler_TimeoutPassedThrough(t *testing.T) {
	stub := &stubClient{
		runFunc: func(_ context.Context, req client.RunRequest) (*client.RunResponse, error) {
			assert.Equal(t, 30, req.Timeout)
			return &client.RunResponse{ExitCode: 0}, nil
		},
	}
	handler := makeBashHandler(stub, nil, nil)
	_, err := callTool(t, handler, bashInput{Command: "ls", Timeout: 30})
	require.NoError(t, err)
}

func TestBashHandler_StopOnErrorPassedToBlock(t *testing.T) {
	stopFalse := false
	stub := &stubClient{
		runBlockFunc: func(_ context.Context, req client.RunBlockRequest) (*client.RunBlockResponse, error) {
			assert.Equal(t, &stopFalse, req.StopOnError)
			return &client.RunBlockResponse{}, nil
		},
	}
	handler := makeBashHandler(stub, nil, nil)
	_, err := callTool(t, handler, bashInput{Command: "§ ls", StopOnError: &stopFalse})
	require.NoError(t, err)
}

func TestBashHandler_RunErrorPropagated(t *testing.T) {
	sandboxErr := errors.New("daemon unreachable")
	stub := &stubClient{
		runFunc: func(_ context.Context, _ client.RunRequest) (*client.RunResponse, error) {
			return nil, sandboxErr
		},
	}
	handler := makeBashHandler(stub, nil, nil)
	_, err := callTool(t, handler, bashInput{Command: "ls"})
	require.Error(t, err)
	assert.ErrorIs(t, err, sandboxErr)
}

func TestBashHandler_RunBlockErrorPropagated(t *testing.T) {
	sandboxErr := errors.New("daemon unreachable")
	stub := &stubClient{
		runBlockFunc: func(_ context.Context, _ client.RunBlockRequest) (*client.RunBlockResponse, error) {
			return nil, sandboxErr
		},
	}
	handler := makeBashHandler(stub, nil, nil)
	_, err := callTool(t, handler, bashInput{Command: "§ ls"})
	require.Error(t, err)
	assert.ErrorIs(t, err, sandboxErr)
}

func TestResolveAllowedPaths_ReadOnly(t *testing.T) {
	t.Setenv("TEMENOS_WRITE", "")
	paths, err := resolveAllowedPaths()
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	// First path (cwd) must be read-only when TEMENOS_WRITE is not set.
	assert.True(t, paths[0].ReadOnly, "cwd should be read-only without TEMENOS_WRITE=true")
}

func TestResolveAllowedPaths_ReadWrite(t *testing.T) {
	t.Setenv("TEMENOS_WRITE", "true")
	paths, err := resolveAllowedPaths()
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	// First path (cwd) must be read-write when TEMENOS_WRITE=true.
	assert.False(t, paths[0].ReadOnly, "cwd should be read-write with TEMENOS_WRITE=true")
}

func TestResolveAllowedPaths_CwdIsFirst(t *testing.T) {
	paths, err := resolveAllowedPaths()
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, cwd, paths[0].Path)
}

func TestParseTemenosPaths_Empty(t *testing.T) {
	assert.Nil(t, parseTemenosPaths(""))
}

func TestParseTemenosPaths_SinglePath(t *testing.T) {
	paths := parseTemenosPaths("/data/shared")
	require.Len(t, paths, 1)
	assert.Equal(t, "/data/shared", paths[0].Path)
	assert.True(t, paths[0].ReadOnly, "default should be read-only")
}

func TestParseTemenosPaths_ReadWriteModifier(t *testing.T) {
	paths := parseTemenosPaths("/data/shared:rw")
	require.Len(t, paths, 1)
	assert.Equal(t, "/data/shared", paths[0].Path)
	assert.False(t, paths[0].ReadOnly)
}

func TestParseTemenosPaths_ReadOnlyModifier(t *testing.T) {
	paths := parseTemenosPaths("/config:ro")
	require.Len(t, paths, 1)
	assert.Equal(t, "/config", paths[0].Path)
	assert.True(t, paths[0].ReadOnly)
}

func TestParseTemenosPaths_MultiplePaths(t *testing.T) {
	paths := parseTemenosPaths("/home/.ttal:rw,/home/.task:rw,/home/.config/ttal:ro")
	require.Len(t, paths, 3)
	assert.Equal(t, "/home/.ttal", paths[0].Path)
	assert.False(t, paths[0].ReadOnly)
	assert.Equal(t, "/home/.task", paths[1].Path)
	assert.False(t, paths[1].ReadOnly)
	assert.Equal(t, "/home/.config/ttal", paths[2].Path)
	assert.True(t, paths[2].ReadOnly)
}

func TestParseTemenosPaths_DefaultReadOnly(t *testing.T) {
	paths := parseTemenosPaths("/data:rw,/config:ro,/logs")
	require.Len(t, paths, 3)
	assert.Equal(t, "/data", paths[0].Path)
	assert.False(t, paths[0].ReadOnly)
	assert.Equal(t, "/config", paths[1].Path)
	assert.True(t, paths[1].ReadOnly)
	assert.Equal(t, "/logs", paths[2].Path)
	assert.True(t, paths[2].ReadOnly, "no suffix should default to read-only")
}

func TestResolveAllowedPaths_IncludesTemenosPaths(t *testing.T) {
	t.Setenv("TEMENOS_PATHS", "/extra/path:rw")
	t.Setenv("TEMENOS_WRITE", "")
	paths, err := resolveAllowedPaths()
	require.NoError(t, err)
	// Should have at least cwd + /extra/path.
	found := false
	for _, p := range paths {
		if p.Path == "/extra/path" {
			found = true
			assert.False(t, p.ReadOnly)
		}
	}
	assert.True(t, found, "TEMENOS_PATHS entry should be in allowed paths")
}

func TestCollectSandboxEnv_ForwardsAllEnv(t *testing.T) {
	t.Setenv("TTAL_JOB_ID", "abc123")
	t.Setenv("TASKRC", "/path/to/taskrc")
	t.Setenv("CUSTOM_VAR", "custom-value")

	env := collectSandboxEnv()
	assert.Equal(t, "abc123", env["TTAL_JOB_ID"])
	assert.Equal(t, "/path/to/taskrc", env["TASKRC"])
	assert.Equal(t, "custom-value", env["CUSTOM_VAR"])
}

func TestCollectSandboxEnv_IncludesAllProcessEnv(t *testing.T) {
	env := collectSandboxEnv()
	// Should contain at least PATH and HOME from the process.
	assert.NotEmpty(t, env["PATH"])
	assert.NotEmpty(t, env["HOME"])
}

func TestBashHandler_EnvForwardedToRun(t *testing.T) {
	env := map[string]string{"TTAL_JOB_ID": "test123"}
	stub := &stubClient{
		runFunc: func(_ context.Context, req client.RunRequest) (*client.RunResponse, error) {
			assert.Equal(t, env, req.Env)
			return &client.RunResponse{ExitCode: 0}, nil
		},
	}
	handler := makeBashHandler(stub, nil, env)
	_, err := callTool(t, handler, bashInput{Command: "ls"})
	require.NoError(t, err)
}

func TestAppendAncestorPaths_AddsAncestors(t *testing.T) {
	paths := []client.AllowedPath{
		{Path: "/Users/neil/Code/project", ReadOnly: false},
	}
	result := appendAncestorPaths(paths)

	byPath := make(map[string]client.AllowedPath)
	for _, p := range result {
		byPath[p.Path] = p
	}
	for _, ancestor := range []string{"/Users/neil/Code", "/Users/neil", "/Users"} {
		p, ok := byPath[ancestor]
		assert.True(t, ok, "ancestor %s should be present", ancestor)
		assert.True(t, p.ReadOnly, "ancestor %s should be read-only", ancestor)
	}
}

func TestAppendAncestorPaths_NoDuplicates(t *testing.T) {
	paths := []client.AllowedPath{
		{Path: "/Users/neil/Code/project-a", ReadOnly: false},
		{Path: "/Users/neil/Code/project-b", ReadOnly: true},
	}
	result := appendAncestorPaths(paths)

	counts := make(map[string]int)
	for _, p := range result {
		counts[p.Path]++
	}
	for path, count := range counts {
		assert.Equal(t, 1, count, "path %s should appear exactly once", path)
	}
}

func TestAppendAncestorPaths_DoesNotDuplicateExisting(t *testing.T) {
	paths := []client.AllowedPath{
		{Path: "/Users/neil/Code", ReadOnly: false},
		{Path: "/Users/neil/Code/project", ReadOnly: true},
	}
	result := appendAncestorPaths(paths)

	counts := make(map[string]int)
	for _, p := range result {
		counts[p.Path]++
	}
	assert.Equal(t, 1, counts["/Users/neil/Code"], "already-existing path should not be duplicated")
	// Original read-write entry should be preserved (not overwritten by read-only ancestor).
	for _, p := range result {
		if p.Path == "/Users/neil/Code" {
			assert.False(t, p.ReadOnly, "original rw entry should be preserved")
			break
		}
	}
}

func TestAppendAncestorPaths_SingleComponentPath(t *testing.T) {
	paths := []client.AllowedPath{
		{Path: "/tmp", ReadOnly: true},
	}
	result := appendAncestorPaths(paths)
	// /tmp's only parent is /, which is excluded — no ancestors added.
	assert.Len(t, result, 1)
}

func TestAppendAncestorPaths_ExcludesRoot(t *testing.T) {
	paths := []client.AllowedPath{
		{Path: "/Users/neil", ReadOnly: false},
	}
	result := appendAncestorPaths(paths)
	for _, p := range result {
		assert.NotEqual(t, "/", p.Path, "root should not be added as ancestor")
	}
}

func TestAppendAncestorPaths_Empty(t *testing.T) {
	result := appendAncestorPaths(nil)
	assert.Nil(t, result)
}

func TestBashHandler_EnvForwardedToRunBlock(t *testing.T) {
	env := map[string]string{"TTAL_AGENT_NAME": "worker"}
	stub := &stubClient{
		runBlockFunc: func(_ context.Context, req client.RunBlockRequest) (*client.RunBlockResponse, error) {
			assert.Equal(t, env, req.Env)
			return &client.RunBlockResponse{}, nil
		},
	}
	handler := makeBashHandler(stub, nil, env)
	_, err := callTool(t, handler, bashInput{Command: "§ ls"})
	require.NoError(t, err)
}
