---
title: Sources & Checksums
---

# Sources & Checksums

Tasks can declare source file patterns. gogo computes a SHA256 checksum of all matching files and skips execution if nothing changed since the last run.

```yaml
build:
  cmd: go build -o myapp ./...
  sources:
    - "**/*.go"
    - go.mod
    - go.sum
```

On the first run, the task executes and the checksum is stored in `.task/checksum/`. On subsequent runs, gogo recomputes the checksum and skips the task if it matches.

## Glob Patterns

Source patterns use Go's `filepath.Glob` syntax:

| Pattern | Matches |
|---------|---------|
| `*.go` | Go files in the task directory |
| `**/*.go` | Go files in all subdirectories |
| `cmd/*.go` | Go files in the cmd directory |
| `go.{mod,sum}` | go.mod and go.sum |

## Checksum Storage

Checksums are stored in `.task/checksum/` relative to the taskfile directory. You should add `.task/` to your `.gitignore`:

```
# .gitignore
.task/
```

## Up-to-Date Output

When a task is skipped, gogo prints:

```
[build] up to date
```
