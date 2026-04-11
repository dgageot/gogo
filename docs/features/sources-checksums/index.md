---
title: Sources & Checksums
---

# Sources & Checksums

Tasks can declare source file patterns. gogo uses these for incremental builds — skipping execution when nothing has changed.

## Checksum Mode (sources only)

When only `sources` is set, gogo computes a SHA256 checksum of all matching files and skips execution if nothing changed since the last run:

```yaml
tasks:
  build:
    cmd: go build -o myapp ./...
    sources:
      - "**/*.go"
      - go.mod
      - go.sum
```

On the first run, the task executes and the checksum is stored in `.gogo/checksum/`. On subsequent runs, gogo recomputes the checksum and skips the task if it matches.

## Timestamp Mode (sources + generates)

When both `sources` and `generates` are set, gogo uses timestamp comparison instead. The task is skipped only when all output files exist and are newer than all source files:

```yaml
tasks:
  build:
    cmd: go build -o bin/myapp ./...
    sources:
      - "**/*.go"
      - go.mod
    generates:
      - bin/myapp
```

This avoids checksum storage and matches traditional `make`-style incremental builds.

## Glob Patterns

Source and generates patterns use Go's [`filepath.Glob`](https://pkg.go.dev/path/filepath#Glob) syntax:

| Pattern | Matches |
|---------|---------|
| `*.go` | Go files in the task directory |
| `cmd/*.go` | Go files in the cmd directory |
| `go.mod` | The go.mod file |

### Recursive Patterns

Patterns containing `**` are matched recursively across all subdirectories (hidden directories starting with `.` are skipped):

| Pattern | Matches |
|---------|---------|
| `**/*.go` | All Go files in any subdirectory |
| `**/*.proto` | All proto files recursively |

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
