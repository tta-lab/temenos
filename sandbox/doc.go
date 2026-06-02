// Package sandbox provides platform-aware sandboxed command execution.
//
// On Linux, it uses bubblewrap (bwrap) for namespace isolation.
// On macOS, it uses seatbelt (sandbox-exec) for kernel-level sandboxing.
// Both provide: deny-default filesystem access, explicit mount/path allowlists,
// network access, per-execution env vars, and timeout enforcement.
//
// # Quick Start for External Consumers
//
// Use LoadConfig for a one-call setup that reads ~/.config/temenos/config.toml
// and returns a ready-to-use Sandbox with baseline mounts:
//
//	cfg, sbx, err := sandbox.LoadConfig("")
//
// For daemon use, call New directly with Options, and load config separately:
//
//	cfg, _ := sandbox.Load("")
//	sbx := sandbox.New(sandbox.Options{Timeout: sandbox.DefaultTimeout})
//
// # Sandbox Backends
//
// Use New(Options) to get the appropriate sandbox for the current platform.
// ExecConfig carries per-execution env vars and mounts passed directly to Exec.
//
// # Config & Mounts
//
// Config (from ~/.config/temenos/config.toml) provides AllowRead/AllowWrite
// paths that become baseline mounts accessible in every sandboxed execution.
// BaselineMounts() converts these to Mount entries. FilterEnv applies the
// baseline+user allow_env to a caller-provided env map.
//
// # Linux cgroup v2 memory limits
//
// When --cgroupv2-memory-limit is set on the daemon, temenos enforces memory
// limits on sandboxed execs via cgroup v2. This requires running inside a
// Kubernetes pod with cgroup v2 delegation configured (memory+pids controllers
// delegated to the container). The daemon fails fast at startup if the
// environment doesn't support it.
//
// Check diagnostics with: temenos doctor
//
// Plane: temenos
package sandbox
