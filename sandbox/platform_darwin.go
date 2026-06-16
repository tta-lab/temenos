//go:build darwin

package sandbox

import (
	"log/slog"
	"runtime"
)

// New creates the appropriate sandbox for the current platform.
// On macOS, it returns a SeatbeltSandbox if sandbox-exec is available.
// Falls back to NoopSandbox when AllowUnsandboxed is true and no platform sandbox is found.
// Returns UnavailableSandbox when AllowUnsandboxed is false and no platform sandbox is found.
func New(opts Options) Sandbox {
	sbx := &SeatbeltSandbox{Timeout: opts.Timeout}
	if sbx.IsAvailable() {
		return sbx
	}
	if opts.AllowUnsandboxed {
		slog.Warn("sandbox: no platform sandbox available, running without isolation", "os", runtime.GOOS)
		return &NoopSandbox{Timeout: opts.Timeout}
	}
	return &UnavailableSandbox{Platform: runtime.GOOS}
}
