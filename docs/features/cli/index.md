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
| `-n`, `--dry` | Print commands without executing them |
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

## Taskfile Discovery

gogo walks up the directory tree to find the topmost directory containing a `gogo.yaml` file. This means you can run gogo from any subdirectory and it will find the root taskfile.
