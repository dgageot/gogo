package taskfile

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestParallelTasksShareInjectedInputSafely(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRunner(&Config{Dir: dir, Tasks: map[string]Task{
		"all": {Deps: []Dep{{Task: "a"}, {Task: "b"}}},
		"a":   {Cmds: []Cmd{{Cmd: "cat > a.txt"}}},
		"b":   {Cmds: []Cmd{{Cmd: "cat > b.txt"}}},
	}}, dir)
	require.NoError(t, err)
	input := strings.Repeat("x", 1<<20)
	r.IO = RunnerIO{Stdin: strings.NewReader(input), Stdout: io.Discard, Stderr: io.Discard}
	require.NoError(t, r.RunContext(t.Context(), "all", ""))
	a, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	require.NoError(t, err)
	b, err := os.ReadFile(filepath.Join(dir, "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, input, string(a)+string(b))
}

func TestInputReaderSerializesConcurrentReads(t *testing.T) {
	r := &Runner{IO: RunnerIO{Stdin: strings.NewReader(strings.Repeat("x", 100000))}}
	var outputs [2][]byte
	var errs [2]error
	var wg sync.WaitGroup
	for i := range outputs {
		wg.Go(func() { outputs[i], errs[i] = io.ReadAll(r.inputReader()) })
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	assert.Equal(t, 100000, len(outputs[0])+len(outputs[1]))
	r.IO.Stdin = os.Stdin
	assert.Same(t, os.Stdin, r.inputReader())
	r.IO.Stdin = nil
	assert.Nil(t, r.inputReader())
}
