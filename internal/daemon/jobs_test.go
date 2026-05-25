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

func TestBackgroundJobManager_StartAndComplete(t *testing.T) {
	mgr := NewBackgroundJobManager()
	sbx := &mockSandbox{stdout: "hello", exitCode: 0}

	job, err := mgr.Start(context.Background(), "test-caller", "echo hello", sbx, &sandbox.ExecConfig{})
	require.NoError(t, err)
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

	_, err := mgr.Start(context.Background(), "caller-a", "cmd1", sbx, &sandbox.ExecConfig{})
	require.NoError(t, err)
	job2, err := mgr.Start(context.Background(), "caller-b", "cmd2", sbx, &sandbox.ExecConfig{})
	require.NoError(t, err)

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

	job, err := mgr.Start(context.Background(), "", "sleep 999", sbx, &sandbox.ExecConfig{})
	require.NoError(t, err)

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

	job, _ := mgr.Start(context.Background(), "", "echo x", sbx, &sandbox.ExecConfig{})
	job.Wait()

	got := mgr.Get(job.ID)
	require.NotNil(t, got)
	assert.Equal(t, job.ID, got.ID)

	assert.Nil(t, mgr.Get("nonexistent"))
}

func TestBackgroundJobManager_MaxJobs(t *testing.T) {
	mgr := NewBackgroundJobManager()
	sbx := &mockSandbox{delay: 10 * time.Second}

	for i := 0; i < maxBackgroundJobs; i++ {
		_, err := mgr.Start(context.Background(), "", "sleep", sbx, &sandbox.ExecConfig{})
		require.NoError(t, err)
	}

	_, err := mgr.Start(context.Background(), "", "one more", sbx, &sandbox.ExecConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "maximum")
}

func TestBackgroundJobManager_GetOutputWhileRunning(t *testing.T) {
	mgr := NewBackgroundJobManager()
	sbx := &mockSandbox{delay: 500 * time.Millisecond, stdout: "partial"}

	job, _ := mgr.Start(context.Background(), "", "slow", sbx, &sandbox.ExecConfig{})

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
