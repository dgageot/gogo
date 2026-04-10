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
| `-l`, `--list` | List available tasks (only tasks with comments) |
| `-w`, `--watch` | Watch sources and re-run on changes |
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

# Watch and re-run
gogo -w test

# Pass arguments to a task
gogo test -- -v -run TestFoo
```

## Subcommands

### `gogo secret set`

Store a secret in the macOS Keychain:

```sh
gogo secret set <service> <key> <value>
```

```sh
gogo secret set myservice api-key sk-abc123
```

## Taskfile Discovery

gogo walks up the directory tree to find the topmost directory containing a taskfile. This means you can run gogo from any subdirectory and it will find the root taskfile.

Taskfile names are tried in this order:

1. `gogo.yaml`
2. `Taskfile.yml`
3. `Taskfile.yaml`
