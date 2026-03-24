package daemon

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/temenos/sandbox"
)

// captureSandbox records the ExecConfig it receives.
type captureSandbox struct {
	lastCfg *sandbox.ExecConfig
}

func (c *captureSandbox) Exec(_ context.Context, _ string, cfg *sandbox.ExecConfig) (string, string, int, error) {
	c.lastCfg = cfg
	return "", "", 0, nil
}

func (c *captureSandbox) IsAvailable() bool { return true }

func TestHandleRun_SetsWorkingDir(t *testing.T) {
	sbx := &captureSandbox{}
	req := RunRequest{
		Command:      "pwd",
		AllowedPaths: []AllowedPath{{Path: "/Users/neil/project", ReadOnly: true}},
	}
	_, err := handleRun(t.Context(), sbx, req)
	require.NoError(t, err)
	require.NotNil(t, sbx.lastCfg)
	assert.Equal(t, "/Users/neil/project", sbx.lastCfg.WorkingDir)
}

func TestHandleRun_NoAllowedPaths_EmptyWorkingDir(t *testing.T) {
	sbx := &captureSandbox{}
	req := RunRequest{Command: "echo hi"}
	_, err := handleRun(t.Context(), sbx, req)
	require.NoError(t, err)
	require.NotNil(t, sbx.lastCfg)
	assert.Empty(t, sbx.lastCfg.WorkingDir)
}
