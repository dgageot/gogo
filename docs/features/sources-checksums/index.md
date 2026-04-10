---
title: Sources & Checksums
---

# Sources & Checksums

Tasks can declare source file patterns. gogo computes a SHA256 checksum of all matching files and skips execution if nothing changed since the last run.

```yaml
tasks:
  build:
    cmd: go build -o myapp ./...
    sources:
      - "*.go"
      - "cmd/*.go"
      - go.mod
      - go.sum
```

On the first run, the task executes and the checksum is stored in `.gogo/checksum/`. On subsequent runs, gogo recomputes the checksum and skips the task if it matches.

## Glob Patterns

Source patterns use Go's [`filepath.Glob`](https://pkg.go.dev/path/filepath#Glob) syntax:

| Pattern | Matches |
|---------|---------|
| `*.go` | Go files in the task directory |
| `cmd/*.go` | Go files in the cmd directory |
| `go.mod` | The go.mod file |

Note: `filepath.Glob` does not support recursive `**` patterns or brace expansion like `{mod,sum}`. List each directory or file explicitly.

## Checksum Storage

Checksums are stored in `.gogo/checksum/` relative to the taskfile directory. You should add `.gogo/` to your `.gitignore`:

```
# .gitignore
.gogo/
```

## Up-to-Date Output

When a task is skipped, gogo prints:

```
[build] up to date
```
