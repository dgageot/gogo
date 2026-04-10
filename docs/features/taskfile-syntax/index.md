---
title: Taskfile Syntax
---

# Taskfile Syntax

gogo looks for a taskfile in the current directory, trying these names in order:

1. `gogo.yaml`
2. `Taskfile.yml`
3. `Taskfile.yaml`

## Top-Level Fields

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Optional version identifier |
| `includes` | list of strings | Subdirectories containing other taskfiles |
| `dotenv` | list of strings | Paths to `.env` files to load |
| `secrets` | map | Secret references (keychain or 1Password) |
| `vars` | map | Global variables |
| `interval` | string | Default polling interval for watch mode (e.g. `500ms`) |
| `tasks` | map | Task definitions (see below) |

## Task Definition

Each task supports the following fields:

| Field | Type | Description |
|-------|------|-------------|
| `cmd` | string | A single command to run |
| `cmds` | list | Multiple commands to run in sequence |
| `deps` | list | Tasks to run before this one (concurrently) |
| `dir` | string | Working directory for the task |
| `dotenv` | list | Paths to `.env` files to load for this task |
| `env` | map | Environment variables |
| `vars` | map | Task-scoped variables |
| `sources` | list | Glob patterns for incremental builds and watch mode |
| `aliases` | list | Alternative names for the task |
| `secrets` | list | Secret names to inject as environment variables |

## Commands

A command can be a simple string:

```yaml
tasks:
  build:
    cmd: go build ./...
```

Or a list of commands:

```yaml
tasks:
  lint:
    cmds:
      - gofmt -w .
      - golangci-lint run
```

A command can also reference another task:

```yaml
tasks:
  all:
    cmds:
      - task: build
      - task: test
```

## Task Descriptions

Comments above a task key are used as the task description, shown by `gogo -l`:

```yaml
tasks:
  # Build the Go binary
  build:
    cmd: go build ./...

  # Run unit tests
  test:
    cmd: go test ./...
```

```sh
$ gogo -l
build  Build the Go binary
test   Run unit tests
```

## Aliases

Tasks can have alternative names:

```yaml
tasks:
  test:
    aliases: [t]
    cmd: go test ./...
```

```sh
gogo t    # same as gogo test
```
