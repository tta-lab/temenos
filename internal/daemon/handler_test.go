package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

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

type deadlineSandbox struct {
	deadline chan deadlineObservation
}

type deadlineObservation struct {
	deadline time.Time
	ok       bool
}

func (d *deadlineSandbox) Exec(ctx context.Context, _ string, _ *sandbox.ExecConfig) (string, string, int, error) {
	deadline, ok := ctx.Deadline()
	d.deadline <- deadlineObservation{deadline: deadline, ok: ok}
	return "", "", 0, nil
}

func (d *deadlineSandbox) IsAvailable() bool { return true }

func TestHandleRun_SetsWorkingDir(t *testing.T) {
	sbx := &captureSandbox{}
	cfg := &sandbox.Config{AllowRead: []string{"/baseline/read"}}
	req := RunRequest{
		Command:      "pwd",
		AllowedPaths: []AllowedPath{{Path: "/Users/neil/project", ReadOnly: true}},
	}
	_, err := handleRun(t.Context(), cfg, sbx, NewBackgroundJobManager(), nil, req)
	require.NoError(t, err)
	require.NotNil(t, sbx.lastCfg)
	assert.Equal(t, "/Users/neil/project", sbx.lastCfg.WorkingDir)
}

func TestHandleRun_NoAllowedPaths_FallsBackToTempDir(t *testing.T) {
	sbx := &captureSandbox{}
	req := RunRequest{Command: "echo hi"}
	_, err := handleRun(t.Context(), &sandbox.Config{}, sbx, NewBackgroundJobManager(), nil, req)
	require.NoError(t, err)
	require.NotNil(t, sbx.lastCfg)
	assert.Equal(t, os.TempDir(), sbx.lastCfg.WorkingDir)
}

func TestHandleRun_DefaultsRunTimeoutToTwentyMinutes(t *testing.T) {
	sbx := &deadlineSandbox{deadline: make(chan deadlineObservation, 1)}
	start := time.Now()

	_, err := handleRun(t.Context(), &sandbox.Config{}, sbx, NewBackgroundJobManager(), nil, RunRequest{
		Command: "echo hi",
	})
	require.NoError(t, err)

	observed := <-sbx.deadline
	require.True(t, observed.ok, "sandbox context should have a deadline")
	assert.WithinDuration(t, start.Add(20*time.Minute), observed.deadline, time.Second)
}

func TestHandleRun_UsesRequestTimeout(t *testing.T) {
	sbx := &deadlineSandbox{deadline: make(chan deadlineObservation, 1)}
	start := time.Now()

	_, err := handleRun(t.Context(), &sandbox.Config{}, sbx, NewBackgroundJobManager(), nil, RunRequest{
		Command: "echo hi",
		Timeout: 30,
	})
	require.NoError(t, err)

	observed := <-sbx.deadline
	require.True(t, observed.ok, "sandbox context should have a deadline")
	assert.WithinDuration(t, start.Add(30*time.Second), observed.deadline, time.Second)
}

func TestHandleRun_BackgroundJobOutlivesRequestContext(t *testing.T) {
	mgr := NewBackgroundJobManager()
	sbx := &mockSandbox{delay: 10 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())

	resp, err := handleRun(ctx, &sandbox.Config{AutoBackgroundAfter: 1}, sbx, mgr, nil, RunRequest{
		Command: "sleep 10",
		Timeout: 30,
	})
	require.NoError(t, err)
	require.Equal(t, "background", resp.Status)
	require.NotEmpty(t, resp.JobID)

	cancel()
	time.Sleep(50 * time.Millisecond)

	job := mgr.Get(resp.JobID)
	require.NotNil(t, job)
	assert.False(t, job.IsDone())
	job.cancel()
	job.Wait()
}

func TestHTTPRun_IgnoresAutoBackgroundAfterRequestField(t *testing.T) {
	mgr := NewBackgroundJobManager()
	sbx := &mockSandbox{delay: 1500 * time.Millisecond}
	handler := handleHTTPRunValidating(httpHandlers{
		run: func(ctx context.Context, req RunRequest) (*RunResponse, error) {
			return handleRun(ctx, &sandbox.Config{AutoBackgroundAfter: 1}, sbx, mgr, nil, req)
		},
	})

	body := []byte(`{"command":"sleep 2","auto_background_after":30,"timeout":5}`)
	req := httptest.NewRequest(http.MethodPost, "/run", bytes.NewReader(body))
	ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp RunResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, "background", resp.Status)
	require.NotEmpty(t, resp.JobID)
}

