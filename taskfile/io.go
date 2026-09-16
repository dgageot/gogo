package taskfile

import (
	"io"
	"os"
	"sync"
)

type lockedWriter struct {
	mu     *sync.Mutex
	writer io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}

// outputWriter leaves files intact so exec inherits their descriptors and TTYs.
// One mutex protects both streams, which may share the same injected buffer.
func (r *Runner) outputWriter(w io.Writer) io.Writer {
	if w == nil {
		return nil
	}
	if _, ok := w.(*os.File); ok {
		return w
	}
	return &lockedWriter{mu: &r.outputMu, writer: w}
}

// outputStreams retains exec's shared pipe when stdout and stderr are identical.
func (r *Runner) outputStreams() (io.Writer, io.Writer) {
	stdout := r.outputWriter(r.IO.Stdout)
	if sameWriter(r.IO.Stdout, r.IO.Stderr) {
		return stdout, stdout
	}
	return stdout, r.outputWriter(r.IO.Stderr)
}

// Interface values may contain non-comparable fields; mirror exec's safe test.
func sameWriter(a, b io.Writer) (equal bool) {
	defer func() { _ = recover() }()
	return a == b
}

type lockedReader struct {
	mu     *sync.Mutex
	reader io.Reader
}

func (r *lockedReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reader.Read(p)
}

// inputReader preserves file descriptors; other readers may be shared by exec
// copy goroutines and prompts, so they must use the same lock.
func (r *Runner) inputReader() io.Reader {
	if r.IO.Stdin == nil {
		return nil
	}
	if _, ok := r.IO.Stdin.(*os.File); ok {
		return r.IO.Stdin
	}
	return &lockedReader{mu: &r.inputMu, reader: r.IO.Stdin}
}
