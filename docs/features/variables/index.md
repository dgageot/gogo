---
title: Variables
---

# Variables

gogo supports variables that can be used in commands via `{{ "{{" }}.VAR}}` templates or `${VAR}` shell expansion.

## Global Variables

Define variables at the top level of your taskfile:

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
| `TASKFILE_DIR` | The working directory for the task (defaults to the taskfile directory) |
| `CLI_ARGS` | Extra arguments passed after `--` |

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

Variables in the taskfile are expanded from environment variables using `{{ "{{" }}.VAR}}` syntax at parse time:

```yaml
tasks:
  deploy:
    cmd: deploy --region {{ "{{" }}.AWS_REGION}}
```

If `AWS_REGION` is set in the environment, it will be substituted before the taskfile is processed.

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