func TestBuildMounts_MetadataOnlyPassedThrough(t *testing.T) {
	paths := []AllowedPath{
		{Path: "/some/path", ReadOnly: true, MetadataOnly: true},
	}
	mounts, err := buildMounts(nil, paths)
	require.NoError(t, err)
	// The MetadataOnly mount is returned, but no ancestors are added for it.
	require.Len(t, mounts, 1)
	assert.True(t, mounts[0].MetadataOnly)
	assert.Equal(t, "/some/path", mounts[0].Source)
}

func TestBuildMounts_AncestorMetadataInjected(t *testing.T) {
	paths := []AllowedPath{
		{Path: "/Users/neil/Code/project", ReadOnly: true},
	}
	mounts, err := buildMounts(nil, paths)
	require.NoError(t, err)

	// Collect by path for easy lookup.
	byPath := make(map[string]sandbox.Mount)
	for _, m := range mounts {
		byPath[m.Source] = m
	}

	// The explicit mount should be first (WorkingDir preservation).
	assert.Equal(t, "/Users/neil/Code/project", mounts[0].Source)
	assert.False(t, mounts[0].MetadataOnly)

	// Ancestors are injected as MetadataOnly.
	for _, anc := range []string{"/Users/neil/Code", "/Users/neil", "/Users"} {
		m, ok := byPath[anc]
		assert.True(t, ok, "ancestor %s should be present", anc)
		assert.True(t, m.MetadataOnly, "ancestor %s should be MetadataOnly", anc)
	}

	// Root should NOT be added.
	_, rootPresent := byPath["/"]
	assert.False(t, rootPresent, "root should not be added as ancestor")
}

func TestBuildMounts_AncestorDeduplicatesExistingMounts(t *testing.T) {
	paths := []AllowedPath{
		{Path: "/Users/neil/Code/project", ReadOnly: true},
		{Path: "/Users/neil", ReadOnly: false}, // already present — should not be duplicated
	}
	mounts, err := buildMounts(nil, paths)
	require.NoError(t, err)

	counts := make(map[string]int)
	for _, m := range mounts {
		counts[m.Source]++
	}

	assert.Equal(t, 1, counts["/Users/neil"], "/Users/neil should appear exactly once")
	// The explicit rw entry should be preserved (not replaced by MetadataOnly).
	for _, m := range mounts {
		if m.Source == "/Users/neil" {
			assert.False(t, m.MetadataOnly, "explicit rw mount should not become MetadataOnly")
			break
		}
	}
}

func TestBuildMounts_WorkingDirPreservedWithAncestors(t *testing.T) {
	sbx := &captureSandbox{}
	cfg := &sandbox.Config{AllowWrite: []string{"/baseline/write"}}
	req := RunRequest{
		Command: "pwd",
		AllowedPaths: []AllowedPath{
			{Path: "/Users/neil/Code/project", ReadOnly: true},
		},
	}
	_, err := handleRun(t.Context(), cfg, sbx, NewBackgroundJobManager(), nil, req)
	require.NoError(t, err)
	require.NotNil(t, sbx.lastCfg)
	// WorkingDir must still be the explicit mount, not an ancestor.
	assert.Equal(t, "/Users/neil/Code/project", sbx.lastCfg.WorkingDir)
}

func TestBuildMounts_BaselinePrecedesRequestPaths(t *testing.T) {
	baseline := []sandbox.Mount{
		{Source: "/baseline/read", Target: "/baseline/read", ReadOnly: true},
		{Source: "/baseline/write", Target: "/baseline/write", ReadOnly: false},
	}
	paths := []AllowedPath{
		{Path: "/request/path", ReadOnly: false},
	}

	mounts, err := buildMounts(baseline, paths)
	require.NoError(t, err)

	// Baseline mounts must appear before request mounts.
	require.GreaterOrEqual(t, len(mounts), 3)
	assert.Equal(t, "/baseline/read", mounts[0].Source, "first baseline mount must be first")
	assert.True(t, mounts[0].ReadOnly)
	assert.Equal(t, "/baseline/write", mounts[1].Source, "second baseline mount must be second")
	assert.False(t, mounts[1].ReadOnly)

	// Request path must appear after baseline.
	var foundRequest bool
	for _, m := range mounts[2:] {
		if m.Source == "/request/path" {
			foundRequest = true
		}
	}
	assert.True(t, foundRequest, "request path must appear after baseline mounts")
}

func TestHandleRun_FiltersDisallowedEnvKeys(t *testing.T) {
	cfg := &sandbox.Config{AllowEnv: []string{"FOO"}}
	var sbx captureSandbox
	req := RunRequest{Command: "echo", Env: map[string]string{"FOO": "1", "BAR": "2"}}

	_, err := handleRun(context.Background(), cfg, &sbx, NewBackgroundJobManager(), nil, req)
	require.NoError(t, err)

	found := false
	for _, e := range sbx.lastCfg.Env {
		if e == "FOO=1" {
			found = true
		}
		assert.NotEqual(t, "BAR=", e[:4], "BAR should not appear in env")
	}
	assert.True(t, found, "FOO should be in env")
}

