package config

import (
	"os"
	"os/user"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds the temenos daemon configuration.
type Config struct {
	AllowRead  []string `toml:"allow_read"`
	AllowWrite []string `toml:"allow_write"`
	MCPPort    int      `toml:"mcp_port"`    // default: 9783
	SocketPath string   `toml:"socket_path"` // default: ~/.temenos/daemon.sock
}

// DefaultConfigPath returns the default configuration file path.
// It checks the TEMENOS_CONFIG_PATH environment variable first,
// otherwise returns ~/.config/temenos/config.toml.
func DefaultConfigPath() (string, error) {
	if path := os.Getenv("TEMENOS_CONFIG_PATH"); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "temenos", "config.toml"), nil
}

// ExpandHome replaces a leading ~ in the given path with the user's home directory.
func ExpandHome(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return path, nil
	}
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	return filepath.Join(usr.HomeDir, path[1:]), nil
}

// Load reads the configuration from the given path.
// If path is empty, DefaultConfigPath() is used.
// If the file does not exist, a default Config is returned.
// Defaults: MCPPort=9783, SocketPath=~/.temenos/daemon.sock
func Load(path string) (*Config, error) {
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return nil, err
		}
	}

	// Check if file exists
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		// Return default config, expanding ~ in socket path
		cfg := &Config{
			MCPPort: 9783,
		}
		cfg.SocketPath, err = ExpandHome("~/.temenos/daemon.sock")
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	// Parse the TOML file
	var cfg Config
	_, err = toml.DecodeFile(path, &cfg)
	if err != nil {
		return nil, err
	}

	// Apply defaults if not set
	if cfg.MCPPort == 0 {
		cfg.MCPPort = 9783
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath, err = ExpandHome("~/.temenos/daemon.sock")
		if err != nil {
			return nil, err
		}
	}

	// Expand ~ in AllowRead, AllowWrite, and SocketPath
	cfg.AllowRead = expandSlice(cfg.AllowRead)
	cfg.AllowWrite = expandSlice(cfg.AllowWrite)
	cfg.SocketPath, err = ExpandHome(cfg.SocketPath)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

// expandSlice expands ~ in each element of the slice.
func expandSlice(paths []string) []string {
	result := make([]string, len(paths))
	for i, p := range paths {
		expanded, err := ExpandHome(p)
		if err != nil {
			result[i] = p
		} else {
			result[i] = expanded
		}
	}
	return result
}
