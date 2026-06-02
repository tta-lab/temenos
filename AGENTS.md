# AGENTS.md

This file provides guidance to coding agents when working with code in this repository.

## What is Temenos

Temenos is a filesystem isolation daemon for AI agents. It sandboxes command execution using seatbelt (`sandbox-exec`) on macOS and bubblewrap (`bwrap`) on Linux. The daemon listens on a unix socket and exposes an HTTP API for running commands inside sandboxed environments with configurable filesystem allowlists.

## Build & Development Commands

```bash
make build          # build binary → ./temenos
make test           # run all tests (go test -v ./...)
make lint           # golangci-lint (v2, config in .golangci.yml)
make fmt            # gofmt -w -s
make vet            # go vet
make qlty           # run qlty check (lint + security scan)
make install-hooks  # install qlty git hooks
make ci             # fmt + tidy + qlty + test + build
make install        # go install ./cmd/temenos
go test -v -run TestFoo ./sandbox/  # run a single test
```

## Git Hooks (qlty)

- **Pre-commit:** `qlty fmt` — auto-formats staged Go files (gofmt + goimports)
- **Pre-push:** `qlty check` — runs golangci-lint + trufflehog + osv-scanner + zizmor
- **Install:** `make install-hooks` or `qlty githooks install`

Qlty config: `.qlty/qlty.toml`

## Architecture

**Daemon** (`internal/daemon/`) — Single-listener design:
- **Admin server** — HTTP on unix socket (`~/.temenos/daemon.sock`, override via `TEMENOS_SOCKET_PATH`). Admin socket has 0o600 filesystem permissions.

Admin endpoints:
- `POST /run` — execute a command in the sandbox with specified allowed paths, env vars, timeout
- `GET /health` — platform/version info
- `GET /jobs` — list background jobs (optional `?status=` and `?caller_id=` filters)
- `GET /jobs/{id}` — get background job status and output
- `DELETE /jobs/{id}` — kill a running background job

**Sandbox** (`sandbox/`) — Platform-dispatched via `sandbox.New(Options)` → `Sandbox` interface:
- `SeatbeltSandbox` (macOS) — uses `/usr/bin/sandbox-exec` with embedded `.sbpl` policy templates. Seatbelt cannot remap paths (Source must equal Target on mounts).
- `BwrapSandbox` (Linux) — uses bubblewrap with namespace isolation, deny-default with explicit bind mounts.
- `NoopSandbox` — fallback when `AllowUnsandboxed: true`.
- `UnavailableSandbox` — always errors, used when no sandbox available and unsandboxed not allowed.

Seatbelt policies are embedded via `//go:embed` from three `.sbpl` files in `sandbox/`: base, network, platform. Dynamic mounts are appended as parameterized rules.

**Client** (`client/`) — Go client library for the daemon. Mirrors the daemon types (`RunRequest`, `RunResponse`, `AllowedPath`).

**CLI** (`internal/cli/`) — Cobra command tree. Entry point: `cmd/temenos/main.go`. Top-level commands: `daemon {start,stop,restart,install,uninstall,status}`, `doctor` (runtime diagnostics with per-check remediation).

## Key Design Decisions

- Daemon creates a fresh temp HOME dir per seatbelt execution and cleans it up after
- Output truncated at 64KB per stream
- Request body capped at 1 MiB
- Default `/run` execution timeout: 20 minutes
- Version injected via `-ldflags` at build time (`cli.Version`)
- `gocyclo` max complexity: 15, line length limit: 120
- RunRequest.Env filters through Config.EffectiveAllowEnv() (BaselineAllowEnv + user allow_env, deduped, filepath.Match globs); baseline is unconditional, user config extends — never replaces — it
