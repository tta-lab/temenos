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
	handler := makeBashHandler(stub, nil)
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
	handler := makeBashHandler(stub, nil)
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
	handler := makeBashHandler(stub, nil)
	result, err := callTool(t, handler, bashInput{Command: "nonexistent"})
	require.NoError(t, err)
	// Non-zero exit code is NOT a tool-level error.
	assert.False(t, result.IsError)
	text := result.Content[0].(*gosdkmcp.TextContent).Text
	assert.Contains(t, text, "[exit_code: 127]")
}

func TestBashHandler_EmptyCommand(t *testing.T) {
	handler := makeBashHandler(&stubClient{}, nil)
	_, err := callTool(t, handler, bashInput{Command: ""})
	assert.Error(t, err)
}

func TestBashHandler_NilParams(t *testing.T) {
	handler := makeBashHandler(&stubClient{}, nil)
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
	handler := makeBashHandler(stub, paths)
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
	handler := makeBashHandler(stub, nil)
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
	handler := makeBashHandler(stub, nil)
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
	handler := makeBashHandler(stub, nil)
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
	handler := makeBashHandler(stub, nil)
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
	cwd, err := osGetwd()
	require.NoError(t, err)
	assert.Equal(t, cwd, paths[0].Path)
}

// osGetwd is a thin wrapper so tests can call it without importing "os" directly.
func osGetwd() (string, error) { return os.Getwd() }
