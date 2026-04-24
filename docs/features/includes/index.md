---
title: Includes
---

# Includes

Split your taskfile into multiple files across subdirectories. Each included directory must contain its own `gogo.yaml`.

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

## Dotenv Deduplication

Each included taskfile can define its own `dotenv` files. If multiple includes reference the same `.env` file (by absolute path), it's loaded only once.
