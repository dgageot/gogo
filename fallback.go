package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// This file implements an opt-in convenience: when gogo can't find a
// gogo.yaml, it looks for a foreign task file (Taskfile / mise.toml) up the
// tree and shells out to the matching tool. It's deliberately self-contained
// and side-tested via the package-level hooks below — the rest of the
// codebase stays oblivious.

// fallbackLookPath / fallbackRun are overridden in tests. Production uses
// exec.LookPath and a real exec.CommandContext.
var (
	fallbackLookPath = exec.LookPath
	fallbackRun      = func(ctx context.Context, name string, argv []string, dir string, app *App) error {
		cmd := exec.CommandContext(ctx, name, argv...)
		cmd.Dir = dir
		cmd.Stdin = os.Stdin
		cmd.Stdout = app.Stdout
		cmd.Stderr = app.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}
)

// foreignRunners lists, in priority order, the foreign task files we know
// how to delegate to.
var foreignRunners = []struct {
	bin   string
	files []string
	build func(tasks, cliArgs []string) []string
}{
	{
		bin:   "task",
		files: []string{"Taskfile.yml", "taskfile.yml", "Taskfile.yaml", "taskfile.yaml"},
		build: func(tasks, cliArgs []string) []string {
			return foreignArgs(nil, tasks, cliArgs)
		},
	},
	{
		bin:   "mise",
		files: []string{"mise.toml"},
		build: func(tasks, cliArgs []string) []string {
			return foreignArgs([]string{"run"}, tasks, cliArgs)
		},
	},
	{
		// `make` doesn't understand a `--` separator, so trailing args are
		// passed as additional positional words — typically extra targets
		// or `VAR=value` overrides, which is the conventional invocation.
		bin:   "make",
		files: []string{"Makefile", "makefile", "GNUmakefile"},
		build: func(tasks, cliArgs []string) []string {
			return append(append([]string{}, tasks...), cliArgs...)
		},
	},
}

// foreignArgs assembles the argv passed to the foreign tool. Multiple task
// names are forwarded as-is; the foreign runner's own multi-task semantics
// take over from there.
func foreignArgs(prefix, tasks, cliArgs []string) []string {
	argv := append([]string{}, prefix...)
	argv = append(argv, tasks...)
	if len(cliArgs) > 0 {
		argv = append(argv, "--")
		argv = append(argv, cliArgs...)
	}
	return argv
}

// tryForeignFallback walks up from cwd looking for a foreign task file whose
// runner is on PATH. Returns (handled=true, err) when one is invoked; a
// (false, nil) return means the caller should surface the original error.
//
// "Silently ignored if not on PATH" means: if a Taskfile is found but `task`
// isn't installed, we don't try to run it — we keep walking and may pick up
// a sibling/parent runner instead.
func (a *App) tryForeignFallback(ctx context.Context, parsed *args) (bool, error) {
	// Tightly coupled to the only error message FindRootDir produces; if
	// that ever changes the test suite will catch it.
	cwd, err := a.Getwd()
	if err != nil {
		return false, nil
	}

	dir := cwd
	for {
		for _, r := range foreignRunners {
			if _, err := fallbackLookPath(r.bin); err != nil {
				continue
			}
			for _, name := range r.files {
				if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
					argv := r.build(parsed.Tasks, parsed.CLIArgs)
					return true, fallbackRun(ctx, r.bin, argv, dir, a)
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false, nil
		}
		dir = parent
	}
}
