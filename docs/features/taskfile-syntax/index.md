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
| `env` | map | Environment variables (supports `op://` references for 1Password secrets) |
| `vars` | map | Task-scoped variables |
| `sources` | list | Glob patterns for incremental builds and watch mode |
| `generates` | list | Output file patterns for timestamp-based incremental builds |
| `aliases` | list | Alternative names for the task |
| `platforms` | list | Restrict task to specific OS/arch (e.g. `linux`, `darwin/arm64`) |
| `requires` | map | Required variables (`vars`) and environment variables (`env`) |

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

## Internal Tasks

Tasks whose name starts with `_` are internal — they don't appear in `gogo -l` but can still be used as dependencies or called directly:

```yaml
tasks:
  build:
    deps: [_generate]
    cmd: go build ./...

  _generate:
    cmd: go generate ./...
```

## Platforms

Restrict a task to specific operating systems or architectures:

```yaml
tasks:
  install-linux:
    platforms: [linux]
    cmd: apt-get install -y mypackage

  build-mac-arm:
    platforms: [darwin/arm64]
    cmd: make build
```

Entries can be `os` (e.g. `linux`, `darwin`), `os/arch` (e.g. `linux/amd64`), or `arch` (e.g. `arm64`). Tasks that don't match the current platform are silently skipped.

## Requires

Validate that variables or environment variables are set before running a task:

```yaml
tasks:
  deploy:
    requires:
      vars: [VERSION]
      env: [DEPLOY_TOKEN]
    cmd: deploy --version ${VERSION}
```

If a required value is missing, gogo prints a clear error and stops.

## Passing Variables to Task Calls

When calling a task from `cmds`, you can pass variables:

```yaml
tasks:
  release:
    cmds:
      - task: deploy
        vars:
          ENV: production
          VERSION: "2.0"

  deploy:
    cmd: deploy --env ${ENV} --version ${VERSION}
```

Call-site variables override the called task's own `vars`.
