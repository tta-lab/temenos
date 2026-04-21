package config

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
		if p == "PATH" || p == "TERM" {
			panic("config: BaselineAllowEnv must not contain PATH or TERM — they are injected by buildEnv; see baseline.go")
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
