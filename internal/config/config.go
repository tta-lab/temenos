package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"

	"github.com/tta-lab/temenos/sandbox"

	"github.com/BurntSushi/toml"
)

// Config holds the temenos daemon configuration.
type Config struct {
	AllowRead  []string `toml:"allow_read"`
	AllowWrite []string `toml:"allow_write"`
	AllowEnv   []string `toml:"allow_env"`
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

// defaultConfig returns a Config with sensible defaults for when no config file exists.
func defaultConfig() (*Config, error) {
	socketPath, err := ExpandHome("~/.temenos/daemon.sock")
	if err != nil {
		return nil, err
	}
	return &Config{
		MCPPort:    9783,
		AllowWrite: nil,
		SocketPath: socketPath,
	}, nil
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
		return defaultConfig()
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
	} else if cfg.MCPPort < 1 || cfg.MCPPort > 65535 {
		return nil, fmt.Errorf("mcp_port %d is out of range (1-65535)", cfg.MCPPort)
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath, err = ExpandHome("~/.temenos/daemon.sock")
		if err != nil {
			return nil, err
		}
	}

	// Expand ~ in AllowRead, AllowWrite, and SocketPath
	cfg.AllowRead, err = expandSlice(cfg.AllowRead)
	if err != nil {
		return nil, fmt.Errorf("allow_read: %w", err)
	}
	cfg.AllowWrite, err = expandSlice(cfg.AllowWrite)
	if err != nil {
		return nil, fmt.Errorf("allow_write: %w", err)
	}
	cfg.SocketPath, err = ExpandHome(cfg.SocketPath)
	if err != nil {
		return nil, err
	}

	// Validate operator-supplied allow_env patterns only. BaselineAllowEnv is
	// known-good by construction (patterns are reviewed and frozen at source).
	if err := validateAllowEnv(cfg.AllowEnv); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validateAllowEnv checks that each pattern in allowEnv is a valid filepath.Match
// pattern. Returns an error describing the first malformed pattern.
func validateAllowEnv(allowEnv []string) error {
	for _, pattern := range allowEnv {
		if _, err := filepath.Match(pattern, ""); err != nil {
			return fmt.Errorf("allow_env: malformed pattern %q: %w", pattern, err)
		}
	}
	return nil
}

// expandSlice expands ~ in each element of the slice.
// Returns an error if any path cannot be expanded.
func expandSlice(paths []string) ([]string, error) {
	result := make([]string, len(paths))
	for i, p := range paths {
		expanded, err := ExpandHome(p)
		if err != nil {
			return nil, fmt.Errorf("expand path %q: %w", p, err)
		}
		result[i] = expanded
	}
	return result, nil
}

// BaselineMounts converts the config's AllowRead and AllowWrite paths
// into sandbox.Mount entries. AllowRead paths become read-only mounts;
// AllowWrite paths become read-write mounts. Returns an empty slice if
// no paths are configured.
func (c *Config) BaselineMounts() []sandbox.Mount {
	mounts := make([]sandbox.Mount, 0, len(c.AllowRead)+len(c.AllowWrite))
	for _, p := range c.AllowRead {
		mounts = append(mounts, sandbox.Mount{Source: p, Target: p, ReadOnly: true})
	}
	for _, p := range c.AllowWrite {
		mounts = append(mounts, sandbox.Mount{Source: p, Target: p, ReadOnly: false})
	}
	return mounts
}

// FilterEnv returns a filtered copy of env containing only keys that match
// at least one glob pattern in the effective allow_env (BaselineAllowEnv + c.AllowEnv,
// filepath.Match semantics, case-sensitive). Stripped keys are returned sorted
// for deterministic logging.
//
// Even if c.AllowEnv is empty, keys matching BaselineAllowEnv (USER, LANG, LC_*, HOME, …)
// still pass through. Baseline is a safety floor — there is no built-in mechanism to disable it.
// A nil c.AllowEnv and an empty []string{} both produce baseline-only behavior — the union
// with BaselineAllowEnv is identical in both cases.
//
// If env is nil/empty, returns nil, nil.
//
// Note: PATH and TERM are injected by teme's buildEnv directly — they do not pass
// through allow_env and must not be added to it (a user-supplied PATH would override
// the sandbox's curated PATH via duplicate-key precedence in os/exec).
//
// HOME is in BaselineAllowEnv, so a caller-supplied HOME passes through and overrides
// buildEnv's fallback. Tools resolving ~/.gitconfig etc. will see the real HOME — but
// sandbox filesystem policy (seatbelt/bwrap mounts) is the security boundary, not env hiding.
func (c *Config) FilterEnv(env map[string]string) (allowed map[string]string, stripped []string) {
	if len(env) == 0 {
		return nil, nil
	}

	allowed = make(map[string]string)
	patterns := c.EffectiveAllowEnv()
	for key, value := range env {
		matched := false
		for _, pattern := range patterns {
			if ok, _ := filepath.Match(pattern, key); ok {
				matched = true
				break
			}
		}
		if matched {
			allowed[key] = value
		} else {
			stripped = append(stripped, key)
		}
	}
	if len(allowed) == 0 {
		allowed = nil
	}

	sort.Strings(stripped)
	return allowed, stripped
}
