//go:build darwin || linux

package taskfile

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// blockingPromptPipe models inherited stdin, unlike os.Pipe's pollable files.
func blockingPromptPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	var fds [2]int
	require.NoError(t, unix.Pipe(fds[:]))
	r := os.NewFile(uintptr(fds[0]), "prompt-input")
	w := os.NewFile(uintptr(fds[1]), "prompt-writer")
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	return r, w
}

func TestWatchPromptCancellationPreservesStdin(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"input.txt": "source"})
	tf := promptConfig(dir)
	task := tf.Tasks["deploy"]
	task.Sources = StringList{"*.txt"}
	tf.Tasks["deploy"] = task
	r := newTestRunner(t, tf, dir)
	stdin, writer := blockingPromptPipe(t)
	r.IO = RunnerIO{Stdin: stdin, Stdout: io.Discard, Stderr: io.Discard}
	execs := captureExecs(r)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Watch(ctx, "deploy", "", 10*time.Millisecond) }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "watch did not cancel its pending prompt")
	}
	assert.Empty(t, *execs)

	_, err := writer.WriteString("y\nremaining input")
	require.NoError(t, err)
	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, r.RunContext(ctx, "deploy", ""))
	require.Len(t, *execs, 2)
	require.NoError(t, writer.Close())
	rest, err := io.ReadAll(stdin)
	require.NoError(t, err)
	assert.Equal(t, "remaining input", string(rest))
}

func TestPromptPartialAnswerCancellation(t *testing.T) {
	stdin, writer := blockingPromptPipe(t)
	_, err := writer.WriteString("y")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	done := make(chan string, 1)
	go func() { done <- readAnswer(ctx, stdin) }()
	select {
	case answer := <-done:
		assert.Equal(t, "y", answer)
		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "partial answer blocked cancellation")
	}
}

func TestPromptTerminalCancellation(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	defer master.Close()
	defer slave.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	done := make(chan string, 1)
	go func() { done <- readAnswer(ctx, slave) }()
	select {
	case answer := <-done:
		assert.Empty(t, answer)
		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "terminal prompt blocked cancellation")
	}
}

func TestPromptFileEOFDeclines(t *testing.T) {
	stdin, writer := blockingPromptPipe(t)
	require.NoError(t, writer.Close())
	dir := t.TempDir()
	r := newTestRunner(t, promptConfig(dir), dir)
	r.IO = RunnerIO{Stdin: stdin, Stderr: io.Discard}
	require.ErrorContains(t, r.RunContext(t.Context(), "deploy", ""), "prompt declined")
}

func TestPromptNullInputDeclines(t *testing.T) {
	stdin, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer stdin.Close()
	dir := t.TempDir()
	r := newTestRunner(t, promptConfig(dir), dir)
	r.IO = RunnerIO{Stdin: stdin, Stderr: io.Discard}
	require.ErrorContains(t, r.RunContext(t.Context(), "deploy", ""), "prompt declined")
}

func TestPromptRegularFilePreservesRemainingInput(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "input")
	require.NoError(t, err)
	defer file.Close()
	_, err = file.WriteString("yes\nrest")
	require.NoError(t, err)
	_, err = file.Seek(0, io.SeekStart)
	require.NoError(t, err)
	assert.Equal(t, "yes", readAnswer(t.Context(), file))
	rest, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.Equal(t, "rest", strings.TrimSpace(string(rest)))
}
