package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ValidTOMLConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
allow_read = ["/read/path1", "/read/path2"]
allow_write = ["/write/path1"]
mcp_port = 5000
socket_path = "/custom/daemon.sock"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, []string{"/read/path1", "/read/path2"}, cfg.AllowRead)
	assert.Equal(t, []string{"/write/path1"}, cfg.AllowWrite)
	assert.Equal(t, 5000, cfg.MCPPort)
	assert.Equal(t, "/custom/daemon.sock", cfg.SocketPath)
}

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	require.NoError(t, err)

	assert.Equal(t, 9783, cfg.MCPPort)
	assert.NotEmpty(t, cfg.SocketPath)
	assert.Contains(t, cfg.SocketPath, ".temenos/daemon.sock")

	// Verify ~ is expanded
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".temenos/daemon.sock"), cfg.SocketPath)

	// Verify no default AllowWrite
	assert.Empty(t, cfg.AllowWrite)
}

func TestLoad_EmptyPath_UsesDefaultConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
mcp_port = 6000
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	t.Setenv("TEMENOS_CONFIG_PATH", configPath)
	defer t.Setenv("TEMENOS_CONFIG_PATH", "")

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, 6000, cfg.MCPPort)
}

func TestLoad_TildeExpansion(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
allow_read = ["~/read/path1", "~/documents"]
allow_write = ["~/write/data"]
mcp_port = 7000
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
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
mcp_port = 8000
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, 8000, cfg.MCPPort)
	assert.NotEmpty(t, cfg.SocketPath)

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".temenos/daemon.sock"), cfg.SocketPath)
}

func TestExpandHome_TildePrefix(t *testing.T) {
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
	tmpDir := t.TempDir()
	expectedPath := filepath.Join(tmpDir, "custom.toml")
	t.Setenv("TEMENOS_CONFIG_PATH", expectedPath)
	defer t.Setenv("TEMENOS_CONFIG_PATH", "")

	path, err := DefaultConfigPath()
	require.NoError(t, err)
	assert.Equal(t, expectedPath, path)
}

func TestDefaultConfigPath_WithoutEnvVar(t *testing.T) {
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

func TestLoad_AllowEnvEmptyArray(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
allow_env = []
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)
	assert.NotNil(t, cfg.AllowEnv)
	assert.Empty(t, cfg.AllowEnv)

	allowed, stripped := cfg.FilterEnv(map[string]string{"FOO": "bar"})
	assert.Nil(t, allowed)
	assert.Equal(t, []string{"FOO"}, stripped)
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

func TestFilterEnv_EmptyAllowList_DenyAll(t *testing.T) {
	cfg := &Config{AllowEnv: nil}
	env := map[string]string{"FOO": "bar"}

	allowed, stripped := cfg.FilterEnv(env)

	assert.Nil(t, allowed)
	assert.Equal(t, []string{"FOO"}, stripped)
}

func TestFilterEnv_NilEnv_ReturnsNil(t *testing.T) {
	cfg := &Config{AllowEnv: []string{"DEBUG"}}

	allowed, stripped := cfg.FilterEnv(nil)

	assert.Nil(t, allowed)
	assert.Nil(t, stripped)
}

func TestFilterEnv_WildcardDoesNotMatchSecretKeys(t *testing.T) {
	cfg := &Config{AllowEnv: []string{"SAFE_*"}}
	env := map[string]string{
		"GITHUB_TOKEN":   "secret1",
		"MY_SECRET":      "secret2",
		"ADMIN_PASSWORD": "secret3",
		"SAFE_FOO":       "safe",
	}

	allowed, stripped := cfg.FilterEnv(env)

	assert.Equal(t, map[string]string{"SAFE_FOO": "safe"}, allowed)
	assert.Equal(t, []string{"ADMIN_PASSWORD", "GITHUB_TOKEN", "MY_SECRET"}, stripped)
}

func TestFilterEnv_StrippedSorted(t *testing.T) {
	cfg := &Config{AllowEnv: []string{"FOO"}}
	env := map[string]string{"ZEBRA": "1", "ALPHA": "2", "MIDDLE": "3"}

	_, stripped := cfg.FilterEnv(env)

	assert.Equal(t, []string{"ALPHA", "MIDDLE", "ZEBRA"}, stripped)
}
