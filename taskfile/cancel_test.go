package taskfile

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatchCancelsRunningShellAndRunsCleanup(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"input.txt": "source"})
	r, err := NewRunner(&Config{Dir: dir, Tasks: map[string]Task{
		"build": {Sources: StringList{"*.txt"}, Cmds: []Cmd{
			{Defer: "printf cleanup > cleaned"},
			{Cmd: "printf started > started; sleep 30"},
			{Cmd: "printf wrong > later"},
		}},
	}}, dir)
	require.NoError(t, err)
	r.IO = RunnerIO{Stdout: io.Discard, Stderr: io.Discard}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Watch(ctx, "build", "", minWatchInterval) }()
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(dir, "started"))
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "watch did not stop its running shell")
	}
	assert.FileExists(t, filepath.Join(dir, "cleaned"))
	assert.NoFileExists(t, filepath.Join(dir, "later"))
	assert.Empty(t, readStoredChecksum(dir, "build"))
}

func TestRunContextCancelledBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, &Config{Dir: dir, Tasks: map[string]Task{"build": {Cmds: []Cmd{{Cmd: "true"}}}}}, dir)
	execs := captureExecs(r)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, r.RunContext(ctx, "build", ""), context.Canceled)
	assert.Empty(t, *execs)
}

func TestRunContextCancelsShellPhases(t *testing.T) {
	for _, phase := range []string{"condition", "variable", "precondition", "status"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			cmd := "printf started > started; sleep 30"
			task := Task{Cmds: []Cmd{{Cmd: "printf wrong > later"}}}
			switch phase {
			case "condition":
				task.If = cmd
			case "variable":
				task.Vars = map[string]Var{"V": {Sh: cmd}}
				task.Cmds[0].Cmd += " {{.V}}"
			case "precondition":
				task.Preconditions = []Precondition{{Sh: cmd}}
			case "status":
				task.Status = StringList{cmd}
			}
			r, err := NewRunner(&Config{Dir: dir, Tasks: map[string]Task{"build": task}}, dir)
			require.NoError(t, err)
			r.IO = RunnerIO{Stdout: io.Discard, Stderr: io.Discard}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- r.RunContext(ctx, "build", "") }()
			require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(dir, "started")); return err == nil }, 5*time.Second, 10*time.Millisecond)
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(3 * time.Second):
				require.FailNow(t, "shell phase did not cancel")
			}
			assert.NoFileExists(t, filepath.Join(dir, "later"))
		})
	}
}

func TestCancelledRunDoesNotPoisonMemoizedResult(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, &Config{Dir: dir, Tasks: map[string]Task{"build": {Cmds: []Cmd{{Cmd: "true"}}}}}, dir)
	ctx, cancel := context.WithCancel(t.Context())
	r.ShellRunner = &fakeShellRunner{runFunc: func(ShellCommand) error { cancel(); return ctx.Err() }}
	require.ErrorIs(t, r.RunContext(ctx, "build", ""), context.Canceled)
	r.ShellRunner = &fakeShellRunner{}
	require.NoError(t, r.RunContext(t.Context(), "build", ""))
}

func TestMemoizedWaiterCanCancelIndependently(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, &Config{Dir: dir, Tasks: map[string]Task{"build": {Cmds: []Cmd{{Cmd: "true"}}}}}, dir)
	r.IO.Stderr = io.Discard
	started, release := make(chan struct{}), make(chan struct{})
	r.ShellRunner = &fakeShellRunner{runFunc: func(ShellCommand) error { close(started); <-release; return nil }}
	owner := make(chan error, 1)
	go func() { owner <- r.RunContext(t.Context(), "build", "") }()
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, r.RunContext(ctx, "build", ""), context.DeadlineExceeded)
	close(release)
	require.NoError(t, <-owner)
}

func TestGitLookupUsesCurrentContextAndRetriesCancellation(t *testing.T) {
	r := newTestRunner(t, &Config{}, t.TempDir())
	first, cancel := context.WithCancel(t.Context())
	r.ShellRunner = &fakeShellRunner{outputFunc: func(req ShellCommand) ([]byte, error) {
		require.NoError(t, req.Context.Err())
		if req.Context == first {
			cancel()
			return nil, first.Err()
		}
		return []byte("branch"), nil
	}}
	value, _ := r.builtins(first)("GIT_COMMIT")
	assert.Empty(t, value)
	value, _ = r.builtins(t.Context())("GIT_BRANCH")
	assert.Equal(t, "branch", value)
	value, _ = r.builtins(t.Context())("GIT_COMMIT")
	assert.Equal(t, "branch", value)
}

func TestGitLookupWaiterCanCancelIndependently(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	g := newGitVars("", &fakeShellRunner{outputFunc: func(ShellCommand) ([]byte, error) {
		close(started)
		<-release
		return []byte("commit"), nil
	}})
	done := make(chan struct{})
	go func() { _, _ = g.lookup(t.Context(), "GIT_COMMIT"); close(done) }()
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	value, known := g.lookup(ctx, "GIT_COMMIT")
	assert.True(t, known)
	assert.Empty(t, value)
	close(release)
	<-done
}
