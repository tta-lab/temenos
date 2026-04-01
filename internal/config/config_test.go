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
