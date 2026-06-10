---
title: Task File Syntax
---

# Task File Syntax

gogo reads task definitions from a `gogo.yaml` file in the current directory.

## Top-Level Fields

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Optional version identifier |
| `default` | string | Task to run when none is given on the CLI — replaces the convention of declaring a `default` task. Must reference a defined (possibly namespaced) task |
| `includes` | list of strings | Subdirectories containing other task files (namespaced — see [Includes](../includes/)) |
| `flatten` | list of strings | YAML files whose tasks merge into the current namespace without a prefix (see [Includes](../includes/#flatten)) |
| `dotenv` | list of strings | Paths to `.env` files to load |
| `vars` | map | Global variables |
| `sources` | map | Named source-pattern presets, referenced by task `sources:` (see [Sources & Checksums](../sources-checksums/#presets)) |
| `secrets` | map | Named secret URIs (currently `op://`), referenced by task `secrets:` (see [Secrets](../secrets/)) |
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
| `secrets` | list or string | Names referencing entries in the top-level `secrets:` map (see [Secrets](../secrets/)) |
| `sources` | list or string | Glob patterns for incremental builds and watch mode. A bare name resolves to a [source preset](../sources-checksums/#presets) (e.g. `sources: go`) |
| `generates` | list | Output file patterns for timestamp-based incremental builds |
| `aliases` | list | Alternative names for the task |
| `platforms` | list | Restrict task to specific OS/arch (e.g. `linux`, `darwin/arm64`) |
| `requires` | map | Required variables (`vars`) and environment variables (`env`) |
| `preconditions` | list | Shell commands that must succeed before the task runs |
| `silent` | bool | When `true`, suppress the `[task] cmd` log line for each command |

## Default Task

A top-level `default:` field names the task that runs when no task is given on the command line:

```yaml
default: dev

tasks:
  dev:
    deps: [build, test]
  build:
    cmd: go build ./...
  test:
    cmd: go test ./...
```

```sh
gogo            # runs `dev`
gogo build      # explicit task still wins over `default:`
```

If `default:` is unset, gogo falls back to running a task literally named `default` (the original convention). The two forms are interchangeable; `default:` simply removes the no-op trampoline:

```yaml
# Before — trampoline
tasks:
  default:
    cmds:
      - task: dev
  dev: { cmd: ... }

# After — top-level field
default: dev
tasks:
  dev: { cmd: ... }
```

A `default:` that names a non-existent task is rejected at load time.

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

A command can also reference another task. The sub-task **inherits the calling task's environment** (its `vars`, dotenv, and `env` block), so a parent's environment flows down without having to re-export it:

```yaml
tasks:
  smoke:
    env:
      TESTSET_SIZE: "2"
      MIRROR_FS: "1"
    cmds:
      - task: gen      # both children see TESTSET_SIZE and MIRROR_FS
      - task: ingest
```

The child's own `env` block (or a per-call `vars:` override) wins per key. See [Variables](../variables/#parent-to-child-env-propagation) for the full precedence rules.

## Deferred Cleanup

A command entry can use `defer:` instead of `cmd:` to register a cleanup command. Deferred commands run after the task's other commands finish — **even when one of them fails** — making them the right place for teardown:

```yaml
tasks:
  test:
    cmds:
      - docker compose up -d
      - defer: docker compose down
      - go test ./...
```

Here `docker compose down` runs whether `go test` passes or fails.

Deferred commands follow Go's `defer` semantics:

- They run in **reverse registration order** (last registered, first run).
- Only defers registered **before** a failure run — a `defer:` listed after a failing command never registered, so it is skipped.
- A deferred command's own failure is logged as a warning but does **not** fail the task: cleanup must not mask the task's real result, and later defers still run.
- Variables (`{{ "{{" }}.VAR}}`) are expanded in deferred commands like in regular ones.
- Under `--dry`, deferred commands are printed but not executed.

`defer:` only accepts a shell command string — `defer: { task: cleanup }` is not supported.

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

## Silent Tasks

By default, gogo prints each command before running it (e.g. `[build] go build ./...`). Set `silent: true` to suppress that log line — only the command's own output is shown:

```yaml
tasks:
  setup:
    silent: true
    cmds:
      - mkdir -p build
      - touch build/.keep
```

`silent` only affects the calling task's own `cmds`. A non-silent task invoked via `task:` from a silent caller still logs normally.

## Field Shorthands

Four fields accept a string *or* a struct, and the long forms are interchangeable. Use whichever reads best at the call site — the parser normalizes both into the same internal shape (see the `UnmarshalYAML` methods on `Cmd`, `Dep`, `Var`, and `Precondition` in `taskfile/types.go`).

| Field | Short form | Long form |
|-------|------------|-----------|
| `cmd` / `cmds[]` | `cmd: go build ./...` | `cmds: [{ cmd: go build ./... }]` or `cmds: [{ task: build }]` (sub-task call, optionally with `vars:`) |
| `deps[]` | `deps: [build]` | `deps: [{ task: build }]` |
| `vars` value | `VERSION: 1.0.0` | `VERSION: { value: "1.0.0" }` or `VERSION: { sh: git describe --tags }` |
| `preconditions[]` | `- test -f config.yaml` | `- { sh: test -f config.yaml, msg: config.yaml is missing }` |

A few non-obvious notes that fall out of the shorthand rules:

- `cmd:` (singular) and `cmds:` (list) are aliases, not additive. If both are present, `cmd:` wins and the `cmds:` list is dropped — see `Task.UnmarshalYAML` in `taskfile/types.go`. Pick one per task.
- `cmds: [{ task: X }]` and `deps: [X]` look similar but behave differently: `cmds` calls run sequentially in order and inherit the parent's resolved env; `deps` run concurrently as prerequisites and do **not** inherit env. See [Variables › Parent-to-child Env Propagation](../variables/#parent-to-child-env-propagation).
- A `Var` written as a bare string is the same as `value:`; using `sh:` switches to dynamic resolution. The two are mutually exclusive in practice — if both are set, the shell command wins (it is what `Var.Sh != ""` triggers).
- A bare-string `Precondition` reuses the failed shell command as its error message; the map form lets you provide a human-readable `msg:` instead.
