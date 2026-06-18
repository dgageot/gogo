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

If `backend/` and `frontend/` each have their own `gogo.yaml` and the parent task file includes both, gogo still uses the parent include root when run from either sub-project. That makes sibling namespaces available from every included project:

```sh
cd backend
gogo frontend:test
```

This promotion only happens for the nearest ancestor task file that directly includes the current project, so unrelated higher-level `gogo.yaml` files are ignored.

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