func TestHandleRun_EmptyUserAllowEnv_BaselineStillPasses(t *testing.T) {
	cfg := &sandbox.Config{AllowEnv: nil}
	var sbx captureSandbox
	req := RunRequest{Command: "echo", Env: map[string]string{"FOO": "1", "USER": "alice", "HOME": "/home/alice"}}

	_, err := handleRun(context.Background(), cfg, &sbx, NewBackgroundJobManager(), nil, req)
	require.NoError(t, err)

	hasUser := false
	hasHome := false
	for _, e := range sbx.lastCfg.Env {
		if e == "USER=alice" {
			hasUser = true
		}
		if e == "HOME=/home/alice" {
			hasHome = true
		}
		assert.NotEqual(t, "FOO=", e[:4], "FOO should not appear in env")
	}
	assert.True(t, hasUser, "USER should pass via baseline")
	assert.True(t, hasHome, "HOME should pass via baseline")
}

func TestHandleRun_GlobAllowEnv_RetainsMatching(t *testing.T) {
	cfg := &sandbox.Config{AllowEnv: []string{"TTAL_*"}}
	var sbx captureSandbox
	req := RunRequest{Command: "echo", Env: map[string]string{"TTAL_JOB_ID": "abc", "OTHER": "xyz"}}

	_, err := handleRun(context.Background(), cfg, &sbx, NewBackgroundJobManager(), nil, req)
	require.NoError(t, err)

	found := false
	for _, e := range sbx.lastCfg.Env {
		if e == "TTAL_JOB_ID=abc" {
			found = true
		}
		assert.NotEqual(t, "OTHER=", e[:6], "OTHER should not appear in env")
	}
	assert.True(t, found, "TTAL_JOB_ID should be in env")
}

func TestHandleRun_NilEnv_DoesNotCrash(t *testing.T) {
	cfg := &sandbox.Config{AllowEnv: []string{"FOO"}}
	var sbx captureSandbox
	req := RunRequest{Command: "echo"} // Env field not set

	_, err := handleRun(context.Background(), cfg, &sbx, NewBackgroundJobManager(), nil, req)
	require.NoError(t, err)

	assert.Empty(t, sbx.lastCfg.Env)
}

func TestHandleRun_InvalidEnvKey_ReturnsError(t *testing.T) {
	cfg := &sandbox.Config{}
	var sbx captureSandbox
	req := RunRequest{Command: "echo", Env: map[string]string{"invalid-key": "value"}}

	_, err := handleRun(context.Background(), cfg, &sbx, NewBackgroundJobManager(), nil, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "env")
}

func TestHandleRun_EmptySliceAllowEnv_BaselineStillPasses(t *testing.T) {
	cfg := &sandbox.Config{AllowEnv: []string{}}
	var sbx captureSandbox
	req := RunRequest{Command: "echo", Env: map[string]string{"FOO": "1", "USER": "alice", "HOME": "/home/alice"}}

	_, err := handleRun(context.Background(), cfg, &sbx, NewBackgroundJobManager(), nil, req)
	require.NoError(t, err)

	hasUser := false
	hasHome := false
	for _, e := range sbx.lastCfg.Env {
		if e == "USER=alice" {
			hasUser = true
		}
		if e == "HOME=/home/alice" {
			hasHome = true
		}
		assert.NotEqual(t, "FOO=", e[:4], "FOO should not appear in env")
	}
	assert.True(t, hasUser, "USER should pass via baseline")
	assert.True(t, hasHome, "HOME should pass via baseline")
}

func TestValidateEnv_InvalidEnvValue(t *testing.T) {
	err := validateEnv(map[string]string{"FOO": "value\x00nul"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NUL")

	err = validateEnv(map[string]string{"BAR": "line\nbreak"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LF")

	err = validateEnv(map[string]string{"BAZ": "carriage\rreturn"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CR")
}

func TestValidateEnv_ValidValues(t *testing.T) {
	err := validateEnv(map[string]string{"FOO": "bar", "BAZ": "qux"})
	require.NoError(t, err)
}

func TestIsValidEnvName_RejectsLeadingDigit(t *testing.T) {
	assert.False(t, isValidEnvName("0ABC"))
}

func TestIsValidEnvName_RejectsGlobChars(t *testing.T) {
	assert.False(t, isValidEnvName("FOO*"))
	assert.False(t, isValidEnvName("BAR?"))
}

func TestIsValidEnvName_LowercaseAccepted(t *testing.T) {
	assert.True(t, isValidEnvName("my_var"))
	assert.True(t, isValidEnvName("PATH"))
}
