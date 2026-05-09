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

Non-recursive patterns are matched with Go's [`filepath.Glob`](https://pkg.go.dev/path/filepath#Glob), which supports `*`, `?`, and `[…]` character classes within a single path segment:

| Pattern | Matches |
|---------|---------|
| `*.go` | Go files in the task directory |
| `cmd/*.go` | Go files in the `cmd` directory |
| `go.mod` | The `go.mod` file |

### Recursive Patterns (`**`)

A pattern containing `**` triggers a recursive walk. gogo's matcher is a small superset of `filepath.Glob` rather than a full doublestar implementation — keep these rules in mind:

- The pattern is split on the **first** `**`. The text before `**` selects the base directory; the text after `**` is matched against each file's **base name** (not its full path).
- Only one `**` per pattern. A second `**` becomes part of the basename match, which almost certainly isn't what you want.
- Hidden directories (names starting with `.`, e.g. `.git`, `.gogo`) are skipped during the walk.
- A trailing `**` with nothing after it matches every file under the base directory.

| Pattern | Matches |
|---------|---------|
| `**/*.go` | All `.go` files in any subdirectory (basename matches `*.go`) |
| `**/*.proto` | All `.proto` files recursively |
| `vendor/**` | Every file under `vendor/` |
| `internal/**/*.go` | All `.go` files under `internal/` (basename matches `*.go`) |

Because matching after `**` is basename-only, a pattern like `**/foo/*.go` doesn't constrain `foo` to be the *direct* parent directory — it just looks for files whose basename matches `foo/*.go`, which won't match anything. Express that constraint as `foo/**/*.go` instead, anchored to a known prefix.

## Presets

The same source list — `**/*.go`, `go.mod`, `go.sum` — is repeated on every Go build/lint/test task. Source **presets** let you name a list once and reference it by name:

```yaml
sources:
  lint: [go, .golangci.yml]    # composes the built-in `go` preset with one literal

tasks:
  build:
    cmd: go build ./...
    sources: go                # short form: a single name
  test:
    cmd: go test ./...
    sources: go
  lint:
    cmd: golangci-lint run
    sources: lint              # references the user-defined preset above
  format:
    cmd: goimports -w .
    sources: "**/*.go"         # plain globs still work
```

A `sources:` entry that has **no glob characters** (`*`, `?`, `[`, `]`, `/`, `\`) is looked up in the preset map first; if no preset matches, it's treated as a literal path. So `go.mod` and `.golangci.yml` continue to work as bare filenames.

### Built-in Presets

gogo ships with two built-ins. User-defined entries with the same name win.

| Preset | Expands to |
|--------|-----------|
| `go` | `**/*.go`, `go.mod`, `go.sum` |
| `go-vendored` | the `go` preset, plus `vendor/**` |

### Composition

Presets can reference other presets — the resolver expands recursively, deduplicates, and rejects cycles:

```yaml
sources:
  go-strict: [go, .golangci.yml, .editorconfig]
  ci: [go-strict, scripts/**]
```

### Overriding a Built-in

Define a preset with the same name to replace the built-in:

```yaml
sources:
  # This project doesn't track go.sum.
  go: ["**/*.go", "go.mod"]
```

## Checksum Storage

Checksums are stored in `.gogo/checksum/` relative to the task file directory. You should add `.gogo/` to your `.gitignore`:

```
# .gitignore
.gogo/
```

## Up-to-Date Output

When a task is skipped, gogo prints:

```
[build] up to date
```
