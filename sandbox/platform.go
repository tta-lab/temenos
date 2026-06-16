//go:build linux

package sandbox

import (
	"cmp"
	"log/slog"
	"runtime"
)

// New creates the appropriate sandbox for the current platform.
// On macOS, it returns a SeatbeltSandbox if sandbox-exec is available.
// On Linux, it returns a BwrapSandbox if bwrap is available.
// Falls back to NoopSandbox when AllowUnsandboxed is true and no platform sandbox is found.
// Returns UnavailableSandbox when AllowUnsandboxed is false and no platform sandbox is found.
func New(opts Options) Sandbox {
	sbx := &BwrapSandbox{
		BwrapPath:      cmp.Or(opts.BwrapPath, "bwrap"),
		Timeout:        opts.Timeout,
		MemoryLimitMB:  opts.MemoryLimitMB,
		KubernetesMode: opts.KubernetesMode,
	}
	if sbx.IsAvailable() {
		return sbx
	}
	if opts.AllowUnsandboxed {
		slog.Warn("sandbox: no platform sandbox available, running without isolation", "os", runtime.GOOS)
		return &NoopSandbox{Timeout: opts.Timeout}
	}
	return &UnavailableSandbox{Platform: runtime.GOOS}
}
