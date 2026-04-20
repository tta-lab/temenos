// Package sandbox provides platform-aware sandboxed command execution.
//
// On Linux, it uses bubblewrap (bwrap) for namespace isolation.
// On macOS, it uses seatbelt (sandbox-exec) for kernel-level sandboxing.
// Both provide: deny-default filesystem access, explicit mount/path allowlists,
// network access, per-execution env vars, and timeout enforcement.
//
// Use New(Options) to get the appropriate sandbox for the current platform.
// ExecConfig carries per-execution env vars and mounts passed directly to Exec.
//
// ## Linux cgroup v2 memory limits
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
