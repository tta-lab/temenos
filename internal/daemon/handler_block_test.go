package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/temenos/sandbox"
)

// sequenceSandbox returns different results for each Exec call.
type sequenceSandbox struct {
	results []struct {
		stdout   string
		stderr   string
		exitCode int
	}
	calls    int
	lastArgs []string // records command args in order
}

func (s *sequenceSandbox) Exec(_ context.Context, cmd string, _ *sandbox.ExecConfig) (string, string, int, error) {
	s.lastArgs = append(s.lastArgs, cmd)
	if s.calls >= len(s.results) {
		return "", "", 0, nil
	}
	r := s.results[s.calls]
	s.calls++
	return r.stdout, r.stderr, r.exitCode, nil
}

func (s *sequenceSandbox) IsAvailable() bool { return true }

// deadlineSandbox records the deadline from each Exec call's context.
type deadlineSandbox struct {
	deadlines []time.Time
}

func (d *deadlineSandbox) Exec(ctx context.Context, _ string, _ *sandbox.ExecConfig) (string, string, int, error) {
	if dl, ok := ctx.Deadline(); ok {
		d.deadlines = append(d.deadlines, dl)
	}
	return "", "", 0, nil
}

func (d *deadlineSandbox) IsAvailable() bool { return true }

func boolPtr(b bool) *bool { return &b }

func TestHandleRunBlock_Basic(t *testing.T) {
	sbx := &sequenceSandbox{
		results: []struct {
			stdout   string
			stderr   string
			exitCode int
		}{
			{stdout: "out1", exitCode: 0},
			{stdout: "out2", exitCode: 0},
		},
	}
	req := RunBlockRequest{
		Block:  "§ echo one\n§ echo two\n",
		Prefix: "§ ",
	}
	resp, err := handleRunBlock(t.Context(), sbx, req)
	require.NoError(t, err)
	require.Len(t, resp.Results, 2)
	assert.Equal(t, "echo one", resp.Results[0].Command)
	assert.Equal(t, "out1", resp.Results[0].Stdout)
	assert.Equal(t, "echo two", resp.Results[1].Command)
	assert.Equal(t, "out2", resp.Results[1].Stdout)
}

func TestHandleRunBlock_StopOnError_Default(t *testing.T) {
	// StopOnError is nil (omitted) → defaults to true → stops after first failure
	sbx := &sequenceSandbox{
		results: []struct {
			stdout   string
			stderr   string
			exitCode int
		}{
			{exitCode: 0},
			{exitCode: 1},
			{exitCode: 0},
		},
	}
	req := RunBlockRequest{
		Block:  "§ cmd1\n§ cmd2\n§ cmd3\n",
		Prefix: "§ ",
		// StopOnError omitted → default true
	}
	resp, err := handleRunBlock(t.Context(), sbx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Results, 2, "should stop after cmd2 (exit code 1)")
}

func TestHandleRunBlock_StopOnError_True(t *testing.T) {
	sbx := &sequenceSandbox{
		results: []struct {
			stdout   string
			stderr   string
			exitCode int
		}{
			{exitCode: 0},
			{exitCode: 1},
			{exitCode: 0},
		},
	}
	req := RunBlockRequest{
		Block:       "§ cmd1\n§ cmd2\n§ cmd3\n",
		Prefix:      "§ ",
		StopOnError: boolPtr(true),
	}
	resp, err := handleRunBlock(t.Context(), sbx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Results, 2, "should stop after cmd2 (exit code 1)")
}

func TestHandleRunBlock_StopOnError_False(t *testing.T) {
	sbx := &sequenceSandbox{
		results: []struct {
			stdout   string
			stderr   string
			exitCode int
		}{
			{exitCode: 0},
			{exitCode: 1},
			{exitCode: 0},
		},
	}
	req := RunBlockRequest{
		Block:       "§ cmd1\n§ cmd2\n§ cmd3\n",
		Prefix:      "§ ",
		StopOnError: boolPtr(false),
	}
	resp, err := handleRunBlock(t.Context(), sbx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Results, 3, "should execute all 3 commands")
}

