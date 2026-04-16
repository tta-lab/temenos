//go:build darwin

package sandbox

// BwrapSandbox is unused on non-Linux platforms. Defined here so platform.go
// can reference it without build-tagging the entire file.
type BwrapSandbox struct{}
