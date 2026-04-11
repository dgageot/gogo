---
title: Dependencies
---

# Dependencies

Tasks can declare dependencies on other tasks. Dependencies run concurrently before the task's own commands.

```yaml
tasks:
  build:
    cmd: go build ./...

  test:
    deps: [build]
    cmd: go test ./...
```

Running `gogo test` will first run `build`, then `test`.

## Concurrent Dependencies

When a task has multiple dependencies, they run in parallel:

```yaml
tasks:
  generate:
    cmd: go generate ./...

  lint:
    cmd: golangci-lint run

  check:
    deps: [generate, lint]
    cmd: echo "All checks passed"
```

`generate` and `lint` run concurrently. `check`'s command runs only after both finish.

## Task References in Commands

You can also call tasks from within a command list:

```yaml
tasks:
  all:
    cmds:
      - task: build
      - task: test
      - task: lint
```

Unlike dependencies, task references in `cmds` run sequentially.

Variables can be passed to called tasks:

```yaml
tasks:
  all:
    cmds:
      - task: build
        vars:
          MODE: release
```

## Deduplication

Tasks are executed at most once per run. If the same task appears multiple times in the dependency graph, only the first execution proceeds — subsequent references wait for it to complete and reuse its result:

```yaml
tasks:
  all:
    deps: [frontend, backend]

  frontend:
    deps: [generate]
    cmd: build-frontend

  backend:
    deps: [generate]
    cmd: build-backend

  generate:
    cmd: go generate ./...
```

Here, `generate` runs only once even though both `frontend` and `backend` depend on it.
