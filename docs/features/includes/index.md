---
title: Includes
---

# Includes

Split your task file into multiple files across subdirectories. Each included directory must contain its own `gogo.yaml`.

## Basic Setup

```
project/
├── gogo.yaml
├── backend/
│   └── gogo.yaml
└── frontend/
    └── gogo.yaml
```

```yaml
# project/gogo.yaml
includes:
  - backend
  - frontend
```

```yaml
# project/backend/gogo.yaml
tasks:
  build:
    cmd: go build ./...

  test:
    cmd: go test ./...
```

## Namespaced Tasks

Included tasks are prefixed with their directory name:

```sh
gogo backend:build
gogo frontend:test
```

## Automatic Namespace Resolution

When you run gogo from a subdirectory, it automatically resolves task names to the matching namespace. From the `backend/` directory:

```sh
cd backend
gogo build      # resolves to backend:build
```

## Wildcard Patterns

A `...` wildcard runs a same-named task across every namespace (Bazel-style):

```sh
gogo ...:test     # runs test, backend:test, frontend:test, ...
```

The wildcard spans zero or more namespace levels, so nested tasks like `a:b:test` match too. Namespaces without a matching task are skipped; the pattern errors only when nothing matches at all. Matching is exact (no prefix shortcuts or aliases) and internal `_`-prefixed tasks are never included. Matches run in parallel and all of them run even if one fails.

When run from a subdirectory, the pattern is scoped to that namespace's subtree — `gogo ...:test` from `backend/` only runs tasks under `backend:`.

Patterns are accepted anywhere a task name is: in `deps:` (matches run in parallel) and in `task:` sub-calls (matches run in sequence). The one exception is `--watch`, which needs a single task to poll and rejects patterns.

## Dotenv Deduplication

Each included task file can define its own `dotenv` files. If multiple includes reference the same `.env` file (by absolute path), it's loaded only once.

## Flatten

`flatten` is a sibling of `includes` for splitting **a single namespace** across multiple YAML files. Where `includes` adds a directory whose tasks become namespaced (`backend:build`), `flatten` pulls another YAML file's tasks into the **current** namespace verbatim:

```yaml
# gogo.yaml
flatten:
  - tasks/lint.yml
  - tasks/test.yml

tasks:
  default:
    deps: [lint, test]
```

```yaml
# tasks/lint.yml
tasks:
  lint:
    cmd: golangci-lint run
```

```yaml
# tasks/test.yml
tasks:
  test:
    cmd: go test ./...
```

```sh
gogo lint    # no namespace prefix
gogo test
```

Key behaviors:

- **No namespace**: tasks land in the parent file's namespace as-is. Colons inside task names are preserved (e.g. `lint:backend` stays `lint:backend`).
- **Path resolution**: `flatten` entries are paths to YAML files (not directories), resolved relative to the file that declares them. Absolute paths and `~/` are supported.
- **First defined wins**: a task declared directly in the parent file beats a same-named task in a flatten file. Between two flatten files, the first listed wins.
- **`dir:` is rooted at the parent**: a `dir: backend` written inside a flatten file means "`backend` next to the parent task file", not relative to the flatten file's location.
- **Variables merge**: global `vars` from flatten files are merged into the parent's `vars`, with the parent winning conflicts.
- **Comments become descriptions**: task comments in a flatten file are preserved as `Desc` and shown by `gogo -l`.
- **Nesting**: a flatten file can itself declare `flatten:` (still no namespace) or `includes:` (sub-namespaces under the parent's namespace).
- **Inside an include**: when used from inside a namespaced include, the flatten file's tasks land in that include's namespace (e.g. `cli:helper` rather than `helper`).
- **Cycles** are rejected with a clear error.
