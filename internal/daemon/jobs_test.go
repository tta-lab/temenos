package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/temenos/sandbox"
)

// mockSandbox returns fixed output and records context cancellation.
type mockSandbox struct {
	stdout   string
	stderr   string
	exitCode int
	delay    time.Duration
}

func (m *mockSandbox) Exec(ctx context.Context, _ string, _ *sandbox.ExecConfig) (string, string, int, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return "", "", -1, ctx.Err()
		}
	}
	return m.stdout, m.stderr, m.exitCode, nil
}

func (m *mockSandbox) IsAvailable() bool { return true }

// startAndRegister is a test helper that creates a job and registers it.
func startAndRegister(t *testing.T, mgr *BackgroundJobManager, sbx *mockSandbox, callerID, command string) *BackgroundJob {
	t.Helper()
	job := newBackgroundJob(context.Background(), callerID, command, sbx, &sandbox.ExecConfig{})
	require.NoError(t, mgr.Add(job))
	return job
}

func TestBackgroundJobManager_AddAndComplete(t *testing.T) {
	mgr := NewBackgroundJobManager()
	sbx := &mockSandbox{stdout: "hello", exitCode: 0}

	job := startAndRegister(t, mgr, sbx, "test-caller", "echo hello")
	assert.NotEmpty(t, job.ID)
	assert.Equal(t, JobStatusRunning, job.Status)

	job.Wait()

	assert.Equal(t, JobStatusCompleted, job.Status)
	assert.Equal(t, 0, job.ExitCode)
	assert.Equal(t, "hello", job.Stdout.String())
	assert.False(t, job.CompletedAt.IsZero())
}

func TestBackgroundJobManager_ListFilter(t *testing.T) {
	mgr := NewBackgroundJobManager()
	sbx := &mockSandbox{stdout: "done"}

	startAndRegister(t, mgr, sbx, "caller-a", "cmd1")
	job2 := startAndRegister(t, mgr, sbx, "caller-b", "cmd2")

	job2.Wait()

	all := mgr.List("", "")
	assert.Len(t, all, 2)

	running := mgr.List(JobStatusRunning, "")
	assert.Len(t, running, 1)

	byCaller := mgr.List("", "caller-a")
	assert.Len(t, byCaller, 1)
	assert.Equal(t, "caller-a", byCaller[0].CallerID)
}

func TestBackgroundJobManager_Kill(t *testing.T) {
	mgr := NewBackgroundJobManager()
	sbx := &mockSandbox{delay: 10 * time.Second}

	job := startAndRegister(t, mgr, sbx, "", "sleep 999")

	ok := mgr.Kill(job.ID)
	assert.True(t, ok)

	job.Wait()
	assert.Equal(t, JobStatusKilled, job.Status)

	// Kill again should return false (already done).
	assert.False(t, mgr.Kill(job.ID))
}

func TestBackgroundJobManager_Get(t *testing.T) {
	mgr := NewBackgroundJobManager()
	sbx := &mockSandbox{stdout: "x"}

	job := startAndRegister(t, mgr, sbx, "", "echo x")
	job.Wait()

	got := mgr.Get(job.ID)
	require.NotNil(t, got)
	assert.Equal(t, job.ID, got.ID)

	assert.Nil(t, mgr.Get("nonexistent"))
}

func TestBackgroundJobManager_Remove(t *testing.T) {
	mgr := NewBackgroundJobManager()
	sbx := &mockSandbox{stdout: "x"}

	job := startAndRegister(t, mgr, sbx, "", "echo x")
	job.Wait()

	assert.Len(t, mgr.List("", ""), 1)
	mgr.Remove(job.ID)
	assert.Len(t, mgr.List("", ""), 0)
	assert.Nil(t, mgr.Get(job.ID))
}

func TestBackgroundJobManager_MaxJobs(t *testing.T) {
	mgr := NewBackgroundJobManager()
	sbx := &mockSandbox{delay: 10 * time.Second}

	for i := 0; i < maxBackgroundJobs; i++ {
		startAndRegister(t, mgr, sbx, "", "sleep")
	}

	job := newBackgroundJob(context.Background(), "", "one more", sbx, &sandbox.ExecConfig{})
	err := mgr.Add(job)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "maximum")
}

func TestBackgroundJobManager_GetOutputWhileRunning(t *testing.T) {
	mgr := NewBackgroundJobManager()
	sbx := &mockSandbox{delay: 500 * time.Millisecond, stdout: "partial"}

	job := startAndRegister(t, mgr, sbx, "", "slow")

	// Should not be done yet.
	assert.False(t, job.IsDone())

	job.Wait()
	assert.True(t, job.IsDone())
	assert.Equal(t, "partial", job.toInfo(true).Stdout)
}

func TestSyncBuffer_ThreadSafety(t *testing.T) {
	var buf syncBuffer
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			_, _ = buf.Write([]byte("a"))
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		_, _ = buf.Write([]byte("b"))
	}
	<-done
	assert.Len(t, buf.String(), 2000)
}
