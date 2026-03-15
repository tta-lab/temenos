package daemon

import "sync"

// ProcessTracker is scaffolding for Phase 2 (GET /ps, DELETE /kill).
// MVP: no-op. Phase 2 adds PID map and KillAll.
type ProcessTracker struct {
	mu sync.Mutex
}

// NewProcessTracker creates a new ProcessTracker.
func NewProcessTracker() *ProcessTracker {
	return &ProcessTracker{}
}

// KillAll is a no-op in MVP. Phase 2: kill all tracked processes.
func (t *ProcessTracker) KillAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Phase 2: iterate PID map and send SIGKILL.
}
