---
title: Variables
---

# Variables

gogo supports variables that can be used in commands via `{{ "{{" }}.VAR}}` templates or `${VAR}` shell expansion.

## Global Variables

Define variables at the top level of your task file:

```yaml
vars:
  BINARY_NAME: myapp
  VERSION: 1.0.0

tasks:
  build:
    cmd: go build -ldflags "-X main.version={{ "{{" }}.VERSION}}" -o {{ "{{" }}.BINARY_NAME}} ./...
```

## Dynamic Variables

Variables can be computed from shell commands:

```yaml
vars:
  GIT_SHA:
    sh: git rev-parse --short HEAD
  DATE:
    sh: date -u +%Y-%m-%dT%H:%M:%SZ

tasks:
  build:
    cmd: go build -ldflags "-X main.sha={{ "{{" }}.GIT_SHA}} -X main.date={{ "{{" }}.DATE}}" ./...
```

## Task-Scoped Variables

Tasks can define their own variables that override global ones:

```yaml
vars:
  ENV: development

tasks:
  deploy:
    vars:
      ENV: production
    cmd: deploy --env {{ "{{" }}.ENV}}
```

## Built-in Variables

| Variable | Description |
|----------|-------------|
| `TASK_FILE_DIR` | The working directory for the task (defaults to the task file directory) |
| `CLI_ARGS` | Extra arguments passed after `--` |
| `GIT_COMMIT` | Full SHA at `HEAD` (empty outside a git repo) |
| `GIT_SHORT_COMMIT` | 7-char SHA at `HEAD` (empty outside a git repo) |
| `GIT_TAG` | Exact-match tag at `HEAD`, or empty if `HEAD` isn't tagged |
| `GIT_BRANCH` | Current branch name; `HEAD` when detached |
| `GIT_DIRTY` | `dirty` if the working tree has changes; empty when clean |

### Built-in Git Variables

The `GIT_*` variables are resolved lazily on first use and cached for the lifetime of one `gogo` invocation, so a task that doesn't reference any of them never shells out to git. Outside a git repo all five resolve to the empty string — they never raise an error.

A user-defined variable with the same name always wins, so a project can pin (for example) `GIT_COMMIT` in CI for a reproducible build:

```yaml
vars:
  GIT_COMMIT: "{{ "{{" }}.CI_COMMIT_SHA}}"   # take it from the CI env instead of git
```

### Example: ldflags from git metadata

```yaml
tasks:
  build:
    cmd: >
      go build
      -ldflags '-X main.Version={{ "{{" }}.GIT_TAG}} -X main.Commit={{ "{{" }}.GIT_COMMIT}}'
      -o bin/myapp .
```

No `vars:` block, no `sh: git rev-parse HEAD`. The same template can be written shell-style — `${GIT_COMMIT}` — because gogo expands both forms before handing the command to `/bin/sh`.

## CLI Arguments

Arguments after `--` are available as `{{ "{{" }}.CLI_ARGS}}`:

```yaml
tasks:
  test:
    cmd: go test {{ "{{" }}.CLI_ARGS}} ./...
```

```sh
gogo test -- -v -run TestFoo
```

## Environment Variable Expansion

Variables in the task file are expanded from environment variables using `{{ "{{" }}.VAR}}` syntax at parse time:

```yaml
tasks:
  deploy:
    cmd: deploy --region {{ "{{" }}.AWS_REGION}}
```

If `AWS_REGION` is set in the environment, it will be substituted before the task file is processed.

## Task Environment

Tasks can set environment variables for their commands. Values support `${VAR}` expansion from variables and the environment:

```yaml
vars:
  PORT: "8080"

tasks:
  serve:
    env:
      PORT: "${PORT}"
      NODE_ENV: production
    cmd: node server.js
```

### Cross-References Between Env Entries

`env` values can reference other entries in the same `env` block. References are resolved transparently before the command runs:

```yaml
tasks:
  serve:
    env:
      HOST: localhost
      PORT: "8080"
      ADDR: "${HOST}:${PORT}"   # → localhost:8080
    cmd: server --addr $ADDR
```

Lookup order for `${VAR}` inside an env value: another key in the same `env` block first, then task `vars`, then the process environment. Self-cycles or mutual cycles between env keys resolve to the empty string rather than looping forever.

## Variable Resolution Order

When resolving `{{ "{{" }}.VAR}}` or `${VAR}` inside a command, gogo looks up the name in this order:

1. Task-scoped `vars` (which override global `vars`)
2. `CLI_ARGS` (if the lookup is for that name)
3. Built-in variables (`GIT_*`)
4. The process environment

Unknown `${VAR}` references are left intact for the shell to expand. Unknown `{{ "{{" }}.VAR}}` templates are left verbatim.
