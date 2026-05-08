---
title: Quick Start
---

# Quick Start

Create a `gogo.yaml` in your project root:

```yaml
tasks:
  # Say hello
  default:
    cmd: echo "Hello from gogo!"

  # Build the project
  build:
    cmd: go build ./...
    sources: go     # built-in preset: **/*.go + go.mod + go.sum

  # Run tests
  test:
    cmd: go test ./...
    sources: go
```

Run a task:

```sh
gogo            # runs the "default" task
gogo build      # runs the "build" task
```

List available tasks (only tasks with comments are shown):

```sh
gogo -l
```

Watch for changes and re-run:

```sh
gogo -w test
```
