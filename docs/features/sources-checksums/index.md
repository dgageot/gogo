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

## Status Mode (`status:`)

When file fingerprints can't answer "is this already done?", give the task `status:` commands — shell probes of the desired end state. The task is skipped when **every** status command exits 0:

```yaml
tasks:
  create-bucket:
    status:
      - aws s3api head-bucket --bucket my-app-artifacts
    cmd: aws s3 mb s3://my-app-artifacts

  install-tools:
    status:
      - which golangci-lint
      - which goimports
    cmds:
      - go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
      - go install golang.org/x/tools/cmd/goimports@latest
```

This suits idempotent operations — creating cloud resources, installing tools, running database migrations — where the world, not a file tree, holds the answer.

Semantics:

- A single command can be written as a plain string: `status: which aws`.
- The first failing command short-circuits — one "not done" answer is enough to run the task.
- **With `sources:` too, both must agree**: changed sources force a run regardless of status (the probes aren't even run), and a failing status forces a run even when sources are unchanged.
- `--force` bypasses status checks like every other up-to-date mechanism.
- Status commands run with the task's full environment, including [`op://` secret](../secrets/) resolution — same as preconditions.
- Under `--dry`, status probes still run (they're read-only checks) so the printed plan reflects what a real run would skip.

Not to be confused with [preconditions](../preconditions/), which invert the meaning: a failing precondition **aborts with an error** ("you may not run"), while a failing status command simply means the task needs to run.

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
