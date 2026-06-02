package sandbox

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireUserCurrent skips the test if os/user.Current() is unavailable
// (e.g. in bubblewrap environments without /etc/passwd).
func requireUserCurrent(t *testing.T) {
	t.Helper()
	if _, err := user.Current(); err != nil {
		t.Skipf("os/user.Current() unavailable: %v", err)
	}
}

func TestLoad_ValidTOMLConfig(t *testing.T) {
	requireUserCurrent(t)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
allow_read = ["/read/path1", "/read/path2"]
allow_write = ["/write/path1"]
socket_path = "/custom/daemon.sock"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, []string{"/read/path1", "/read/path2"}, cfg.AllowRead)
	assert.Equal(t, []string{"/write/path1"}, cfg.AllowWrite)
	assert.Equal(t, "/custom/daemon.sock", cfg.SocketPath)
	assert.Equal(t, DefaultAutoBackgroundAfter, cfg.AutoBackgroundAfter)
}

func TestLoad_AutoBackgroundAfterFromConfig(t *testing.T) {
	requireUserCurrent(t)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
auto_background_after = 1
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, 1, cfg.AutoBackgroundAfter)
}

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	requireUserCurrent(t)
	cfg, err := Load("/nonexistent/path/config.toml")
	require.NoError(t, err)

	assert.Equal(t, DefaultAutoBackgroundAfter, cfg.AutoBackgroundAfter)
	assert.NotEmpty(t, cfg.SocketPath)
	assert.Contains(t, cfg.SocketPath, ".temenos/daemon.sock")

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".temenos/daemon.sock"), cfg.SocketPath)

	assert.Empty(t, cfg.AllowWrite)
}

func TestLoad_EmptyPath_UsesDefaultConfigPath(t *testing.T) {
	requireUserCurrent(t)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
socket_path = "/tmp/temenos.sock"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	t.Setenv("TEMENOS_CONFIG_PATH", configPath)
	defer t.Setenv("TEMENOS_CONFIG_PATH", "")

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/temenos.sock", cfg.SocketPath)
}

func TestLoad_TildeExpansion(t *testing.T) {
	requireUserCurrent(t)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
allow_read = ["~/read/path1", "~/documents"]
allow_write = ["~/write/data"]
socket_path = "~/daemon.sock"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, []string{filepath.Join(home, "/read/path1"), filepath.Join(home, "/documents")}, cfg.AllowRead)
	assert.Equal(t, []string{filepath.Join(home, "/write/data")}, cfg.AllowWrite)
	assert.Equal(t, filepath.Join(home, "/daemon.sock"), cfg.SocketPath)
}

func TestLoad_PartialConfig_DefaultsSocketPath(t *testing.T) {
	requireUserCurrent(t)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
allow_read = ["/read/path1"]
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.NotEmpty(t, cfg.SocketPath)

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".temenos/daemon.sock"), cfg.SocketPath)
}

func TestExpandHome_TildePrefix(t *testing.T) {
	requireUserCurrent(t)
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	result, err := ExpandHome("~/some/path")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "/some/path"), result)
}

func TestExpandHome_NonTildePath(t *testing.T) {
	result, err := ExpandHome("/absolute/path")
	require.NoError(t, err)
	assert.Equal(t, "/absolute/path", result)
}

