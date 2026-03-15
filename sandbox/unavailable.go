package sandbox

import (
	"context"
	"fmt"
)

// UnavailableSandbox always returns an error from Exec.
// Used when no platform sandbox is available and AllowUnsandboxed is false.
type UnavailableSandbox struct {
	Platform string
}

// Exec always returns an error explaining what sandbox is needed.
func (u *UnavailableSandbox) Exec(_ context.Context, _ string, _ *ExecConfig) (string, string, int, error) {
	return "", "", -1, fmt.Errorf(
		"no sandbox available on %s — install bwrap (Linux) or check sandbox-exec (macOS), "+
			"or set AllowUnsandboxed=true for local dev",
		u.Platform,
	)
}

// IsAvailable always returns false.
func (u *UnavailableSandbox) IsAvailable() bool { return false }
