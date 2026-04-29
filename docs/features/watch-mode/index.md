---
title: Watch Mode
---

# Watch Mode

Watch mode polls source files and re-runs the task when they change. The task (or any of its transitive dependencies) must have `sources` defined.

```sh
gogo -w test
```

## How It Works

1. The task runs once immediately
2. gogo polls the source files at a regular interval
3. When the checksum changes, the task re-runs
4. Errors are printed but don't stop the watcher

Sources are collected recursively across the task and all of its dependencies, each with their own working directory. Watching `gogo -w default` therefore picks up changes to anything that feeds into `default` — you don't need to copy `sources` onto the parent task.

## Polling Interval

The default interval is 500ms. Override it in the task file:

```yaml
interval: 1s

tasks:
  test:
    cmd: go test ./...
    sources:
      - "*.go"
      - "cmd/*.go"
```

The `interval` field accepts any Go duration string: `100ms`, `1s`, `2s`, etc.

## Example

```yaml
# gogo.yaml
tasks:
  test:
    cmd: go test ./...
    sources:
      - "*.go"
      - "cmd/*.go"
      - go.mod
```

```sh
gogo -w test
```

Edit a `.go` file and save — the tests re-run automatically.
