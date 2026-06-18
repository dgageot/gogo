package taskfile

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sync"
	"sync/atomic"
)

// Execution records a single command that was (or would be) executed.
type Execution struct {
	Task     string
	Command  string
	Dir      string
	Env      []string
	UseOpRun bool
}

// RunnerIO contains process streams used by a Runner.
type RunnerIO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func defaultRunnerIO() RunnerIO {
	return RunnerIO{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Runner executes tasks from a loaded task file.
type Runner struct {
	tf                 *Config
	cwd                string
	BaseEnv            []string          // base process environment (defaults to os.Environ() + dotenv)
	aliases            map[string]string // alias -> task name
	DryRun             bool              // if true, print commands without executing them
	Force              bool              // if true, ignore sources and generates (always run)
	AssumeYes          bool              // if true, task prompts are auto-confirmed (--yes)
	PreferCwdNamespace bool              // if true, bare task names prefer the cwd namespace
	ShellRunner        ShellRunner       // replaceable shell executor (defaults to real exec)
	IO                 RunnerIO          // process streams used for logs and command stdio
	runs               sync.Map          // (resolved task name, CLI args) -> *taskRun
	gitVars            *gitVars          // lazy {{.GIT_*}} resolver, built on first reference
	gitOnce            sync.Once         // guards gitVars construction
	inputMu            sync.Mutex        // protects injected non-file readers
	outputMu           sync.Mutex        // protects non-file output streams
	graphMu            sync.Mutex
	waits              map[*taskRun]map[*taskRun]int // active invocation edges, including memoized waits
	promptSem          chan struct{}                 // serializes cancellable prompt interactions
}

type taskRunKey struct {
	name    string
	cliArgs string
}

// taskRun memoizes a single task execution. The first caller runs the body;
// concurrent and later callers observe the same result.
type taskRun struct {
	name    string
	depth   int // nested non-memoized calls
	started atomic.Bool
	done    chan struct{}
	err     error
}

// do runs fn once; other callers may cancel without waiting for its owner.
func (t *taskRun) do(ctx context.Context, fn func() error) error {
	if t.started.CompareAndSwap(false, true) {
		defer close(t.done)
		t.err = fn()
		return t.err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.done:
		return t.err
	}
}

// NewRunner creates a task runner for the given task file.
func NewRunner(tf *Config, cwd string) (*Runner, error) {
	// Build alias map for O(1) lookup
	aliases := make(map[string]string)
	for _, name := range slices.Sorted(maps.Keys(tf.Tasks)) {
		for _, alias := range tf.Tasks[name].Aliases {
			if existing, ok := aliases[alias]; ok {
				return nil, fmt.Errorf("alias %q is defined by both %q and %q", alias, existing, name)
			}
			aliases[alias] = name
		}
	}

	r := &Runner{
		tf:          tf,
		cwd:         cwd,
		BaseEnv:     baseEnvWithDotenv(tf.DotenvVars),
		aliases:     aliases,
		ShellRunner: newDefaultShellRunner(),
		IO:          defaultRunnerIO(),
		promptSem:   make(chan struct{}, 1),
	}
	return r, nil
}

// builtins returns the value of a gogo-provided variable (currently
// {{.HOME}} and the {{.GIT_*}} family). The gitVars resolver is constructed
// on first call so tests that swap r.ShellRunner after NewRunner still see
// git invocations routed through their fake runner — binding earlier would
// capture the real shell runner in the lazy closures.
func (r *Runner) builtins(ctx context.Context) func(string) (string, bool) {
	return func(name string) (string, bool) {
		if name == "HOME" {
			// os.UserHomeDir resolves $HOME on Unix and USERPROFILE on Windows,
			// falling back to the user database if those are unset. An error
			// resolves to the empty string — same convention as the GIT_* family.
			home, _ := os.UserHomeDir()
			return home, true
		}
		r.gitOnce.Do(func() {
			r.gitVars = newGitVars(r.tf.Dir, r.ShellRunner)
		})
		return r.gitVars.lookup(ctx, name)
	}
}

// ResetRan clears the memoized task results, allowing tasks to run again.
// This is used by watch mode between iterations. Built-in git vars are
// re-resolved too because file edits between iterations may have changed
// the working tree's dirty state or HEAD.
func (r *Runner) ResetRan() {
	r.runs = sync.Map{}
	r.gitOnce = sync.Once{}
	r.gitVars = nil
}