func TestHandleRunBlock_Heredoc(t *testing.T) {
	sbx := &sequenceSandbox{
		results: []struct {
			stdout   string
			stderr   string
			exitCode int
		}{
			{stdout: "heredoc-out", exitCode: 0},
			{stdout: "ls-out", exitCode: 0},
		},
	}
	req := RunBlockRequest{
		Block:  "§ cat <<'EOF'\nhello\nEOF\n§ ls\n",
		Prefix: "§ ",
	}
	resp, err := handleRunBlock(t.Context(), sbx, req)
	require.NoError(t, err)
	require.Len(t, resp.Results, 2)
	// First command should have full heredoc body
	assert.Equal(t, "cat <<'EOF'\nhello\nEOF", sbx.lastArgs[0])
	assert.Equal(t, "ls", sbx.lastArgs[1])
}

func TestHandleRunBlock_Validation_EmptyBlock(t *testing.T) {
	sbx := &captureSandbox{}
	req := RunBlockRequest{
		Block:  "",
		Prefix: "§ ",
	}
	_, err := handleRunBlock(t.Context(), sbx, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, errHTTPValidation)
}

func TestHandleRunBlock_Validation_EmptyPrefix(t *testing.T) {
	sbx := &captureSandbox{}
	req := RunBlockRequest{
		Block:  "§ ls\n",
		Prefix: "",
	}
	_, err := handleRunBlock(t.Context(), sbx, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, errHTTPValidation)
}

func TestHandleRunBlock_Validation_InvalidPath(t *testing.T) {
	sbx := &captureSandbox{}
	req := RunBlockRequest{
		Block:        "§ ls\n",
		Prefix:       "§ ",
		AllowedPaths: []AllowedPath{{Path: "relative/path", ReadOnly: false}},
	}
	_, err := handleRunBlock(t.Context(), sbx, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, errHTTPValidation)
}

func TestHandleRunBlock_NoMatchingCommands(t *testing.T) {
	sbx := &captureSandbox{}
	req := RunBlockRequest{
		Block:  "just some text\nno commands here\n",
		Prefix: "§ ",
	}
	resp, err := handleRunBlock(t.Context(), sbx, req)
	require.NoError(t, err)
	assert.Empty(t, resp.Results, "no prefix-matching lines → empty results, not error")
}

func TestHandleRunBlock_Timeout_PerCommand(t *testing.T) {
	sbx := &deadlineSandbox{}
	req := RunBlockRequest{
		Block:   "§ cmd1\n§ cmd2\n",
		Prefix:  "§ ",
		Timeout: 5,
	}
	resp, err := handleRunBlock(t.Context(), sbx, req)
	require.NoError(t, err)
	require.Len(t, resp.Results, 2)
	require.Len(t, sbx.deadlines, 2)

	// Each command should have a ~5s deadline from now (not cumulative).
	// Allow 2s window for test execution time.
	for i, dl := range sbx.deadlines {
		until := time.Until(dl)
		assert.True(t, until > 2*time.Second && until <= 5*time.Second+500*time.Millisecond,
			"cmd[%d] deadline should be ~5s from now, got %v", i, until)
	}
}

func TestHandleRunBlock_ContextCancellation(t *testing.T) {
	sbx := &sequenceSandbox{
		results: []struct {
			stdout   string
			stderr   string
			exitCode int
		}{
			{exitCode: 0},
			{exitCode: 0},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel context before the call so the second command should not execute.
	// We'll use a sandbox that cancels after first Exec.
	cancelAfterOneSandbox := &cancelAfterNSandbox{inner: sbx, n: 1, cancel: cancel}

	req := RunBlockRequest{
		Block:  "§ cmd1\n§ cmd2\n",
		Prefix: "§ ",
	}
	resp, err := handleRunBlock(ctx, cancelAfterOneSandbox, req)
	require.NoError(t, err)
	assert.Len(t, resp.Results, 1, "second command should not execute after context cancel")
}

// cancelAfterNSandbox cancels the context after n Exec calls.
type cancelAfterNSandbox struct {
	inner  sandbox.Sandbox
	n      int
	cancel context.CancelFunc
	calls  int
}

func (c *cancelAfterNSandbox) Exec(
	ctx context.Context, cmd string, cfg *sandbox.ExecConfig,
) (string, string, int, error) {
	stdout, stderr, exitCode, err := c.inner.Exec(ctx, cmd, cfg)
	c.calls++
	if c.calls >= c.n {
		c.cancel()
	}
	return stdout, stderr, exitCode, err
}

func (c *cancelAfterNSandbox) IsAvailable() bool { return true }
