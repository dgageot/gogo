package taskfile

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParallelTasksShareInjectedOutputSafely(t *testing.T) {
	dir := t.TempDir()
	cmds := make([]Cmd, 30)
	for i := range cmds {
		cmds[i] = Cmd{Cmd: "printf out; printf err >&2"}
	}
	r, err := NewRunner(&Config{Dir: dir, Tasks: map[string]Task{
		"all": {Deps: []Dep{{Task: "a"}, {Task: "b"}}},
		"a":   {Cmds: cmds},
		"b":   {Cmds: cmds},
	}}, dir)
	require.NoError(t, err)
	var buf bytes.Buffer
	r.IO = RunnerIO{Stdout: &buf, Stderr: &buf}
	require.NoError(t, r.Run("all", ""))
	assert.Equal(t, 60, strings.Count(buf.String(), "printf out"))
	assert.Equal(t, 120, strings.Count(buf.String(), "out"))
	assert.Equal(t, 120, strings.Count(buf.String(), "err"))
}

func TestOutputWriterPreservesFileDescriptors(t *testing.T) {
	r := &Runner{}
	assert.Same(t, os.Stdout, r.outputWriter(os.Stdout))
	assert.Same(t, os.Stderr, r.outputWriter(os.Stderr))
	assert.Nil(t, r.outputWriter(nil))
}

func TestSharedOutputRetainsSinglePipe(t *testing.T) {
	var buf bytes.Buffer
	r := &Runner{IO: RunnerIO{Stdout: &buf, Stderr: &buf}}
	stdout, stderr := r.outputStreams()
	assert.Same(t, stdout, stderr)
}

type sliceWriter []byte

func (sliceWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestOutputStreamsAcceptNonComparableWriters(t *testing.T) {
	r := &Runner{IO: RunnerIO{Stdout: sliceWriter{}, Stderr: sliceWriter{}}}
	stdout, stderr := r.outputStreams()
	require.NotNil(t, stdout)
	require.NotNil(t, stderr)
}

type interfaceWriter struct{ value any }

func (interfaceWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestOutputStreamsAcceptNestedNonComparableWriters(t *testing.T) {
	w := interfaceWriter{value: []byte("x")}
	r := &Runner{IO: RunnerIO{Stdout: w, Stderr: w}}
	stdout, stderr := r.outputStreams()
	require.NotNil(t, stdout)
	require.NotNil(t, stderr)
}
