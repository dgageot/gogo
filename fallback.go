package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// This file implements an opt-in convenience: when gogo can't find a
// gogo.yaml, it looks for a foreign task file (Taskfile / mise.toml /
// Makefile) in the current working directory only and shells out to the
// matching tool. It's deliberately self-contained and side-tested via the
// package-level hooks below — the rest of the codebase stays oblivious.

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

// foreignRunner describes one foreign task file format and the tool that
// runs it.
type foreignRunner struct {
	bin      string
	files    []string
	build    func(tasks, cliArgs []string) []string
	listArgs []string
}

// foreignRunners lists, in priority order, the foreign task files we know
// how to delegate to. `listArgs` is the runner's native `--list`
// invocation; leave it nil when the runner has no equivalent.
var foreignRunners = []foreignRunner{
	{
		bin:   "task",
		files: []string{"Taskfile.yml", "taskfile.yml", "Taskfile.yaml", "taskfile.yaml"},
		build: func(tasks, cliArgs []string) []string {
			return foreignArgs(nil, tasks, cliArgs)
		},
		listArgs: []string{"--list"},
	},
	{
		bin:   "mise",
		files: []string{"mise.toml"},
		build: func(tasks, cliArgs []string) []string {
			return foreignArgs([]string{"run"}, tasks, cliArgs)
		},
		listArgs: []string{"tasks", "ls"},
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

// delegateToForeign looks in the current working directory only for a
// foreign task file whose runner is on PATH, and invokes it with the argv
// returned by argvFor. Runners for which argvFor reports ok=false are
// skipped. Returns (handled=true, err) when one is invoked; a (false, nil)
// return means the caller should surface its original error.
//
// We deliberately do NOT walk up: running gogo from inside an untrusted
// checkout (or a temp dir under one) must not silently execute an
// ancestor's Taskfile/Makefile/mise.toml.
//
// "Silently ignored if not on PATH" means: if a Taskfile is found but
// `task` isn't installed, we don't try to run it — we may still pick up a
// sibling runner (e.g. mise.toml) in the same directory.
func (a *App) delegateToForeign(ctx context.Context, argvFor func(foreignRunner) ([]string, bool)) (bool, error) {
	dir, err := a.Getwd()
	if err != nil {
		return false, nil
	}

	for _, r := range foreignRunners {
		argv, ok := argvFor(r)
		if !ok {
			continue
		}
		if _, err := fallbackLookPath(r.bin); err != nil {
			continue
		}
		for _, name := range r.files {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				return true, fallbackRun(ctx, r.bin, argv, dir, a)
			}
		}
	}
	return false, nil
}

// tryForeignFallback delegates a normal run to a colocated foreign task
// file's runner.
func (a *App) tryForeignFallback(ctx context.Context, parsed *args) (bool, error) {
	return a.delegateToForeign(ctx, func(r foreignRunner) ([]string, bool) {
		return r.build(parsed.Tasks, parsed.CLIArgs), true
	})
}

// tryForeignListFallback delegates `--list` to a colocated foreign task
// file's runner. Runners without a native listing command (listArgs == nil)
// are skipped.
func (a *App) tryForeignListFallback(ctx context.Context) (bool, error) {
	return a.delegateToForeign(ctx, func(r foreignRunner) ([]string, bool) {
		return r.listArgs, r.listArgs != nil
	})
}
