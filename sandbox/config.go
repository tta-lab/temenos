package sandbox

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

// DefaultAutoBackgroundAfter is the default number of seconds to wait before
// moving a long-running command to a background job.
const DefaultAutoBackgroundAfter = 30

// Config holds the temenos configuration.
type Config struct {
	AllowRead           []string `toml:"allow_read"`
	AllowWrite          []string `toml:"allow_write"`
	AllowEnv            []string `toml:"allow_env"`
	AutoBackgroundAfter int      `toml:"auto_background_after"` // seconds, default: 30
	MCPPort             int      `toml:"mcp_port"`              // default: 9783
	SocketPath          string   `toml:"socket_path"`           // default: ~/.temenos/daemon.sock
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
		AutoBackgroundAfter: DefaultAutoBackgroundAfter,
		MCPPort:             9783,
		AllowWrite:          nil,
		SocketPath:          socketPath,
	}, nil
}

// LoadConfig reads the configuration from the given path and returns a
// ready-to-use Sandbox. If path is empty, DefaultConfigPath() is used.
// If the file does not exist, a default Config is used.
//
// This is the primary entry point for external consumers (e.g. Lenos) that
// want a one-call setup: load config, get sandbox with baseline mounts.
// The daemon also uses this internally.
func LoadConfig(path string) (*Config, Sandbox, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, nil, err
	}
	sbx := New(Options{
		Timeout:          DefaultTimeout,
		AllowUnsandboxed: false,
	})
	return cfg, sbx, nil
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

	if err := cfg.applyDefaults(); err != nil {
		return nil, err
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

func (c *Config) applyDefaults() error {
	if c.MCPPort == 0 {
		c.MCPPort = 9783
	} else if c.MCPPort < 1 || c.MCPPort > 65535 {
		return fmt.Errorf("mcp_port %d is out of range (1-65535)", c.MCPPort)
	}
	if c.AutoBackgroundAfter == 0 {
		c.AutoBackgroundAfter = DefaultAutoBackgroundAfter
	}
	if c.SocketPath == "" {
		socketPath, err := ExpandHome("~/.temenos/daemon.sock")
		if err != nil {
			return err
		}
		c.SocketPath = socketPath
	}
	return nil
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
func (c *Config) BaselineMounts() []Mount {
	mounts := make([]Mount, 0, len(c.AllowRead)+len(c.AllowWrite))
	for _, p := range c.AllowRead {
		mounts = append(mounts, Mount{Source: p, Target: p, ReadOnly: true})
	}
	for _, p := range c.AllowWrite {
		mounts = append(mounts, Mount{Source: p, Target: p, ReadOnly: false})
	}
	return mounts
}

// BaselineAllowEnv is the set of env-var key patterns that are universally
// safe to forward into sandboxed processes. Operators' allow_env in
// config.toml extends — does not replace — this list. The merged set is
// returned by Config.EffectiveAllowEnv().
//
// Pattern rules:
//   - filepath.Match glob semantics (case-sensitive)
//   - Literal names (USER) match exactly one key
//   - Trailing "*" (LC_*) matches a family
//
// Keys deliberately EXCLUDED from baseline (do NOT add):
//
//	PATH        Sandbox builds PATH via buildSandboxPATH() with a curated
//	            trusted set of tool dirs. Allowing user-supplied PATH lets
//	            callers override the sandbox PATH (Go exec uses the LAST
//	            value for duplicate keys) — security regression.
//
//	TERM        Sandbox hardcodes TERM=dumb in buildEnv. Allowing override
//	            breaks intentional terminal-mode behavior. If TUI is
//	            needed, operators add TERM to allow_env explicitly.
//
//	GOTELEMETRY Sandbox hardcodes GOTELEMETRY=off in buildEnv. The Go
//	            toolchain telemetry sidecar requires /proc/self/exe which
//	            is unavailable in sandboxed environments (bwrap, seatbelt).
//	            Forcing off prevents noisy startup errors.
//
//	SSH_AUTH_SOCK / SSH_AGENT_PID
//	            Exposes the host SSH agent to sandboxed processes.
//
//	HTTP_PROXY / HTTPS_PROXY / NO_PROXY
//	            Network-affecting; per-deployment concern, not universal.
//
//	XDG_*       Config paths into user dirs; risk of unexpected host-fs
//	            access.
//
//	EDITOR / VISUAL
//	            Sandbox shouldn't launch interactive editors.
//
//	HOSTNAME    Lightly identifying, no strong need.
//
//	*_TOKEN, *_SECRET, *_PASSWORD, *_KEY
//	            Sensitive — must be explicitly opted in by name.
//
// Note on TMUX / TMUX_PANE: these are in baseline. They expose the
// tmux session/pane handle that ttal CLI commands read inside worker
// sandboxes to prefix alerts with the session name, ping counterpart
// agents via tmux notification, auto-close reviewer windows on LGTM,
// and attribute pipeline advances to the caller session.
//
// Security: the tmux socket itself is protected by filesystem policy,
// not env hiding. On macOS (seatbelt), /tmp allows metadata-read only;
// on Linux (bwrap), /tmp is an isolated tmpfs — the host tmux socket
// at /tmp/tmux-* is completely invisible inside the sandbox. In both
// cases, reading TMUX is safe: the socket path cannot be used to bypass
// the sandbox. Without these keys, ttal CLI degrades gracefully but
// loses ops quality (missed review notifications, orphaned reviewer
// windows, no session context in alerts).
//
// Note on HOME: HOME is in baseline. The sandbox.buildEnv fallback only
// injects HOME when cfg.Env doesn't already set it; with HOME in baseline
// (and HOME present in the caller's env), the caller's HOME is forwarded.
// Filesystem isolation is enforced by mount policy, not by hiding HOME.
var BaselineAllowEnv = []string{
	// Identity
	"USER",
	"LOGNAME",
	// Locale & time (POSIX)
	"LANG",
	"LC_*",
	"TZ",
	// Standard paths
	"HOME",
	"PWD",
	"TMPDIR",
	// Shell
	"SHELL",
	// Terminal sizing
	"COLUMNS",
	"LINES",
	// Diagnostic / TUI
	"DEBUG",
	"CI",
	"NO_COLOR",
	"FORCE_COLOR",
	// Session identity (tmux)
	"TMUX",
	"TMUX_PANE",
}

func init() {
	for _, p := range BaselineAllowEnv {
		if p == "PATH" || p == "TERM" || p == "GOTELEMETRY" {
			panic("sandbox: BaselineAllowEnv must not contain PATH, TERM, or " +
				"GOTELEMETRY — they are injected by buildEnv; see config.go")
		}
	}
}

// EffectiveAllowEnv returns the union of BaselineAllowEnv and c.AllowEnv,
// deduplicating exact matches. Baseline patterns appear first; user
// additions follow in their original order. Used by FilterEnv to decide
// which env keys may cross into the sandbox.
func (c *Config) EffectiveAllowEnv() []string {
	out := make([]string, 0, len(BaselineAllowEnv)+len(c.AllowEnv))
	seen := make(map[string]bool, len(BaselineAllowEnv)+len(c.AllowEnv))
	for _, p := range BaselineAllowEnv {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range c.AllowEnv {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
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
