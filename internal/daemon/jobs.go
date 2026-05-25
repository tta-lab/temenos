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
	maxBackgroundJobs     = 50
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
	ID          string
	CallerID    string
	Command     string
	Status      JobStatus
	Stdout      *syncBuffer
	Stderr      *syncBuffer
	ExitCode    int
	StartedAt   time.Time
	CompletedAt time.Time
	cancel      context.CancelFunc
	done        chan struct{}
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

// Start launches a command in the background. Returns the job immediately.
func (m *BackgroundJobManager) Start(
	ctx context.Context,
	callerID, command string,
	sbx sandbox.Sandbox,
	cfg *sandbox.ExecConfig,
) (*BackgroundJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gcLocked()

	if len(m.jobs) >= maxBackgroundJobs {
		return nil, fmt.Errorf("maximum number of background jobs (%d) reached", maxBackgroundJobs)
	}

	m.seq++
	id := fmt.Sprintf("%06x", m.seq)

	jobCtx, cancel := context.WithCancel(ctx)
	job := &BackgroundJob{
		ID:        id,
		CallerID:  callerID,
		Command:   command,
		Status:    JobStatusRunning,
		Stdout:    &syncBuffer{},
		Stderr:    &syncBuffer{},
		StartedAt: time.Now(),
		cancel:    cancel,
		done:      make(chan struct{}),
	}

	m.jobs[id] = job

	go func() {
		defer close(job.done)
		stdout, stderr, exitCode, err := sbx.Exec(jobCtx, command, cfg)
		if err != nil {
			_, _ = job.Stderr.Write([]byte(err.Error() + "\n"))
		}
		_, _ = job.Stdout.Write([]byte(stdout))
		_, _ = job.Stderr.Write([]byte(stderr))
		job.ExitCode = exitCode
		if jobCtx.Err() != nil {
			job.Status = JobStatusKilled
		} else {
			job.Status = JobStatusCompleted
		}
		job.CompletedAt = time.Now()
	}()

	return job, nil
}

// Get returns a job by ID, or nil if not found.
func (m *BackgroundJobManager) Get(id string) *BackgroundJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[id]
}

// List returns jobs matching the filter. Empty status or callerID means "all".
func (m *BackgroundJobManager) List(status JobStatus, callerID string) []JobInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []JobInfo
	for _, j := range m.jobs {
		if status != "" && j.Status != status {
			continue
		}
		if callerID != "" && j.CallerID != callerID {
			continue
		}
		result = append(result, j.toInfo(false))
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

// gcLocked removes completed jobs older than the retention period.
func (m *BackgroundJobManager) gcLocked() {
	cutoff := time.Now().Add(-completedJobRetention)
	for id, j := range m.jobs {
		if j.Status != JobStatusRunning && j.CompletedAt.Before(cutoff) {
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

// toInfo converts a BackgroundJob to a serializable JobInfo snapshot.
func (j *BackgroundJob) toInfo(withOutput bool) JobInfo {
	info := JobInfo{
		ID:        j.ID,
		CallerID:  j.CallerID,
		Command:   j.Command,
		Status:    string(j.Status),
		ExitCode:  j.ExitCode,
		StartedAt: j.StartedAt.Format(time.RFC3339),
	}
	if !j.CompletedAt.IsZero() {
		info.CompletedAt = j.CompletedAt.Format(time.RFC3339)
	}
	if withOutput {
		info.Stdout = truncate(j.Stdout.String(), maxOutputBytes)
		info.Stderr = truncate(j.Stderr.String(), maxOutputBytes)
	}
	return info
}

func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n...(truncated)"
}
