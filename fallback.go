package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/dgageot/gogo/taskfile"
)

// fallbackRunner describes an alternative task runner that gogo shells out
// to when no gogo.yaml is found anywhere up the directory tree. Each runner
// is recognised by the presence of one of `files` and invoked as `bin` with
// arguments built by `argsFor`.
type fallbackRunner struct {
	bin     string
	files   []string
	argsFor func(task string, cliArgs []string) []string
}

// fallbackRunners is the ordered list of runners gogo recognises in the
// absence of a gogo.yaml. Order matters: a directory that contains both a
// Taskfile and a mise.toml is handled by `task`.
var fallbackRunners = []fallbackRunner{
	{
		bin:     "task",
		files:   []string{"Taskfile.yml", "taskfile.yml", "Taskfile.yaml", "taskfile.yaml"},
		argsFor: taskArgs,
	},
	{
		bin:     "mise",
		files:   []string{"mise.toml"},
		argsFor: miseArgs,
	},
}

// taskArgs builds the argv for the `task` CLI. The arg-parser default of
// "default" is dropped so `gogo` (no positional) maps to `task` (no
// positional), letting the underlying runner pick its own default task.
func taskArgs(task string, cliArgs []string) []string {
	var args []string
	if task != "" && task != "default" {
		args = append(args, task)
	}
	if len(cliArgs) > 0 {
		args = append(args, "--")
		args = append(args, cliArgs...)
	}
	return args
}

// miseArgs builds the argv for `mise run`. The "default" task name is dropped
// for the same reason as in taskArgs.
func miseArgs(task string, cliArgs []string) []string {
	args := []string{"run"}
	if task != "" && task != "default" {
		args = append(args, task)
	}
	if len(cliArgs) > 0 {
		args = append(args, "--")
		args = append(args, cliArgs...)
	}
	return args
}

// fallbackCandidate pairs a found task file with the runner that should
// handle it.
type fallbackCandidate struct {
	runner fallbackRunner
	dir    string
}

// findFallback walks up from cwd looking for the first directory that
// contains a file recognised by any registered fallback runner whose binary
// is on PATH. A matching file whose runner is not installed is treated as
// absent — "silently ignore" — so a sibling fallback (or a parent dir
// closer to the user) can still apply.
func findFallback(cwd string, lookPath func(string) (string, error)) (fallbackCandidate, bool) {
	dir := cwd
	for {
		for _, r := range fallbackRunners {
			if _, err := lookPath(r.bin); err != nil {
				continue
			}
			for _, name := range r.files {
				if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
					return fallbackCandidate{runner: r, dir: dir}, true
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fallbackCandidate{}, false
		}
		dir = parent
	}
}

// tryFallback is invoked when loadConfig fails because no gogo.yaml exists.
// It returns (handled=true, err) when a fallback ran; (handled=false, nil)
// means no usable fallback was found and the caller should surface the
// original ErrNoTaskFile.
func (a *App) tryFallback(ctx context.Context, parsed *args) (bool, error) {
	cwd, err := a.Getwd()
	if err != nil {
		return false, err
	}

	candidate, ok := findFallback(cwd, a.lookPath())
	if !ok {
		return false, nil
	}

	argv := candidate.runner.argsFor(parsed.Task, parsed.CLIArgs)
	return true, a.runCommand()(ctx, candidate.runner.bin, argv, candidate.dir, a.Stdout, a.Stderr)
}

// lookPath returns the configured LookPath, falling back to exec.LookPath.
// Tests inject a fake to drive the "binary not installed" branch without
// touching the host PATH.
func (a *App) lookPath() func(string) (string, error) {
	if a.LookPath != nil {
		return a.LookPath
	}
	return exec.LookPath
}

// runCommand returns the configured RunCommand, falling back to a real
// exec.CommandContext that inherits stdin and writes to the App's streams.
func (a *App) runCommand() func(context.Context, string, []string, string, io.Writer, io.Writer) error {
	if a.RunCommand != nil {
		return a.RunCommand
	}
	return defaultRunCommand
}

// defaultRunCommand is the production implementation: it shells out for real
// and propagates the child's exit code as a Go error. Stdin is inherited so
// interactive runners (e.g. `task` prompts) keep working.
func defaultRunCommand(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// isNoTaskFile reports whether err is the sentinel returned by FindRootDir
// when nothing is found while walking up from cwd.
func isNoTaskFile(err error) bool {
	return errors.Is(err, taskfile.ErrNoTaskFile)
}
