# Sandbox Security Model

## Principle: System Tools Are Trusted, User Data Is Not

Temenos sandboxes AI agent command execution. The security boundary
protects **user data** — home directories, project files, secrets,
credentials — not tool installations.

All system tool directories (Homebrew, Go, Cargo, system packages)
are readable inside the sandbox by default. The agent can run any
installed tool, but cannot access files outside explicitly allowed
paths.

## Why Allow All Tools?

The sandbox already grants `process-exec`, `process-fork`, and full
network access. Blocking tool binaries adds operational friction
(broken `node`, missing `defuddle`, invisible `temenos`) without
meaningful security benefit — an agent with network access can
already exfiltrate data through any allowed tool.

The real threat model is an AI agent reading or writing files it
shouldn't: SSH keys, environment files with secrets, other users'
project directories, etc. The deny-default filesystem policy
addresses this directly.

## What Is Allowed (Read-Only)

### Static OS Paths (in `.sbpl` templates)

Always available on the platform, never change:

| Path | Purpose |
|------|---------|
| `/bin`, `/usr/bin` | System binaries (bash, ls, grep, etc.) |
| `/usr/lib`, `/usr/share` | System libraries and shared data |
| `/etc`, `/private/etc` | System configuration |
| `/tmp`, `/private/tmp` | Temporary files (read-write) |
| `/Library/Apple`, `/System/Library` | macOS frameworks and dylibs |
| `/var/db`, `/private/var/db` | System databases (timezone, etc.) |

### Tool Installation Paths (in `paths.go`)

Discovered at daemon startup, injected dynamically into the sandbox
policy. Only directories that exist on disk are included.

| Tool | BinDir (PATH) | ReadDirs (seatbelt) | Platform |
|------|--------------|---------------------|----------|
| Apple Silicon Homebrew | `/opt/homebrew/bin` | `/opt/homebrew` | macOS |
| Intel Homebrew | `/usr/local/bin` | `/usr/local` | macOS |
| Linuxbrew | `/home/linuxbrew/.linuxbrew/bin` | `/home/linuxbrew/.linuxbrew` | Linux |
| Snap | `/snap/bin` | `/snap` | Linux |
| Go (GOPATH) | `$GOPATH/bin` or `~/go/bin` | same | both |
| Cargo (Rust) | `~/.cargo/bin` | same | both |

The full list is defined in `sandbox/paths.go` as Go slices. To add
a new tool directory, add a `ToolDir` entry to `staticToolDirs()` or
`dynamicToolDirs()`.

## What Is Blocked

Everything not explicitly listed above, including:

- **`$HOME`** — The user's home directory is not readable. Only
  specific subdirs (`~/.cargo/bin`, `~/go/bin`) are allowed.
- **Project directories** — Must be passed as `allowed_paths` in the
  `/run` request.
- **Other users' directories** — No access by default.
- **Writable access** — Tool directories are read-only. Only `/tmp`
  and explicitly requested writable `allowed_paths` are writable.

## Architecture

```
┌──────────────────────────────────────────────────┐
│ paths.go — SSOT for tool directories             │
│   staticToolDirs() → platform-specific paths     │
│   dynamicToolDirs() → GOPATH, cargo, etc.        │
│   allToolDirs()     → filtered to existing dirs  │
│   buildSandboxPATH() → constructs PATH string    │
├──────────────────────────────────────────────────┤
│ Consumers                                        │
│   buildEnv()    → PATH env var (all platforms)   │
│   buildPolicy() → seatbelt rules (macOS)         │
│   buildArgs()   → bwrap --ro-bind flags (Linux)  │
├──────────────────────────────────────────────────┤
│ Static policies (.sbpl files, macOS only)         │
│   seatbelt_base.sbpl     → kernel, IPC, devices  │
│   seatbelt_platform.sbpl → OS filesystem paths   │
│   seatbelt_network.sbpl  → network, TLS, DNS     │
└──────────────────────────────────────────────────┘
```

## Adding a New Tool Directory

1. Add a `ToolDir` entry in `sandbox/paths.go`:
   - Static paths → `darwinToolDirs()` or `linuxToolDirs()`
   - Dynamic paths (env-dependent) → `dynamicToolDirs()`
2. Run `make test` — the path is automatically picked up by
   `buildEnv`, `buildPolicy`, and `buildArgs`.
3. No `.sbpl` changes needed. Tool paths are injected dynamically.

## Design Decisions

### Why not inherit the daemon's PATH?

The daemon's PATH may include user-specific directories (`.local/bin`,
editor paths) that shouldn't be exposed. The curated list ensures
only well-known tool installations are available.

### Why filter to existing directories?

Avoids polluting PATH with non-existent entries and prevents bwrap
from failing on `--ro-bind` for missing directories.

### Why separate static and dynamic paths?

Static paths are known at compile time and don't change between
installations. Dynamic paths depend on the daemon's environment
(GOPATH, HOME) and are resolved at runtime.

### Why allow the full Homebrew tree, not just bin?

Homebrew symlinks binaries from `/opt/homebrew/bin` into
`/opt/homebrew/Cellar`. Tools also load shared libraries from
`/opt/homebrew/lib` and `/opt/homebrew/opt`. Allowing only `bin`
breaks dynamically linked tools like Node.js.