func TestExpandHome_EmptyString(t *testing.T) {
	result, err := ExpandHome("")
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestDefaultConfigPath_WithEnvVar(t *testing.T) {
	requireUserCurrent(t)
	tmpDir := t.TempDir()
	expectedPath := filepath.Join(tmpDir, "custom.toml")
	t.Setenv("TEMENOS_CONFIG_PATH", expectedPath)
	defer t.Setenv("TEMENOS_CONFIG_PATH", "")

	path, err := DefaultConfigPath()
	require.NoError(t, err)
	assert.Equal(t, expectedPath, path)
}

func TestDefaultConfigPath_WithoutEnvVar(t *testing.T) {
	requireUserCurrent(t)
	t.Setenv("TEMENOS_CONFIG_PATH", "")
	defer t.Setenv("TEMENOS_CONFIG_PATH", "")

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	expectedPath := filepath.Join(home, ".config", "temenos", "config.toml")

	path, err := DefaultConfigPath()
	require.NoError(t, err)
	assert.Equal(t, expectedPath, path)
}

func TestBaselineMounts(t *testing.T) {
	cfg := &Config{
		AllowRead:  []string{"/read/path1", "/read/path2"},
		AllowWrite: []string{"/write/path1"},
	}
	mounts := cfg.BaselineMounts()
	require.Len(t, mounts, 3)
	assert.Equal(t, "/read/path1", mounts[0].Source)
	assert.True(t, mounts[0].ReadOnly)
	assert.Equal(t, "/read/path2", mounts[1].Source)
	assert.True(t, mounts[1].ReadOnly)
	assert.Equal(t, "/write/path1", mounts[2].Source)
	assert.False(t, mounts[2].ReadOnly)
}

func TestBaselineMounts_Empty(t *testing.T) {
	cfg := &Config{}
	mounts := cfg.BaselineMounts()
	assert.Empty(t, mounts)
}

func TestLoad_AllowEnv(t *testing.T) {
	requireUserCurrent(t)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
allow_env = ["FOO_*", "BAR"]
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, []string{"FOO_*", "BAR"}, cfg.AllowEnv)
}

func TestLoad_AllowEnvMalformedPattern(t *testing.T) {
	requireUserCurrent(t)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
allow_env = ["[invalid"]
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	_, err = Load(configPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allow_env")
}

func TestFilterEnv_LiteralMatch(t *testing.T) {
	cfg := &Config{AllowEnv: []string{"DEBUG"}}
	env := map[string]string{"DEBUG": "1", "DEBUG_MODE": "full"}

	allowed, stripped := cfg.FilterEnv(env)

	assert.Equal(t, map[string]string{"DEBUG": "1"}, allowed)
	assert.Equal(t, []string{"DEBUG_MODE"}, stripped)
}

func TestFilterEnv_GlobMatch(t *testing.T) {
	cfg := &Config{AllowEnv: []string{"TTAL_*"}}
	env := map[string]string{
		"TTAL_JOB_ID":     "abc",
		"TTAL_AGENT_NAME": "coder",
		"OTHER":           "xyz",
	}

	allowed, stripped := cfg.FilterEnv(env)

	assert.Equal(t, map[string]string{"TTAL_JOB_ID": "abc", "TTAL_AGENT_NAME": "coder"}, allowed)
	assert.Equal(t, []string{"OTHER"}, stripped)
}

func TestFilterEnv_EmptyUserAllowList_BaselineStillPasses(t *testing.T) {
	cfg := &Config{AllowEnv: nil}
	env := map[string]string{"FOO": "bar", "USER": "alice"}

	allowed, stripped := cfg.FilterEnv(env)

	assert.NotContains(t, allowed, "FOO")
	assert.Contains(t, stripped, "FOO")
	assert.Equal(t, "alice", allowed["USER"])
}

func TestFilterEnv_NilEnv_ReturnsNil(t *testing.T) {
	cfg := &Config{AllowEnv: []string{"DEBUG"}}

	allowed, stripped := cfg.FilterEnv(nil)

	assert.Nil(t, allowed)
	assert.Nil(t, stripped)
}

func TestFilterEnv_StrippedSorted(t *testing.T) {
	cfg := &Config{AllowEnv: []string{"FOO"}}
	env := map[string]string{"ZEBRA": "1", "ALPHA": "2", "MIDDLE": "3"}

	_, stripped := cfg.FilterEnv(env)

	assert.Equal(t, []string{"ALPHA", "MIDDLE", "ZEBRA"}, stripped)
}
