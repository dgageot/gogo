---
title: Dependencies
---

# Dependencies

Tasks can declare dependencies on other tasks. Dependencies run concurrently before the task's own commands.

```yaml
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
all:
  cmds:
    - task: build
    - task: test
    - task: lint
```

Unlike dependencies, task references in `cmds` run sequentially.
