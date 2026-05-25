package daemon

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tta-lab/temenos/sandbox"
)

const (
	maxBackgroundJobs     = 100
	completedJobRetention = 5 * time.Minute
	maxOutputBytes        = 64 * 1024 // 64KB, consistent with sandbox truncation
)

// JobStatus represents the lifecycle state of a background job.
type JobStatus string

const (
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusKilled    JobStatus = "killed"
)

// BackgroundJob tracks a detached command execution.
type BackgroundJob struct {
	mu sync.Mutex

	// immutable after creation
	id        string
	callerID  string
	command   string
	stdout    *syncBuffer
	stderr    *syncBuffer
	cancel    context.CancelFunc
	done      chan struct{}
	startedAt time.Time

	// mutable during execution — protected by mu
	status         JobStatus
	exitCode       int
	completedAt    time.Time
	lastAccessedAt time.Time
}

// JobInfo is the serializable view of a BackgroundJob.
type JobInfo struct {
	ID          string `json:"id"`
	CallerID    string `json:"caller_id,omitempty"`
	Command     string `json:"command"`
	Status      string `json:"status"`
	ExitCode    int    `json:"exit_code,omitempty"`
	Stdout      string `json:"stdout,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at,omitempty"`
}

// syncBuffer is a thread-safe bytes.Buffer.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// BackgroundJobManager manages background command executions.
type BackgroundJobManager struct {
	mu   sync.RWMutex
	jobs map[string]*BackgroundJob
	seq  uint64
}

// NewBackgroundJobManager creates a new job manager.
func NewBackgroundJobManager() *BackgroundJobManager {
	return &BackgroundJobManager{
		jobs: make(map[string]*BackgroundJob),
	}
}

// newBackgroundJob creates and starts a background job without registering it.
// The caller is responsible for calling Add() if the job needs to be tracked.
func newBackgroundJob(
	ctx context.Context,
	callerID, command string,
	sbx sandbox.Sandbox,
	cfg *sandbox.ExecConfig,
) *BackgroundJob {
	jobCtx, cancel := context.WithCancel(ctx)
	job := &BackgroundJob{
		callerID:  callerID,
		command:   command,
		status:    JobStatusRunning,
		stdout:    &syncBuffer{},
		stderr:    &syncBuffer{},
		startedAt: time.Now(),
		cancel:    cancel,
		done:      make(chan struct{}),
	}

	go func() {
		defer close(job.done)
		stdout, stderr, exitCode, err := sbx.Exec(jobCtx, command, cfg)
		if err != nil {
			_, _ = job.stderr.Write([]byte(err.Error() + "\n"))
		}
		_, _ = job.stdout.Write([]byte(stdout))
		_, _ = job.stderr.Write([]byte(stderr))
		if jobCtx.Err() != nil {
			job.finish(JobStatusKilled, exitCode)
		} else {
			job.finish(JobStatusCompleted, exitCode)
		}
	}()

	return job
}

// finish records the terminal state of a job.
func (j *BackgroundJob) finish(status JobStatus, exitCode int) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.status = status
	j.exitCode = exitCode
	j.completedAt = time.Now()
}

// snapshot returns a point-in-time JobInfo. When withOutput is true and the
// job is done, it includes stdout/stderr and marks the job as accessed
// (starting the GC retention countdown).
func (j *BackgroundJob) snapshot(withOutput bool) JobInfo {
	j.mu.Lock()
	defer j.mu.Unlock()

	info := JobInfo{
		ID:        j.id,
		CallerID:  j.callerID,
		Command:   j.command,
		Status:    string(j.status),
		ExitCode:  j.exitCode,
		StartedAt: j.startedAt.Format(time.RFC3339),
	}
	if !j.completedAt.IsZero() {
		info.CompletedAt = j.completedAt.Format(time.RFC3339)
	}
	if withOutput && j.status != JobStatusRunning {
		info.Stdout = truncate(j.stdout.String())
		info.Stderr = truncate(j.stderr.String())
		if j.lastAccessedAt.IsZero() {
			j.lastAccessedAt = time.Now()
		}
	}
	return info
}

// isExpired reports whether a completed job should be garbage-collected.
func (j *BackgroundJob) isExpired(now time.Time) bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.status == JobStatusRunning {
		return false
	}
	if j.lastAccessedAt.IsZero() {
		return false
	}
	return now.Sub(j.lastAccessedAt) >= completedJobRetention
}

// Add registers an already-running job in the registry.
// Returns an error if the max concurrent limit is reached.
func (m *BackgroundJobManager) Add(job *BackgroundJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gcLocked()

	if len(m.jobs) >= maxBackgroundJobs {
		return fmt.Errorf("maximum number of background jobs (%d) reached", maxBackgroundJobs)
	}

	m.seq++
	job.id = fmt.Sprintf("%06x", m.seq)
	m.jobs[job.id] = job
	return nil
}

// Get returns a job by ID, or nil if not found.
func (m *BackgroundJobManager) Get(id string) *BackgroundJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}

// Remove deletes a job from the registry.
func (m *BackgroundJobManager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, id)
}

// List returns jobs matching the filter. Empty status or callerID means "all".
func (m *BackgroundJobManager) List(status JobStatus, callerID string) []JobInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []JobInfo
	for _, j := range m.jobs {
		info := j.snapshot(false)
		if status != "" && info.Status != string(status) {
			continue
		}
		if callerID != "" && info.CallerID != callerID {
			continue
		}
		result = append(result, info)
	}
	return result
}

// Kill cancels a running job. Returns false if not found or already done.
func (m *BackgroundJobManager) Kill(id string) bool {
	m.mu.RLock()
	j, ok := m.jobs[id]
	m.mu.RUnlock()
	if !ok {
		return false
	}

	select {
	case <-j.done:
		return false
	default:
		j.cancel()
		return true
	}
}

// gcLocked removes completed jobs that have been accessed and are older than
// the retention period.
func (m *BackgroundJobManager) gcLocked() {
	now := time.Now()
	for id, j := range m.jobs {
		if j.isExpired(now) {
			delete(m.jobs, id)
		}
	}
}

// IsDone returns true if the job has finished.
func (j *BackgroundJob) IsDone() bool {
	select {
	case <-j.done:
		return true
	default:
		return false
	}
}

// Wait blocks until the job finishes.
func (j *BackgroundJob) Wait() {
	<-j.done
}

func truncate(s string) string {
	if len(s) <= maxOutputBytes {
		return s
	}
	return s[:maxOutputBytes] + "\n...(truncated)"
}
