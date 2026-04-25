package taskfile

import (
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sync"
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

// Runner executes tasks from a loaded Taskfile.
type Runner struct {
	tf          *Taskfile
	cwd         string
	BaseEnv     []string          // base process environment (defaults to os.Environ() + dotenv)
	aliases     map[string]string // alias -> task name
	DryRun      bool              // if true, print commands without executing them
	Force       bool              // if true, ignore sources and generates (always run)
	ShellRunner ShellRunner       // replaceable shell executor (defaults to real exec)
	IO          RunnerIO          // process streams used for logs and command stdio
	runs        sync.Map          // resolved task name -> *taskRun
}

// taskRun memoizes a single task execution. The first caller runs the body;
// concurrent and later callers observe the same result.
type taskRun struct {
	once sync.Once
	err  error
}

// do runs fn exactly once, returning its memoized result to every caller.
func (t *taskRun) do(fn func() error) error {
	t.once.Do(func() { t.err = fn() })
	return t.err
}

// NewRunner creates a task runner for the given taskfile.
func NewRunner(tf *Taskfile, cwd string) (*Runner, error) {
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
		ShellRunner: defaultShellRunner{},
		IO:          defaultRunnerIO(),
	}
	return r, nil
}

// ResetRan clears the memoized task results, allowing tasks to run again.
// This is used by watch mode between iterations.
func (r *Runner) ResetRan() {
	r.runs = sync.Map{}
}
