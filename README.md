# temenos

Sacred boundary for AI agents. Filesystem isolation via seatbelt (macOS) and bubblewrap (Linux). YAGNI containers.

Temenos is a daemon that sandboxes command execution for AI agents. It listens on a unix socket and exposes an HTTP API for running commands inside isolated environments with configurable filesystem allowlists.

## Why not containers?

Containers are heavy. AI agents don't need network namespaces, layered filesystems, or image registries. They need one thing: **don't let the LLM `rm -rf /`**. Temenos uses the kernel's own sandboxing — seatbelt on macOS, bubblewrap on Linux — to deny-default the filesystem and allowlist only what the agent needs.

## Install

### Homebrew

```bash
brew install tta-lab/ttal/temenos
```

### From source

```bash
go install github.com/tta-lab/temenos/cmd/temenos@latest
```

### From release

Download the binary from [GitHub Releases](https://github.com/tta-lab/temenos/releases).

## Quick start

```bash
# Install and start the daemon as a launchd service (macOS)
temenos daemon install

# Check it's running
temenos daemon status

# Run a command in the sandbox (via the Go client or curl)
curl --unix-socket ~/.temenos/daemon.sock http://temenos/run \
  -X POST -H "Content-Type: application/json" \
  -d '{"command": "echo hello from the sandbox"}'
```

## Daemon management

The daemon listens on a unix socket at `~/.temenos/daemon.sock` (override with `TEMENOS_SOCKET_PATH`).

```bash
temenos daemon install     # install as launchd service + start
temenos daemon uninstall   # remove launchd service
temenos daemon start       # start via launchctl
temenos daemon stop        # stop via launchctl
temenos daemon restart     # restart via launchctl kickstart
temenos daemon status      # check if running
```

On macOS, `daemon install` writes a LaunchAgent plist to `~/Library/LaunchAgents/` with `RunAtLoad` and `KeepAlive` enabled. Logs go to `~/.temenos/temenos.{stdout,stderr}.log`.

## Configuration

Temenos is configured via `~/.config/temenos/config.toml` (override with `TEMENOS_CONFIG_PATH`). The config declares baseline filesystem and environment allow-lists for every sandboxed command.

```toml
# ~/.config/temenos/config.toml

# Filesystem paths the sandbox may READ (read-only bind mounts).
allow_read = [
  "~/Code",
  "~/.config",
]

# Filesystem paths the sandbox may WRITE.
allow_write = [
  "~/.ttal",
]

# Environment variable names allowed into the sandbox.
# A built-in baseline (USER, LANG, LC_*, HOME, PWD, SHELL, TZ, NO_COLOR, ...)
# is ALWAYS applied — entries below extend it.
# Glob wildcards use filepath.Match (e.g. "TTAL_*" matches "TTAL_JOB_ID").
allow_env = [
  "TTAL_JOB_ID",
  "TTAL_AGENT_NAME",
]

# Admin socket path (default: ~/.temenos/daemon.sock).
# socket_path = "~/.temenos/daemon.sock"

# MCP port (default: 9783, bound to 127.0.0.1 only).
# mcp_port = 9783
```

Callers cannot extend `allow_env` per-request — it is intentionally operator-only. See `docs/sandbox-security-model.md` for the full security model.

### Baseline allow_env

Temenos ships with a built-in baseline of universally-safe env keys
(identity, locale/time, standard paths, shell, terminal sizing, common
diagnostic flags, tmux session identity). Operator config in `allow_env` **extends** the baseline
— it does not replace it. The full baseline list and exclusion rationale
live in `internal/config/baseline.go`.

Keys excluded from baseline include `PATH`, `TERM` (injected by the
sandbox directly), `SSH_AUTH_SOCK`, `*_TOKEN`/`*_SECRET`/`*_PASSWORD`,
proxy vars, and `XDG_*`. Operators may still allow these explicitly if
they understand the trade-offs.

## API

### `POST /run`

Execute a command in the sandbox.

```json
{
  "command": "ls -la /project",
  "allowed_paths": [
    {"path": "/project", "read_only": true},
    {"path": "/tmp/workdir", "read_only": false}
  ],
  "env": {"FOO": "bar"},
  "network": true,
  "timeout": 30
}
```

Response:

```json
{
  "stdout": "...",
  "stderr": "...",
  "exit_code": 0
}
```

Keys in `env` not matching the effective allow_env (baseline + operator config) are silently stripped before execution. Stripped keys are logged at debug level and surfaced in `stripped_env_keys` in the response (MCP tool results include the same field in `CommandResult`).

### `GET /health`

Returns platform and version info.

## How it works

### macOS — Seatbelt

Uses `/usr/bin/sandbox-exec` with an embedded `.sbpl` deny-default policy. Each execution gets a fresh temp HOME directory that's cleaned up after. Allowed paths are injected as parameterized rules in the policy.

### Linux — Bubblewrap

Uses `bwrap` with namespace isolation (`--unshare-all`). Read-only binds for `/usr`, `/bin`, `/lib`, DNS, and TLS certs. Allowed paths are added as explicit bind mounts.

### Fallbacks

- **NoopSandbox** — passthrough when `AllowUnsandboxed: true` (for development)
- **UnavailableSandbox** — always errors when no sandbox runtime is found

## Go client

```go
import "github.com/tta-lab/temenos/client"

c, err := client.New("") // uses default socket path
resp, err := c.Run(ctx, client.RunRequest{
    Command: "echo hello",
    AllowedPaths: []client.AllowedPath{
        {Path: "/my/project", ReadOnly: true},
    },
})
fmt.Println(resp.Stdout)
```

## Included Tools

The Docker image includes [Organon](https://github.com/tta-lab/organon) binaries on PATH:

- `src` — tree-sitter symbol-aware source reading/editing
- `url` — web page fetching as markdown
- `web` — web search (Brave API / DuckDuckGo)

## Limits

- Output truncated at 64KB per stream (stdout/stderr)
- Request body capped at 1 MiB
- Default execution timeout: 120s

## Memory limits (Linux)

When `--cgroupv2-memory-limit` is set, temenos enforces per-execution memory limits via cgroup v2. This requires running inside a Kubernetes pod with cgroup v2 delegation (memory + pids controllers delegated to the container).

```bash
# Set 128 MB memory limit per sandboxed exec
temenos daemon --cgroupv2-memory-limit 128 start
```

**Requirements:**
- Kubernetes pod with cgroup v2 mounted
- Memory controller delegated to the container (default on most distros)
- No `SYS_ADMIN` capability required — delegation provides the necessary permissions
- The daemon fails fast at startup if the environment doesn't support it

**Diagnostics:** `temenos doctor` reports per-check cgroup v2 / k8s / memory-delegation status with remediation hints for any failed probe.

## Development

```bash
make build    # build binary → ./temenos
make test     # go test -v ./...
make lint     # golangci-lint (v2)
make ci       # fmt + vet + lint + test + build
```

## License

MIT
