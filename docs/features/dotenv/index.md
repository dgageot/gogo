---
title: Dotenv
---

# Dotenv

gogo can load environment variables from `.env` files. Variables are injected into task commands but don't override existing environment variables.

## Global Dotenv

The top-level `dotenv` field loads `.env` files for all tasks:

```yaml
dotenv:
  - .env
  - .env.local
```

## Per-Task Dotenv

Each task can also define its own `dotenv` files. These are resolved relative to the task's working directory and are loaded in addition to global dotenv files. Per-task values override global dotenv values for that task.

```yaml
dotenv:
  - .env

tasks:
  api:
    dotenv:
      - .env.api
    cmd: go run ./cmd/api

  worker:
    dir: services/worker
    dotenv:
      - .env.worker
    cmd: go run .
```

## File Format

Standard `.env` format with support for comments, quoted values, and an optional `export` prefix:

```
# Database config
DB_HOST=localhost
DB_PORT=5432
DB_NAME="myapp_dev"
DB_PASSWORD='s3cret'

# An `export` prefix is allowed and ignored
export API_TOKEN=abc123
```

Quoting rules:

- **Single quotes** are literal — the value between them is taken verbatim.
- **Double quotes** support the escape sequences `\"`, `\\`, `\n` (newline), and `\t` (tab).
- **Unquoted** values are trimmed of surrounding whitespace.

Keys must match `[A-Za-z_][A-Za-z0-9_]*`; invalid lines cause a parse error.

## Resolution Rules

- Files are loaded in order; later files override earlier ones
- Per-task dotenv values override global dotenv values
- Existing environment variables are never overridden
- Missing files are silently skipped
- Global dotenv paths are relative to the task file directory
- Per-task dotenv paths are relative to the task's working directory
- `~/` is expanded to the home directory

## With Includes

When using [includes](../includes/), each included task file can define its own `dotenv` files. Files are deduplicated by absolute path — the same `.env` file is never loaded twice.
