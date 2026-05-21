---
title: CLI Reference
---

# CLI Reference

## Usage

```
gogo [options] [task...] [-- args...]
```

Multiple task names run in sequence. Each task is executed as if it were
a separate `gogo` invocation — dependencies are *not* deduplicated across
tasks, so `gogo clean install` runs `clean`, then `install` (including
`install`'s own `clean` dep, if any).

## Options

| Flag | Description |
|------|-------------|
| `-l`, `--list` | List available tasks (only tasks with descriptions) |
| `-w`, `--watch` | Watch sources and re-run on changes |
| `-f`, `--force` | Ignore `sources` / `generates` and always run the task |
| `-n`, `--dry` | Print commands without executing them |
| `--completion <shell>` | Print a shell completion script (`bash`, `zsh`, or `fish`) |
| `--help` | Show help |

## Arguments

| Argument | Description |
|----------|-------------|
| `task...` | One or more tasks to run in sequence (default: `default`) |
| `args...` | Extra arguments passed as `{{ "{{" }}.CLI_ARGS}}` to every task (after `--`) |

## Examples

```sh
# Run the default task
gogo

# Run a specific task
gogo build

# Run several tasks in sequence
gogo clean install

# Run a namespaced task
gogo backend:test

# List tasks
gogo -l

# Dry run — see what would execute
gogo -n build

# Watch and re-run
gogo -w test

# Pass arguments to a task
gogo test -- -v -run TestFoo
```

## Dry Run (`-n`, `--dry`)

`gogo -n <task>` prints every command that would run, in order, without executing it. Output looks like a normal task run minus the command's own stdout/stderr:

```
$ gogo -n build
[build] go build -trimpath -ldflags "-s -w" -o bin/gogo .
```

Dry run skips command execution only — everything that decides *whether* a command should run still happens for real. In particular:

- **Dependencies** are walked normally. A dep's `cmds:` are also dry-run, so you get the full plan.
- **`sh:` variables** (and built-in `GIT_*` vars) shell out as usual — their values are part of the command being printed.
- **`requires:`** is enforced.
- **Preconditions execute for real.** They're how a task decides whether it's safe to run; a dry-run that skipped them would print plans the task would normally refuse to attempt.
- **Sources / generates** are not consulted: an up-to-date task is still printed in dry-run so you can see what *would* run on a forced re-build.
- **Secrets:** any `op://` URI is left as a placeholder in the printed output. Because the command never executes, `op run` is not invoked and no Touch ID prompt appears.

Dry run pairs naturally with `-f` to see the full plan after a clean: `gogo -n -f build`.

## Force (`-f`, `--force`)

`gogo -f <task>` ignores `sources:` and `generates:` and runs the task even when its checksum (or output timestamps) say it's up to date. The flag propagates through dependencies — `gogo -f test` re-runs `build` even if its sources are unchanged — so it's the right hammer when you suspect the cache is stale.

`-f` does **not** affect:

- **Preconditions** — still checked; a failing precondition still aborts the task.
- **`requires:`** — still enforced.
- **Memoization within a single run** — if two parents depend on the same task, it still runs once (deduplication is per-invocation, not per-cache-entry).

Combine with `-w` to disable the up-to-date short-circuit during a watch loop, or with `-n` to dump the full plan as if from a clean state.

## Task File Discovery

gogo walks up the directory tree from the current working directory and stops at the **nearest** ancestor that contains a `gogo.yaml` file. That directory becomes the project root, so you can run gogo from any subdirectory of your project.

## Shell Completion

gogo can print completion scripts for `bash`, `zsh`, and `fish`. Source the script in your shell startup file:

### bash

```sh
# ~/.bashrc
source <(gogo --completion bash)
```

### zsh

```sh
# ~/.zshrc
source <(gogo --completion zsh)
```

Or drop the script into a directory on `$fpath` (e.g. `~/.zfunc/_gogo`).

### fish

```sh
gogo --completion fish > ~/.config/fish/completions/gogo.fish
```

Completion suggests every visible task name in the current `gogo.yaml`, including namespaced tasks like `backend:build`. Internal tasks (names starting with `_`) are excluded.
