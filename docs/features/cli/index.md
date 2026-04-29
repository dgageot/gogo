---
title: CLI Reference
---

# CLI Reference

## Usage

```
gogo [options] [task] [-- args...]
```

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
| `task` | Task to run (default: `default`) |
| `args...` | Extra arguments passed as `{{ "{{" }}.CLI_ARGS}}` (after `--`) |

## Examples

```sh
# Run the default task
gogo

# Run a specific task
gogo build

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
