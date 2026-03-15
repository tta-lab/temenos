# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is Temenos

Temenos is a filesystem isolation daemon for AI agents. It sandboxes command execution using seatbelt (`sandbox-exec`) on macOS and bubblewrap (`bwrap`) on Linux. The daemon listens on a unix socket and exposes an HTTP API for running commands inside sandboxed environments with configurable filesystem allowlists.

## Build & Development Commands

```bash
make build          # build binary → ./temenos
make test           # run all tests (go test -v ./...)
make lint           # golangci-lint (v2, config in .golangci.yml)
make fmt            # gofmt -w -s
make vet            # go vet
make ci             # fmt + vet + lint + test + build
make install        # go install ./cmd/temenos
go test -v -run TestFoo ./sandbox/  # run a single test
```

Pre-commit hooks (lefthook): fmt check, vet, lint — run in parallel.

## Architecture

**Daemon** (`internal/daemon/`) — HTTP server on unix socket (`~/.ttal/temenos.sock`, override via `TEMENOS_SOCKET_PATH`). Two endpoints:
- `POST /run` — execute a command in the sandbox with specified allowed paths, env vars, timeout
- `GET /health` — platform/version info

**Sandbox** (`sandbox/`) — Platform-dispatched via `sandbox.New(Options)` → `Sandbox` interface:
- `SeatbeltSandbox` (macOS) — uses `/usr/bin/sandbox-exec` with embedded `.sbpl` policy templates. Seatbelt cannot remap paths (Source must equal Target on mounts).
- `BwrapSandbox` (Linux) — uses bubblewrap with namespace isolation, deny-default with explicit bind mounts.
- `NoopSandbox` — fallback when `AllowUnsandboxed: true`.
- `UnavailableSandbox` — always errors, used when no sandbox available and unsandboxed not allowed.

Seatbelt policies are embedded via `//go:embed` from three `.sbpl` files in `sandbox/`: base, network, platform. Dynamic mounts are appended as parameterized rules.

**Client** (`client/`) — Go client library for the daemon. Mirrors the daemon types (`RunRequest`, `RunResponse`, `AllowedPath`).

**Tools** (`tools/`) — Standalone utilities (file reading, URL fetching, web search) used by CLI subcommands. `CommandHelp` structs are the SSOT for help text shared between cobra and system prompt generation.

**CLI** (`internal/cli/`) — Cobra command tree. Entry point: `cmd/temenos/main.go`. Subcommands: `daemon {start,stop,restart,install,uninstall,status}`, `read-url`, `search`.

## Key Design Decisions

- Daemon creates a fresh temp HOME dir per seatbelt execution and cleans it up after
- Output truncated at 64KB per stream
- Request body capped at 1 MiB
- Default execution timeout: 30s (daemon sets 120s)
- Version injected via `-ldflags` at build time (`cli.Version`)
- `gocyclo` max complexity: 15, line length limit: 120
